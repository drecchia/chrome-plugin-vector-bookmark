package store

import (
	"strings"
	"testing"
	"time"

	"github.com/vbm/daemon/internal/embed"
)

// newTestStore creates a Store backed by a temp directory + StubEmbedder.
// Each test gets a fresh DB; t.TempDir() is auto-cleaned.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir(), embed.NewStubEmbedder())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// longText produces a text with at least n words, enough to clear chunk.MinTokens.
func longText(n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = "word"
	}
	return strings.Join(words, " ")
}

func nowMs() int64 { return time.Now().UnixMilli() }

// ── RecordVisit ──────────────────────────────────────────────────────────────

func TestRecordVisit_UpsertsByURLHash(t *testing.T) {
	s := newTestStore(t)
	url := "https://example.com/a"

	for i := 0; i < 5; i++ {
		if err := s.RecordVisit(VisitRequest{
			URL: url, Title: "A", Domain: "example.com",
			VisitTs: nowMs(), DwellMs: 12_000,
		}); err != nil {
			t.Fatalf("visit %d: %v", i, err)
		}
	}

	visited, _, _, err := s.GetStatus()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if visited != 1 {
		t.Errorf("expected 1 page after 5 revisits (url_hash UNIQUE), got %d", visited)
	}
}

func TestRecordVisit_DifferentURLsAreSeparateRows(t *testing.T) {
	s := newTestStore(t)
	for _, u := range []string{
		"https://example.com/a",
		"https://example.com/b",
		"https://example.com/?q=1",
	} {
		if err := s.RecordVisit(VisitRequest{
			URL: u, Title: "x", Domain: "example.com", VisitTs: nowMs(),
		}); err != nil {
			t.Fatalf("visit %s: %v", u, err)
		}
	}
	visited, _, _, _ := s.GetStatus()
	if visited != 3 {
		t.Errorf("expected 3 distinct pages, got %d", visited)
	}
}

// ── Ingest ───────────────────────────────────────────────────────────────────

func TestIngest_TooShortIsNoOp(t *testing.T) {
	s := newTestStore(t)
	err := s.Ingest(IngestRequest{
		URL: "https://example.com/short", Domain: "example.com",
		Title: "x", Text: "tiny", VisitTs: nowMs(),
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	_, indexed, _, _ := s.GetStatus()
	if indexed != 0 {
		t.Errorf("short text should not produce an indexed page, got indexed=%d", indexed)
	}
}

func TestIngest_PromotesToIndexed(t *testing.T) {
	s := newTestStore(t)
	err := s.Ingest(IngestRequest{
		URL: "https://example.com/long", Domain: "example.com",
		Title: "long page", Text: longText(120), VisitTs: nowMs(),
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	_, indexed, _, _ := s.GetStatus()
	if indexed != 1 {
		t.Errorf("expected indexed=1, got %d", indexed)
	}
}

// ── Tags: set-mode vs merge-mode ────────────────────────────────────────────

func TestIngest_SetTagsReplacesExisting(t *testing.T) {
	s := newTestStore(t)
	url := "https://example.com/tags"

	// Initial: set [a, b, c].
	mustIngest(t, s, IngestRequest{
		URL: url, Domain: "example.com", Title: "t",
		Text: longText(120), VisitTs: nowMs(),
		Tags: []string{"a", "b", "c"}, SetTags: true,
	})
	requireTags(t, s, url, []string{"a", "b", "c"})

	// Re-ingest with set-mode [b, d] — should drop a, c; keep b; add d.
	mustIngest(t, s, IngestRequest{
		URL: url, Domain: "example.com", Title: "t",
		Text: longText(120), VisitTs: nowMs(),
		Tags: []string{"b", "d"}, SetTags: true,
	})
	requireTags(t, s, url, []string{"b", "d"})
}

func TestIngest_MergeTagsKeepsExisting(t *testing.T) {
	s := newTestStore(t)
	url := "https://example.com/merge"

	mustIngest(t, s, IngestRequest{
		URL: url, Domain: "example.com", Title: "t",
		Text: longText(120), VisitTs: nowMs(),
		Tags: []string{"x", "y"}, SetTags: true,
	})
	// Merge-mode (SetTags=false) should add z without removing x/y.
	mustIngest(t, s, IngestRequest{
		URL: url, Domain: "example.com", Title: "t",
		Text: longText(120), VisitTs: nowMs(),
		Tags: []string{"z"}, SetTags: false,
	})
	requireTags(t, s, url, []string{"x", "y", "z"})
}

func TestIngest_TagsAreNormalised(t *testing.T) {
	s := newTestStore(t)
	url := "https://example.com/norm"
	mustIngest(t, s, IngestRequest{
		URL: url, Domain: "example.com", Title: "t",
		Text: longText(120), VisitTs: nowMs(),
		// Mixed case + duplicate after lowercasing → store should dedupe and lowercase.
		Tags: []string{"AI", "ai", "Read-Later"}, SetTags: true,
	})
	got, err := s.GetPageTags(url)
	if err != nil {
		t.Fatalf("GetPageTags: %v", err)
	}
	for _, tag := range got {
		if tag != strings.ToLower(tag) {
			t.Errorf("expected lowercase tag, got %q", tag)
		}
	}
	if len(got) != 2 {
		t.Errorf("expected 2 tags after dedup, got %d (%v)", len(got), got)
	}
}

// ── ListTags / ListPagesByTag ────────────────────────────────────────────────

func TestListTags_CountsPagesPerTag(t *testing.T) {
	s := newTestStore(t)
	mustIngest(t, s, IngestRequest{
		URL: "https://a/", Domain: "a", Title: "a", Text: longText(80),
		VisitTs: nowMs(), Tags: []string{"shared", "only-a"}, SetTags: true,
	})
	mustIngest(t, s, IngestRequest{
		URL: "https://b/", Domain: "b", Title: "b", Text: longText(80),
		VisitTs: nowMs(), Tags: []string{"shared"}, SetTags: true,
	})
	tags, err := s.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	got := map[string]int{}
	for _, t := range tags {
		got[t.Tag] = t.Count
	}
	if got["shared"] != 2 {
		t.Errorf("'shared' count = %d, want 2", got["shared"])
	}
	if got["only-a"] != 1 {
		t.Errorf("'only-a' count = %d, want 1", got["only-a"])
	}
}

// ── Search ───────────────────────────────────────────────────────────────────

func TestSearch_BM25FindsIngestedDoc(t *testing.T) {
	s := newTestStore(t)
	mustIngest(t, s, IngestRequest{
		URL: "https://ml.dev/", Domain: "ml.dev", Title: "Machine Learning Primer",
		Text: "machine learning is a powerful technique for pattern recognition. " +
			longText(80), // pad to clear MinTokens
		VisitTs: nowMs(),
	})
	results, err := s.Search("machine learning", 5, "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 hit")
	}
	if results[0].URL != "https://ml.dev/" {
		t.Errorf("top hit url = %q", results[0].URL)
	}
}

func TestSearch_TagFilterRestrictsCandidates(t *testing.T) {
	s := newTestStore(t)
	commonText := "common keyword appears here. " + longText(80)
	mustIngest(t, s, IngestRequest{
		URL: "https://x/", Domain: "x", Title: "x", Text: commonText,
		VisitTs: nowMs(), Tags: []string{"keep"}, SetTags: true,
	})
	mustIngest(t, s, IngestRequest{
		URL: "https://y/", Domain: "y", Title: "y", Text: commonText,
		VisitTs: nowMs(), Tags: []string{"drop"}, SetTags: true,
	})
	results, err := s.Search("common keyword", 5, "keep")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://x/" {
		t.Errorf("tag filter failed: got %+v", results)
	}
}

// ── Forget ───────────────────────────────────────────────────────────────────

func TestForget_ByURLDeletesOnePageOnly(t *testing.T) {
	s := newTestStore(t)
	mustIngest(t, s, IngestRequest{
		URL: "https://keep/", Domain: "keep", Title: "k",
		Text: longText(80), VisitTs: nowMs(),
	})
	mustIngest(t, s, IngestRequest{
		URL: "https://drop/", Domain: "drop", Title: "d",
		Text: longText(80), VisitTs: nowMs(),
	})
	if err := s.Forget(ForgetRequest{Type: "url", Value: "https://drop/"}); err != nil {
		t.Fatalf("Forget url: %v", err)
	}
	exists, _, _ := s.PageStatus("https://drop/")
	if exists {
		t.Error("dropped page still exists")
	}
	exists, _, _ = s.PageStatus("https://keep/")
	if !exists {
		t.Error("kept page was wrongly deleted")
	}
}

func TestForget_ByDomainDeletesAllPagesOnDomain(t *testing.T) {
	s := newTestStore(t)
	mustIngest(t, s, IngestRequest{
		URL: "https://gone.dev/a", Domain: "gone.dev", Title: "1",
		Text: longText(80), VisitTs: nowMs(),
	})
	mustIngest(t, s, IngestRequest{
		URL: "https://gone.dev/b", Domain: "gone.dev", Title: "2",
		Text: longText(80), VisitTs: nowMs(),
	})
	mustIngest(t, s, IngestRequest{
		URL: "https://stay.dev/", Domain: "stay.dev", Title: "3",
		Text: longText(80), VisitTs: nowMs(),
	})
	if err := s.Forget(ForgetRequest{Type: "domain", Value: "gone.dev"}); err != nil {
		t.Fatalf("Forget domain: %v", err)
	}
	visited, _, _, _ := s.GetStatus()
	if visited != 1 {
		t.Errorf("expected 1 page left, got %d", visited)
	}
}

// ── Blacklist ────────────────────────────────────────────────────────────────

func TestBlacklist_AddListRemove(t *testing.T) {
	s := newTestStore(t)
	if err := s.AddToBlacklist("ads.com"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.AddToBlacklist("ads.com"); err != nil { // idempotent
		t.Fatalf("re-add: %v", err)
	}
	patterns, _ := s.GetBlacklist()
	if !contains(patterns, "ads.com") {
		t.Errorf("expected 'ads.com' in blacklist, got %v", patterns)
	}
	_ = s.RemoveFromBlacklist("ads.com")
	patterns, _ = s.GetBlacklist()
	if contains(patterns, "ads.com") {
		t.Errorf("expected 'ads.com' removed, got %v", patterns)
	}
}

// ── Cleanup (TTL) ────────────────────────────────────────────────────────────

func TestCleanup_DeletesPagesOlderThanTTL(t *testing.T) {
	s := newTestStore(t)
	old := time.Now().Add(-100 * 24 * time.Hour).UnixMilli()
	mustIngest(t, s, IngestRequest{
		URL: "https://stale/", Domain: "stale", Title: "old",
		Text: longText(80), VisitTs: old,
	})
	mustIngest(t, s, IngestRequest{
		URL: "https://fresh/", Domain: "fresh", Title: "new",
		Text: longText(80), VisitTs: nowMs(),
	})
	deleted, err := s.Cleanup(30) // keep last 30 days
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deletion, got %d", deleted)
	}
	exists, _, _ := s.PageStatus("https://fresh/")
	if !exists {
		t.Error("fresh page was wrongly deleted by TTL")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func mustIngest(t *testing.T, s *Store, req IngestRequest) {
	t.Helper()
	if err := s.Ingest(req); err != nil {
		t.Fatalf("Ingest %s: %v", req.URL, err)
	}
}

func requireTags(t *testing.T, s *Store, url string, want []string) {
	t.Helper()
	got, err := s.GetPageTags(url)
	if err != nil {
		t.Fatalf("GetPageTags: %v", err)
	}
	gotSet := map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	if len(got) != len(want) {
		t.Errorf("tag count mismatch: got %v, want %v", got, want)
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Errorf("missing tag %q (got %v)", w, got)
		}
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
