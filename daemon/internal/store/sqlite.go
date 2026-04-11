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

const schema = `
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

func migrate(db *sql.DB) error {
	_, err := db.Exec(schema)
	return err
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

		_, err = tx.Exec(`
			INSERT OR IGNORE INTO chunks (page_id, chunk_idx, text, text_hash, embedding, model_ver)
			VALUES (?, ?, ?, ?, ?, ?)
		`, pageID, c.Index, c.Text, c.Hash, blob, s.embedder.Version())
		if err != nil {
			return fmt.Errorf("insert chunk %d: %w", c.Index, err)
		}
	}

	// Rebuild FTS index
	if _, err := tx.Exec(`INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')`); err != nil {
		return fmt.Errorf("fts rebuild: %w", err)
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

	// Step 2: Dense search
	queryVec, err := s.embedder.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

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
	denseRanks := map[int64]int{}
	cap50 := 50
	for i, sc := range scored {
		if i >= cap50 {
			break
		}
		denseRanks[sc.id] = i
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

	// Step 4: Build results
	results := make([]SearchResult, 0, len(entries))
	for _, e := range entries {
		var text string
		var pageID int64
		row := s.db.QueryRow(`SELECT text, page_id FROM chunks WHERE id = ?`, e.id)
		if err := row.Scan(&text, &pageID); err != nil {
			continue
		}
		var url, title, domain string
		var visitTs int64
		prow := s.db.QueryRow(`SELECT url, title, domain, visit_ts FROM pages WHERE id = ?`, pageID)
		if err := prow.Scan(&url, &title, &domain, &visitTs); err != nil {
			continue
		}
		snippet := text
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		results = append(results, SearchResult{
			URL:     url,
			Title:   title,
			Snippet: snippet,
			VisitTs: visitTs,
			Score:   e.score,
			Domain:  domain,
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
func (s *Store) Forget(req ForgetRequest) error {
	var err error
	switch req.Type {
	case "url":
		_, err = s.db.Exec(`DELETE FROM pages WHERE url = ?`, req.Value)
	case "domain":
		_, err = s.db.Exec(`DELETE FROM pages WHERE domain = ?`, req.Value)
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
		_, err = s.db.Exec(`DELETE FROM pages WHERE visit_ts >= ? AND visit_ts <= ?`, fromMs, toMs)
	default:
		return fmt.Errorf("unknown forget type: %q", req.Type)
	}
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')`)
	return err
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
