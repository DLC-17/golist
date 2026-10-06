package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	cfg := LoadConfig()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	bm, err := NewBatchManager(cfg.DataDir)
	if err != nil {
		slog.Error("failed to initialize batch manager", "error", err)
		os.Exit(1)
	}

	limiter := NewRateLimiter(50) // 50 requests per minute
	ai := NewAICategorizer(cfg.GeminiAPIKey, cfg.GeminiModel)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cmd := os.Args[1]
	switch cmd {
	case "process":
		handleProcess(ctx, bm, ai, os.Args[2:])
	case "review":
		handleReview(bm, os.Args[2:])
	case "publish":
		handlePublish(ctx, cfg, bm, limiter, os.Args[2:])
	case "status":
		handleStatus(bm, limiter)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`GoList - Lean AI-Powered eBay Listing Generator

Usage:
  golist <command> [arguments]

Commands:
  process [folder]   Process image batches into AI drafts (supports --watch)
  review <batch_id>  Inspect and approve/edit a draft (supports --edit, --approve)
  publish [batch_id] Publish draft(s) to eBay (supports --all, --dry-run)
  status             Show system status and batch counts across pipeline
  help               Show this help message

Configuration (.env or environment variables):
  GEMINI_API_KEY      Google Gemini API key for vision analysis
  GEMINI_MODEL        Vision model (default: gemini-2.5-flash)
  EBAY_CLIENT_ID      eBay Developer App ID
  EBAY_CLIENT_SECRET  eBay Developer Cert ID
  EBAY_REFRESH_TOKEN  eBay User Refresh Token
  EBAY_ENV            sandbox (default) or production
  DATA_DIR            Storage directory (default: ./data)`)
}

func handleProcess(ctx context.Context, bm *BatchManager, ai *AICategorizer, args []string) {
	fs := flag.NewFlagSet("process", flag.ExitOnError)
	watch := fs.Bool("watch", false, "Continuously poll dump directory for new batches")
	_ = fs.Parse(normalizeArgs(args))

	// Step 1: Check and resume any batches stuck in processing/
	recovered, err := bm.RecoverProcessing()
	if err == nil && len(recovered) > 0 {
		slog.Info("resuming incomplete batches from processing/", "count", len(recovered))
		for _, batchID := range recovered {
			processSingleBatch(ctx, bm, ai, batchID, false)
		}
	}

	remaining := fs.Args()
	if len(remaining) > 0 {
		// Specific folder passed: if it's outside dump/, import it into dump/ first
		target := remaining[0]
		target = strings.TrimSuffix(target, string(filepath.Separator))
		batchID := filepath.Base(target)

		// If path is external, copy/move it to dump/
		dumpTarget := filepath.Join(bm.DumpDir(), batchID)
		if filepath.Clean(target) != filepath.Clean(dumpTarget) {
			if err := copyDir(target, dumpTarget); err != nil {
				slog.Error("failed to import folder into dump", "src", target, "error", err)
				return
			}
		}

		processSingleBatch(ctx, bm, ai, batchID, true)
		return
	}

	// Process all existing in dump/
	processDumpQueue(ctx, bm, ai)

	if *watch {
		slog.Info("watching dump directory for new image batches", "interval", "5s")
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("process watch stopped by user")
				return
			case <-ticker.C:
				processDumpQueue(ctx, bm, ai)
			}
		}
	}
}

func processDumpQueue(ctx context.Context, bm *BatchManager, ai *AICategorizer) {
	batches, err := bm.ListDumpBatches()
	if err != nil {
		slog.Error("failed to scan dump directory", "error", err)
		return
	}
	for _, batchID := range batches {
		if ctx.Err() != nil {
			return
		}
		processSingleBatch(ctx, bm, ai, batchID, true)
	}
}

func processSingleBatch(ctx context.Context, bm *BatchManager, ai *AICategorizer, batchID string, fromDump bool) {
	slog.Info("processing batch", "batch_id", batchID)

	var procDir string
	var err error
	if fromDump {
		procDir, err = bm.StartBatch(batchID)
		if err != nil {
			slog.Error("failed to move batch to processing", "batch_id", batchID, "error", err)
			return
		}
	} else {
		procDir = filepath.Join(bm.ProcessingDir(), batchID)
	}

	images, err := CollectImages(procDir)
	if err != nil || len(images) == 0 {
		slog.Warn("no images found in batch", "batch_id", batchID, "path", procDir)
		return
	}

	slog.Info("running AI vision analysis", "batch_id", batchID, "images", len(images))
	draft, err := ai.GenerateDraft(ctx, batchID, images)
	if err != nil {
		slog.Error("AI categorization failed", "batch_id", batchID, "error", err)
		return
	}

	if err := bm.CompleteDraft(batchID, draft); err != nil {
		slog.Error("failed to finalize draft", "batch_id", batchID, "error", err)
		return
	}

	slog.Info("draft created successfully", "batch_id", batchID, "title", draft.Title, "price", draft.SuggestedPrice)
	fmt.Printf("\n✓ Draft created: %s\n  Title: %s\n  Category: %s (%s)\n  Price: $%.2f\n  Run 'golist review %s' to verify.\n\n",
		batchID, draft.Title, draft.CategoryName, draft.CategoryID, draft.SuggestedPrice, batchID)
}

func handleReview(bm *BatchManager, args []string) {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	editFlag := fs.Bool("edit", false, "Open draft item.json in $EDITOR")
	approveFlag := fs.Bool("approve", false, "Mark draft as APPROVED for publishing")
	_ = fs.Parse(normalizeArgs(args))

	if fs.NArg() < 1 {
		drafts, _ := bm.ListDrafts()
		fmt.Printf("Available drafts (%d):\n", len(drafts))
		for _, d := range drafts {
			fmt.Printf("  - %s\n", d)
		}
		fmt.Println("\nUsage: golist review <batch_id> [--edit] [--approve]")
		return
	}

	batchID := fs.Arg(0)
	draft, err := bm.LoadDraft(batchID)
	if err != nil {
		slog.Error("failed to load draft", "batch_id", batchID, "error", err)
		return
	}

	if *approveFlag {
		draft.Status = "APPROVED"
		if err := bm.SaveDraft(batchID, draft); err != nil {
			slog.Error("failed to approve draft", "error", err)
			return
		}
		fmt.Printf("✓ Batch %s approved for publishing.\n", batchID)
		return
	}

	if *editFlag {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "nano"
		}
		draftFile := filepath.Join(bm.DraftsDir(), batchID, "item.json")
		cmd := exec.Command(editor, draftFile)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			slog.Error("failed to launch editor", "error", err)
		}
		return
	}

	fmt.Println("==================================================")
	fmt.Printf("DRAFT REVIEW: %s\n", batchID)
	fmt.Println("==================================================")
	fmt.Printf("Status:          %s\n", draft.Status)
	fmt.Printf("Title:           %s\n", draft.Title)
	fmt.Printf("Category:        %s (ID: %s)\n", draft.CategoryName, draft.CategoryID)
	fmt.Printf("Condition:       %s\n", draft.Condition)
	fmt.Printf("Suggested Price: $%.2f\n", draft.SuggestedPrice)
	fmt.Printf("Images:          %d files\n", len(draft.Images))
	fmt.Println("\nItem Specifics:")
	for k, v := range draft.ItemSpecifics {
		fmt.Printf("  • %s: %s\n", k, v)
	}
	fmt.Println("\nDescription:")
	fmt.Println(draft.Description)
	fmt.Println("==================================================")
	fmt.Println("Actions:")
	fmt.Printf("  Approve:  golist review %s --approve\n", batchID)
	fmt.Printf("  Edit:     golist review %s --edit\n", batchID)
	fmt.Printf("  Publish:  golist publish %s\n", batchID)
}

func handlePublish(ctx context.Context, cfg Config, bm *BatchManager, limiter *RateLimiter, args []string) {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	allFlag := fs.Bool("all", false, "Publish all approved drafts")
	dryRun := fs.Bool("dry-run", false, "Simulate eBay publish without making live API calls")
	force := fs.Bool("force", false, "Publish even if status is not APPROVED")
	_ = fs.Parse(normalizeArgs(args))

	client := NewEbayClient(cfg, limiter, *dryRun)

	var targets []string
	if *allFlag {
		drafts, _ := bm.ListDrafts()
		targets = drafts
	} else if fs.NArg() > 0 {
		targets = []string{fs.Arg(0)}
	} else {
		fmt.Println("Specify a batch_id or use --all. Run 'golist status' to view drafts.")
		return
	}

	for _, batchID := range targets {
		if ctx.Err() != nil {
			slog.Info("publish interrupted by user")
			return
		}

		draft, err := bm.LoadDraft(batchID)
		if err != nil {
			slog.Error("failed to load draft", "batch_id", batchID, "error", err)
			continue
		}

		if draft.Status != "APPROVED" && !*force && !*dryRun {
			slog.Warn("skipping unapproved draft (use --force to override)", "batch_id", batchID, "status", draft.Status)
			continue
		}

		slog.Info("publishing draft to eBay", "batch_id", batchID, "dry_run", *dryRun)
		listingID, err := client.PublishDraft(ctx, draft)
		if err != nil {
			slog.Error("failed to publish draft to eBay", "batch_id", batchID, "error", err)
			continue
		}

		if err := bm.MarkPublished(batchID, listingID); err != nil {
			slog.Error("failed to relocate published draft", "batch_id", batchID, "error", err)
			continue
		}

		fmt.Printf("✓ Successfully published %s -> eBay Listing ID: %s\n", batchID, listingID)
	}
}

func handleStatus(bm *BatchManager, limiter *RateLimiter) {
	status, err := bm.GetStatus()
	if err != nil {
		slog.Error("failed to inspect status", "error", err)
		return
	}

	fmt.Println("GoList Pipeline Status:")
	fmt.Printf("  • Dump (Pending Ingestion): %d batches\n", status.DumpCount)
	fmt.Printf("  • Processing (Active):      %d batches\n", status.ProcessingCount)
	fmt.Printf("  • Drafts (Pending Review):  %d batches\n", status.DraftsCount)
	fmt.Printf("  • Published:                %d listings\n", status.PublishedCount)
	fmt.Printf("  • Rate Limiter:             %s\n", limiter.Status())
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(s)
			if err != nil {
				return err
			}
			if err := os.WriteFile(d, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeArgs(args []string) []string {
	var flags, pos []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			pos = append(pos, a)
		}
	}
	return append(flags, pos...)
}
