package store

import (
	"testing"
)

// CR-0010: the queue must survive a restart with the popup's tag delta intact.
func TestAddQueueItem_PersistsTagsAndSetTags(t *testing.T) {
	s := newTestStore(t)
	req := IngestRequest{
		URL: "https://example.com/a", Title: "A", Text: longText(60),
		VisitTs: nowMs(), Domain: "example.com",
		Tags: []string{"work", "read-later"}, SetTags: true,
	}
	if err := s.AddQueueItem(req); err != nil {
		t.Fatalf("AddQueueItem: %v", err)
	}

	items, err := s.LoadPendingItems()
	if err != nil {
		t.Fatalf("LoadPendingItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 pending item, got %d", len(items))
	}
	got := items[0]
	if len(got.Tags) != 2 || got.Tags[0] != "work" || got.Tags[1] != "read-later" {
		t.Errorf("tags not preserved: %v", got.Tags)
	}
	if !got.SetTags {
		t.Errorf("setTags not preserved")
	}
	if got.Source != "indexed" {
		t.Errorf("source = %q, want indexed", got.Source)
	}
}

// CR-0010: a definitive embed failure flips the row to 'failed' (visible via
// GetQueueItemStatus), and RetryQueueItem resets it to 'pending' with the
// original tags. GetStatus counts pending vs failed correctly along the way.
func TestQueueFailAndRetryLifecycle(t *testing.T) {
	s := newTestStore(t)
	url := "https://example.com/b"
	req := IngestRequest{
		URL: url, Title: "B", Text: longText(60),
		VisitTs: nowMs(), Domain: "example.com",
		Tags: []string{"ai"}, SetTags: true,
	}
	if err := s.AddQueueItem(req); err != nil {
		t.Fatalf("AddQueueItem: %v", err)
	}

	// Pending → counted as pending, not failed.
	if _, _, pending, failed, _ := s.GetStatus(); pending != 1 || failed != 0 {
		t.Fatalf("after add: pending=%d failed=%d, want 1/0", pending, failed)
	}

	// Mark failed.
	if err := s.MarkQueueItemFailed(url, "openrouter 429", 4); err != nil {
		t.Fatalf("MarkQueueItemFailed: %v", err)
	}
	status, lastErr, ok, err := s.GetQueueItemStatus(url)
	if err != nil || !ok {
		t.Fatalf("GetQueueItemStatus: ok=%v err=%v", ok, err)
	}
	if status != "failed" || lastErr != "openrouter 429" {
		t.Errorf("status=%q lastErr=%q, want failed/openrouter 429", status, lastErr)
	}
	if _, _, pending, failed, _ := s.GetStatus(); pending != 0 || failed != 1 {
		t.Fatalf("after fail: pending=%d failed=%d, want 0/1", pending, failed)
	}

	// Retry → back to pending, tags intact.
	retried, found, err := s.RetryQueueItem(url)
	if err != nil || !found {
		t.Fatalf("RetryQueueItem: found=%v err=%v", found, err)
	}
	if len(retried.Tags) != 1 || retried.Tags[0] != "ai" || !retried.SetTags {
		t.Errorf("retry lost tag delta: tags=%v setTags=%v", retried.Tags, retried.SetTags)
	}
	if _, _, pending, failed, _ := s.GetStatus(); pending != 1 || failed != 0 {
		t.Fatalf("after retry: pending=%d failed=%d, want 1/0", pending, failed)
	}

	// Retrying again with no failed row → not found.
	if _, found, _ := s.RetryQueueItem(url); found {
		t.Errorf("RetryQueueItem found a row when none should be failed")
	}
}

// CR-0010: re-ingesting a URL clears a stale 'failed' row so it doesn't linger.
func TestAddQueueItem_ClearsStaleFailedRow(t *testing.T) {
	s := newTestStore(t)
	url := "https://example.com/c"
	base := IngestRequest{URL: url, Text: longText(60), VisitTs: nowMs(), Domain: "example.com"}

	if err := s.AddQueueItem(base); err != nil {
		t.Fatalf("AddQueueItem: %v", err)
	}
	if err := s.MarkQueueItemFailed(url, "boom", 4); err != nil {
		t.Fatalf("MarkQueueItemFailed: %v", err)
	}
	// Fresh ingest of the same URL.
	if err := s.AddQueueItem(base); err != nil {
		t.Fatalf("AddQueueItem re-ingest: %v", err)
	}
	if _, _, pending, failed, _ := s.GetStatus(); pending != 1 || failed != 0 {
		t.Fatalf("re-ingest: pending=%d failed=%d, want 1/0 (stale failed cleared)", pending, failed)
	}
}
