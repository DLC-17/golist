package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBatchLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "golist_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	bm, err := NewBatchManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create BatchManager: %v", err)
	}

	// 1. Create a batch in dump/
	batchName := "item_shoes_01"
	batchDumpDir := filepath.Join(bm.DumpDir(), batchName)
	if err := os.MkdirAll(batchDumpDir, 0755); err != nil {
		t.Fatal(err)
	}
	testImg := filepath.Join(batchDumpDir, "photo1.jpg")
	if err := os.WriteFile(testImg, []byte("fake image data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Check dump lists it
	dumps, err := bm.ListDumpBatches()
	if err != nil || len(dumps) != 1 || dumps[0] != batchName {
		t.Fatalf("expected dump batch %s, got %v (err: %v)", batchName, dumps, err)
	}

	// 2. Start processing
	procDir, err := bm.StartBatch(batchName)
	if err != nil {
		t.Fatalf("failed to start batch: %v", err)
	}
	if procDir != filepath.Join(bm.ProcessingDir(), batchName) {
		t.Fatalf("unexpected procDir: %s", procDir)
	}

	// Verify dump is now empty, processing has it
	dumps, _ = bm.ListDumpBatches()
	if len(dumps) != 0 {
		t.Fatalf("dump should be empty, got %v", dumps)
	}
	recoverBatches, _ := bm.RecoverProcessing()
	if len(recoverBatches) != 1 || recoverBatches[0] != batchName {
		t.Fatalf("processing should have batch %s, got %v", batchName, recoverBatches)
	}

	// 3. Complete draft
	draft := &ItemDraft{
		ID:             batchName,
		Title:          "Test Shoes",
		CategoryID:     "15709",
		Condition:      "USED_EXCELLENT",
		SuggestedPrice: 49.99,
		ItemSpecifics:  map[string]string{"Brand": "Nike"},
		Description:    "Great shoes",
		Images:         []string{"photo1.jpg"},
	}
	if err := bm.CompleteDraft(batchName, draft); err != nil {
		t.Fatalf("failed to complete draft: %v", err)
	}

	// Verify it is in drafts/
	drafts, err := bm.ListDrafts()
	if err != nil || len(drafts) != 1 || drafts[0] != batchName {
		t.Fatalf("expected drafts to have %s, got %v", batchName, drafts)
	}

	loaded, err := bm.LoadDraft(batchName)
	if err != nil {
		t.Fatalf("failed to load draft: %v", err)
	}
	if loaded.Title != "Test Shoes" || loaded.Status != "DRAFT_PENDING_REVIEW" {
		t.Fatalf("draft content mismatch: %+v", loaded)
	}

	// 4. Mark published
	if err := bm.MarkPublished(batchName, "EBAY-123456"); err != nil {
		t.Fatalf("failed to mark published: %v", err)
	}

	// Verify it is in published/
	status, err := bm.GetStatus()
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	if status.DraftsCount != 0 || status.PublishedCount != 1 {
		t.Fatalf("expected 0 drafts and 1 published, got %+v", status)
	}
}
