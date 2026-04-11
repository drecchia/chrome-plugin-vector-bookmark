package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vbm/daemon/internal/chunk"
	"github.com/vbm/daemon/internal/embed"
	_ "modernc.org/sqlite" // registers "sqlite" driver
)

// Store holds the database connection and embedder.
type Store struct {
	db       *sql.DB
	embedder embed.Embedder
}

// Page represents a page record.
type Page struct {
	ID       int64
	URL      string
	URLHash  string
	Title    string
	Domain   string
	VisitTs  int64
	DwellMs  int64
	ModelVer string
}

// IngestRequest holds data for ingesting a page.
type IngestRequest struct {
	URL     string
	Title   string
	Text    string
	VisitTs int64
	DwellMs int64
	Domain  string
}

// SearchResult holds a single search hit.
type SearchResult struct {
	URL     string
	Title   string
	Snippet string
	VisitTs int64
	Score   float64
	Domain  string
}

// ForgetRequest describes what to delete.
type ForgetRequest struct {
	Type  string // "url", "domain", "timerange"
	Value string // URL, domain name, or "fromMs:toMs"
}

// schemaV1 is the initial database schema (migration version 1).
const schemaV1 = `
CREATE TABLE IF NOT EXISTS pages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    url_hash TEXT NOT NULL UNIQUE,
    title TEXT DEFAULT '',
    domain TEXT DEFAULT '',
    visit_ts INTEGER NOT NULL,
    dwell_ms INTEGER NOT NULL DEFAULT 0,
    model_ver TEXT NOT NULL DEFAULT 'stub-v0',
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chunks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    chunk_idx INTEGER NOT NULL,
    text TEXT NOT NULL,
    text_hash TEXT NOT NULL,
    embedding BLOB,
    model_ver TEXT NOT NULL DEFAULT 'stub-v0',
    UNIQUE(page_id, chunk_idx)
);

CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    text,
    content='chunks',
    content_rowid='id'
);

CREATE TABLE IF NOT EXISTS queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    title TEXT DEFAULT '',
    text TEXT NOT NULL,
    visit_ts INTEGER NOT NULL,
    dwell_ms INTEGER NOT NULL DEFAULT 0,
    domain TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at INTEGER NOT NULL,
    updated_at INTEGER
);
`

// New opens (or creates) the SQLite database at dataDir/vbm.db.
func New(dataDir string, e embed.Embedder) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("mkdirall: %w", err)
	}
	dbPath := filepath.Join(dataDir, "vbm.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// P2-03: SQLite is single-writer — make the pool constraint explicit.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("wal mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("foreign keys: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db, embedder: e}, nil
}

// migrate applies all pending schema migrations in order.
// Schema versions are tracked in the schema_versions table.
func migrate(db *sql.DB) error {
	// Bootstrap version tracking table (always safe — IF NOT EXISTS).
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_versions (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_versions: %w", err)
	}

	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_versions`).Scan(&current); err != nil {
		return fmt.Errorf("get schema version: %w", err)
	}

	migrations := []struct {
		version int
		sql     string
	}{
		{1, schemaV1},
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			return fmt.Errorf("migrate to v%d: %w", m.version, err)
		}
		if _, err := db.Exec(
			`INSERT INTO schema_versions (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().UnixMilli(),
		); err != nil {
			return fmt.Errorf("record migration v%d: %w", m.version, err)
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Ingest chunks and stores a page, embedding each chunk.
func (s *Store) Ingest(req IngestRequest) error {
	chunks := chunk.SplitIntoChunks(req.Text)
	if chunks == nil {
		return nil // too short
	}

	urlHash := chunk.Hash(req.URL)
	now := time.Now().UnixMilli()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Upsert page
	res, err := tx.Exec(`
		INSERT INTO pages (url, url_hash, title, domain, visit_ts, dwell_ms, model_ver, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(url_hash) DO UPDATE SET
			title=excluded.title,
			domain=excluded.domain,
			visit_ts=excluded.visit_ts,
			dwell_ms=excluded.dwell_ms,
			model_ver=excluded.model_ver
	`, req.URL, urlHash, req.Title, req.Domain, req.VisitTs, req.DwellMs, s.embedder.Version(), now)
	if err != nil {
		return fmt.Errorf("upsert page: %w", err)
	}

	pageID, err := res.LastInsertId()
	if err != nil || pageID == 0 {
		// ON CONFLICT DO UPDATE doesn't return a new LastInsertId in all drivers; fetch it
		row := tx.QueryRow(`SELECT id FROM pages WHERE url_hash = ?`, urlHash)
		if err2 := row.Scan(&pageID); err2 != nil {
			return fmt.Errorf("get page id: %w", err2)
		}
	}

	for _, c := range chunks {
		vec, err := s.embedder.Embed(c.Text)
		if err != nil {
			return fmt.Errorf("embed chunk %d: %w", c.Index, err)
		}
		blob := embed.EncodeEmbedding(vec)

		// P0-03: use RowsAffected to detect new vs duplicate chunk.
		res, err := tx.Exec(`
			INSERT OR IGNORE INTO chunks (page_id, chunk_idx, text, text_hash, embedding, model_ver)
			VALUES (?, ?, ?, ?, ?, ?)
		`, pageID, c.Index, c.Text, c.Hash, blob, s.embedder.Version())
		if err != nil {
			return fmt.Errorf("insert chunk %d: %w", c.Index, err)
		}
		// Update FTS incrementally — only for newly inserted chunks.
		if n, _ := res.RowsAffected(); n > 0 {
			chunkID, _ := res.LastInsertId()
			if _, err := tx.Exec(`INSERT INTO chunks_fts(rowid, text) VALUES(?, ?)`, chunkID, c.Text); err != nil {
				return fmt.Errorf("fts insert chunk %d: %w", c.Index, err)
			}
		}
	}

	return tx.Commit()
}

type chunkRow struct {
	id     int64
	text   string
	pageID int64
}

// Search performs hybrid BM25 + cosine RRF search.
func (s *Store) Search(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	// Step 1: BM25 via FTS5
	bm25Chunks := []chunkRow{}
	bm25Ranks := map[int64]int{}

	rows, err := s.db.Query(`
		SELECT c.id, c.text, c.page_id, bm25(chunks_fts) as bm25_score
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		WHERE chunks_fts MATCH ?
		ORDER BY bm25_score
		LIMIT 50
	`, query)
	if err != nil && !strings.Contains(err.Error(), "no such table") {
		return nil, fmt.Errorf("fts query: %w", err)
	}
	if rows != nil {
		defer rows.Close()
		rank := 0
		for rows.Next() {
			var cr chunkRow
			var bm25Score float64
			if err := rows.Scan(&cr.id, &cr.text, &cr.pageID, &bm25Score); err != nil {
				continue
			}
			bm25Chunks = append(bm25Chunks, cr)
			bm25Ranks[cr.id] = rank
			rank++
		}
	}

	// Step 2: Dense search — skipped when embedder is stub (all-zero vectors).
	queryVec, err := s.embedder.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	denseRanks := map[int64]int{}

	// P0-05: detect stub/zero query vector to avoid O(N) full scan.
	isStub := true
	for _, v := range queryVec {
		if v != 0 {
			isStub = false
			break
		}
	}

	if !isStub {
		type scoredChunk struct {
			id     int64
			text   string
			pageID int64
			score  float32
		}

		allRows, err := s.db.Query(`SELECT id, text, page_id, embedding FROM chunks`)
		if err != nil {
			return nil, fmt.Errorf("load chunks: %w", err)
		}
		defer allRows.Close()

		var scored []scoredChunk
		for allRows.Next() {
			var id, pageID int64
			var text string
			var blob []byte
			if err := allRows.Scan(&id, &text, &pageID, &blob); err != nil {
				continue
			}
			vec := embed.DecodeEmbedding(blob)
			sim := embed.CosineSimilarity(queryVec, vec)
			scored = append(scored, scoredChunk{id: id, text: text, pageID: pageID, score: sim})
		}
		allRows.Close()

		sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
		cap50 := 50
		for i, sc := range scored {
			if i >= cap50 {
				break
			}
			denseRanks[sc.id] = i
		}
	}

	// Step 3: RRF fusion
	const k = 60
	rrfScores := rrf(bm25Ranks, denseRanks, k)

	// Sort by RRF score
	type rrfEntry struct {
		id    int64
		score float64
	}
	var entries []rrfEntry
	for id, score := range rrfScores {
		entries = append(entries, rrfEntry{id: id, score: score})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].score > entries[j].score })

	if len(entries) > limit {
		entries = entries[:limit]
	}

	// Step 4: Build results — single JOIN query instead of N+1.
	if len(entries) == 0 {
		return nil, nil
	}
	scoreByID := make(map[int64]float64, len(entries))
	ids := make([]any, len(entries))
	for i, e := range entries {
		ids[i] = e.id
		scoreByID[e.id] = e.score
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	joinQuery := fmt.Sprintf(`
		SELECT c.id, c.text, p.url, p.title, p.domain, p.visit_ts
		FROM chunks c JOIN pages p ON c.page_id = p.id
		WHERE c.id IN (%s)
	`, placeholders)
	joinRows, err := s.db.Query(joinQuery, ids...)
	if err != nil {
		return nil, fmt.Errorf("build results: %w", err)
	}
	defer joinRows.Close()

	// Collect into map to preserve RRF order after JOIN.
	type rowData struct {
		text    string
		url     string
		title   string
		domain  string
		visitTs int64
	}
	rowMap := make(map[int64]rowData, len(entries))
	for joinRows.Next() {
		var id int64
		var rd rowData
		if err := joinRows.Scan(&id, &rd.text, &rd.url, &rd.title, &rd.domain, &rd.visitTs); err != nil {
			continue
		}
		rowMap[id] = rd
	}

	results := make([]SearchResult, 0, len(entries))
	for _, e := range entries {
		rd, ok := rowMap[e.id]
		if !ok {
			continue
		}
		// P2-05: 400 chars gives better context for technical docs.
		snippet := rd.text
		if len(snippet) > 400 {
			snippet = snippet[:400]
		}
		results = append(results, SearchResult{
			URL:     rd.url,
			Title:   rd.title,
			Snippet: snippet,
			VisitTs: rd.visitTs,
			Score:   e.score,
			Domain:  rd.domain,
		})
	}
	return results, nil
}

func rrf(bm25Ranks, denseRanks map[int64]int, k int) map[int64]float64 {
	scores := make(map[int64]float64)
	for id, rank := range bm25Ranks {
		scores[id] += 1.0 / float64(k+rank)
	}
	for id, rank := range denseRanks {
		scores[id] += 1.0 / float64(k+rank)
	}
	return scores
}

// Forget deletes pages (and cascades to chunks) based on type.
// Also cleans matching rows from the queue table (P1-03).
func (s *Store) Forget(req ForgetRequest) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("forget tx: %w", err)
	}
	defer tx.Rollback()

	switch req.Type {
	case "url":
		if _, err = tx.Exec(`DELETE FROM pages WHERE url = ?`, req.Value); err != nil {
			return fmt.Errorf("forget pages url: %w", err)
		}
		if _, err = tx.Exec(`DELETE FROM queue WHERE url = ?`, req.Value); err != nil {
			return fmt.Errorf("forget queue url: %w", err)
		}
	case "domain":
		if _, err = tx.Exec(`DELETE FROM pages WHERE domain = ?`, req.Value); err != nil {
			return fmt.Errorf("forget pages domain: %w", err)
		}
		if _, err = tx.Exec(`DELETE FROM queue WHERE domain = ?`, req.Value); err != nil {
			return fmt.Errorf("forget queue domain: %w", err)
		}
	case "timerange":
		parts := strings.SplitN(req.Value, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid timerange value: %q", req.Value)
		}
		fromMs, err1 := strconv.ParseInt(parts[0], 10, 64)
		toMs, err2 := strconv.ParseInt(parts[1], 10, 64)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("invalid timerange numbers: %q", req.Value)
		}
		if _, err = tx.Exec(`DELETE FROM pages WHERE visit_ts >= ? AND visit_ts <= ?`, fromMs, toMs); err != nil {
			return fmt.Errorf("forget pages timerange: %w", err)
		}
		if _, err = tx.Exec(`DELETE FROM queue WHERE visit_ts >= ? AND visit_ts <= ?`, fromMs, toMs); err != nil {
			return fmt.Errorf("forget queue timerange: %w", err)
		}
	default:
		return fmt.Errorf("unknown forget type: %q", req.Type)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("forget commit: %w", err)
	}
	_, err = s.db.Exec(`INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')`)
	return err
}

// Cleanup deletes pages (and their chunks via CASCADE) older than ttlDays days.
// Returns the number of pages deleted. ttlDays <= 0 is a no-op.
func (s *Store) Cleanup(ttlDays int) (int64, error) {
	if ttlDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -ttlDays).UnixMilli()
	res, err := s.db.Exec(`DELETE FROM pages WHERE visit_ts < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		if _, err := s.db.Exec(`INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')`); err != nil {
			return n, fmt.Errorf("cleanup fts rebuild: %w", err)
		}
	}
	return n, nil
}

// GetStatus returns the number of indexed pages and pending queue items.
func (s *Store) GetStatus() (indexed int, pending int, err error) {
	row := s.db.QueryRow(`SELECT COUNT(*) FROM pages`)
	if err = row.Scan(&indexed); err != nil {
		return
	}
	row = s.db.QueryRow(`SELECT COUNT(*) FROM queue WHERE status = 'pending'`)
	err = row.Scan(&pending)
	return
}

// Ping verifies the database connection is alive and readable.
func (s *Store) Ping() error {
	var n int
	return s.db.QueryRow(`SELECT 1`).Scan(&n)
}

// ExportChunk holds a single chunk for export.
type ExportChunk struct {
	ChunkIdx int    `json:"chunkIdx"`
	Text     string `json:"text"`
}

// ExportPage holds a page and all its chunks for export.
type ExportPage struct {
	URL     string        `json:"url"`
	Title   string        `json:"title"`
	Domain  string        `json:"domain"`
	VisitTs int64         `json:"visitTs"`
	DwellMs int64         `json:"dwellMs"`
	Chunks  []ExportChunk `json:"chunks"`
}

// AddQueueItem persists an ingest request in the queue table with status='pending'.
// P2-02: makes pending count in GetStatus() accurate and enables cleanup.
func (s *Store) AddQueueItem(req IngestRequest) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`
		INSERT INTO queue (url, title, text, visit_ts, dwell_ms, domain, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)
	`, req.URL, req.Title, req.Text, req.VisitTs, req.DwellMs, req.Domain, now)
	if err != nil {
		return fmt.Errorf("add queue item: %w", err)
	}
	return nil
}

// RemoveQueueItem deletes all pending queue rows for a given URL after successful ingest.
// P2-02: prevents the queue table from accumulating processed entries indefinitely.
func (s *Store) RemoveQueueItem(url string) error {
	_, err := s.db.Exec(`DELETE FROM queue WHERE url = ? AND status = 'pending'`, url)
	if err != nil {
		return fmt.Errorf("remove queue item: %w", err)
	}
	return nil
}

// Export returns all indexed pages with their chunks for LGPD portability.
func (s *Store) Export() ([]ExportPage, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.url, p.title, p.domain, p.visit_ts, p.dwell_ms,
		       c.chunk_idx, c.text
		FROM pages p
		LEFT JOIN chunks c ON c.page_id = p.id
		ORDER BY p.visit_ts DESC, c.chunk_idx ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("export query: %w", err)
	}
	defer rows.Close()

	pageMap := make(map[int64]*ExportPage)
	var order []int64 // preserve visit_ts DESC order
	for rows.Next() {
		var pageID int64
		var url, title, domain string
		var visitTs, dwellMs int64
		var chunkIdx sql.NullInt64
		var chunkText sql.NullString
		if err := rows.Scan(&pageID, &url, &title, &domain, &visitTs, &dwellMs, &chunkIdx, &chunkText); err != nil {
			continue
		}
		if _, seen := pageMap[pageID]; !seen {
			pageMap[pageID] = &ExportPage{
				URL:     url,
				Title:   title,
				Domain:  domain,
				VisitTs: visitTs,
				DwellMs: dwellMs,
				Chunks:  []ExportChunk{},
			}
			order = append(order, pageID)
		}
		if chunkIdx.Valid && chunkText.Valid {
			pageMap[pageID].Chunks = append(pageMap[pageID].Chunks, ExportChunk{
				ChunkIdx: int(chunkIdx.Int64),
				Text:     chunkText.String,
			})
		}
	}

	result := make([]ExportPage, 0, len(order))
	for _, id := range order {
		result = append(result, *pageMap[id])
	}
	return result, nil
}
