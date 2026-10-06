package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ItemDraft struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	CategoryID     string            `json:"category_id"`
	CategoryName   string            `json:"category_name,omitempty"`
	Condition      string            `json:"condition"` // NEW, LIKE_NEW, USED_EXCELLENT, USED_GOOD, FOR_PARTS
	SuggestedPrice float64           `json:"suggested_price"`
	ItemSpecifics  map[string]string `json:"item_specifics"`
	Description    string            `json:"description"`
	Images         []string          `json:"images"`
	Status         string            `json:"status"` // DRAFT_PENDING_REVIEW, APPROVED, PUBLISHED
	EbayListingID  string            `json:"ebay_listing_id,omitempty"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`
}

type BatchManager struct {
	baseDir string
}

func NewBatchManager(baseDir string) (*BatchManager, error) {
	bm := &BatchManager{baseDir: baseDir}
	dirs := []string{
		bm.DumpDir(),
		bm.ProcessingDir(),
		bm.DraftsDir(),
		bm.PublishedDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", d, err)
		}
	}
	return bm, nil
}

func (bm *BatchManager) DumpDir() string       { return filepath.Join(bm.baseDir, "dump") }
func (bm *BatchManager) ProcessingDir() string { return filepath.Join(bm.baseDir, "processing") }
func (bm *BatchManager) DraftsDir() string     { return filepath.Join(bm.baseDir, "drafts") }
func (bm *BatchManager) PublishedDir() string  { return filepath.Join(bm.baseDir, "published") }

// ListDumpBatches returns pending folders inside dump/
func (bm *BatchManager) ListDumpBatches() ([]string, error) {
	return listSubdirs(bm.DumpDir())
}

// ListDrafts returns batches currently in drafts/
func (bm *BatchManager) ListDrafts() ([]string, error) {
	return listSubdirs(bm.DraftsDir())
}

// RecoverProcessing returns batches stuck in processing/ from previous interruptions
func (bm *BatchManager) RecoverProcessing() ([]string, error) {
	return listSubdirs(bm.ProcessingDir())
}

// StartBatch moves dump/<batchName> to processing/<batchName> atomically
func (bm *BatchManager) StartBatch(batchName string) (string, error) {
	src := filepath.Join(bm.DumpDir(), batchName)
	dst := filepath.Join(bm.ProcessingDir(), batchName)
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("failed to move batch to processing: %w", err)
	}
	return dst, nil
}

// CompleteDraft moves processing/<batchName> to drafts/<batchName> and saves item.json
func (bm *BatchManager) CompleteDraft(batchName string, draft *ItemDraft) error {
	src := filepath.Join(bm.ProcessingDir(), batchName)
	dst := filepath.Join(bm.DraftsDir(), batchName)

	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("failed to move batch to drafts: %w", err)
	}

	draft.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if draft.CreatedAt == "" {
		draft.CreatedAt = draft.UpdatedAt
	}
	if draft.Status == "" {
		draft.Status = "DRAFT_PENDING_REVIEW"
	}

	// Update image paths relative to draft folder
	draftPath := filepath.Join(dst, "item.json")
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode draft json: %w", err)
	}
	return os.WriteFile(draftPath, data, 0644)
}

// LoadDraft loads item.json from drafts/<batchName>
func (bm *BatchManager) LoadDraft(batchName string) (*ItemDraft, error) {
	draftPath := filepath.Join(bm.DraftsDir(), batchName, "item.json")
	data, err := os.ReadFile(draftPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read draft file %s: %w", draftPath, err)
	}
	var draft ItemDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		return nil, fmt.Errorf("invalid draft json: %w", err)
	}
	return &draft, nil
}

// SaveDraft updates item.json in drafts/<batchName>
func (bm *BatchManager) SaveDraft(batchName string, draft *ItemDraft) error {
	draftPath := filepath.Join(bm.DraftsDir(), batchName, "item.json")
	draft.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(draftPath, data, 0644)
}

// MarkPublished updates draft status and moves drafts/<batchName> to published/<batchName>
func (bm *BatchManager) MarkPublished(batchName string, ebayListingID string) error {
	draft, err := bm.LoadDraft(batchName)
	if err != nil {
		return err
	}
	draft.Status = "PUBLISHED"
	draft.EbayListingID = ebayListingID
	draft.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := bm.SaveDraft(batchName, draft); err != nil {
		return err
	}

	src := filepath.Join(bm.DraftsDir(), batchName)
	dst := filepath.Join(bm.PublishedDir(), batchName)
	return os.Rename(src, dst)
}

// CollectImages finds all image files in a given directory
func CollectImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var images []string
	exts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if exts[ext] {
			images = append(images, filepath.Join(dir, e.Name()))
		}
	}
	return images, nil
}

type StatusSummary struct {
	DumpCount       int
	ProcessingCount int
	DraftsCount     int
	PublishedCount  int
}

func (bm *BatchManager) GetStatus() (StatusSummary, error) {
	var s StatusSummary
	d, err := listSubdirs(bm.DumpDir())
	if err == nil {
		s.DumpCount = len(d)
	}
	p, err := listSubdirs(bm.ProcessingDir())
	if err == nil {
		s.ProcessingCount = len(p)
	}
	dr, err := listSubdirs(bm.DraftsDir())
	if err == nil {
		s.DraftsCount = len(dr)
	}
	pub, err := listSubdirs(bm.PublishedDir())
	if err == nil {
		s.PublishedCount = len(pub)
	}
	return s, nil
}

func listSubdirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var subdirs []string
	for _, e := range entries {
		if e.IsDir() {
			subdirs = append(subdirs, e.Name())
		}
	}
	return subdirs, nil
}
