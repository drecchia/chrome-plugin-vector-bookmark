package store

import (
	"database/sql"
	"fmt"
	"log/slog"
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

// VisitRequest holds data for recording a passive page visit (no text/embedding).
type VisitRequest struct {
	URL      string
	Title    string
	VisitTs  int64
	DwellMs  int64
	Domain   string
	MetaJSON string
}

// IngestRequest holds data for ingesting a page (manual full-index).
type IngestRequest struct {
	URL     string
	Title   string
	Text    string
	VisitTs int64
	DwellMs int64
	Domain  string
	Tags    []string
	// SetTags=true replaces the page's tag set with Tags exactly (delete + insert).
	// SetTags=false (default) merges Tags into existing (INSERT OR IGNORE only).
	SetTags bool
	// Source labels how the page entered the index. "indexed" = explicit
	// /ingest from the popup; "history" = passive /visit dwell capture.
	// Empty defaults to "history" at persistence time.
	Source string
}

// TagCount holds a tag and the number of pages that carry it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// SearchResult holds a single search hit. One result represents one page;
// when several chunks of the same page match, their texts are merged into
// Snippets (top-ranked first) and Snippet keeps the best for backward compat.
type SearchResult struct {
	URL      string
	Title    string
	Snippet  string
	Snippets []string
	VisitTs  int64
	Score    float64
	Domain   string
	Tags     []string
	Source   string // "indexed" | "history"
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
    indexed INTEGER NOT NULL DEFAULT 0,
    meta_json TEXT DEFAULT '',
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

// LoadPendingItems returns all queue rows with status='pending' so they can be
// re-enqueued after a daemon restart. Items that were in-flight when the daemon
// crashed are recovered this way.
func (s *Store) LoadPendingItems() ([]IngestRequest, error) {
	rows, err := s.db.Query(`
		SELECT url, title, text, visit_ts, dwell_ms, domain
		FROM queue WHERE status = 'pending' ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("load pending: %w", err)
	}
	defer rows.Close()
	var items []IngestRequest
	for rows.Next() {
		var r IngestRequest
		if err := rows.Scan(&r.URL, &r.Title, &r.Text, &r.VisitTs, &r.DwellMs, &r.Domain); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		items = append(items, r)
	}
	return items, rows.Err()
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

	// schemaV2: upgrade FTS5 to porter stemmer for better BM25 recall.
	// DROP + recreate is required — FTS5 virtual tables don't support ALTER TABLE.
	const schemaV2 = `
DROP TABLE IF EXISTS chunks_fts;
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    text,
    content='chunks',
    content_rowid='id',
    tokenize='porter unicode61'
);
INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild');
`
	const schemaV3 = `ALTER TABLE pages ADD COLUMN star_rank INTEGER NOT NULL DEFAULT 0;`
	const schemaV4 = `
CREATE TABLE IF NOT EXISTS blacklist (
    pattern    TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL
);`
	// schemaV5: drop unused star_rank column (never referenced by any query).
	// DROP COLUMN requires SQLite ≥3.35; modernc.org/sqlite ships a newer engine.
	// Wrapped in a best-effort block — if a fresh DB was created after V3 was
	// rolled back, the column may not exist; tolerate that silently.
	const schemaV5 = `ALTER TABLE pages DROP COLUMN star_rank;`
	// schemaV6: index chunks(page_id) for fast cascade/joins.
	const schemaV6 = `CREATE INDEX IF NOT EXISTS idx_chunks_page_id ON chunks(page_id);`
	// schemaV7: per-page tags for user curation (read-later, reference, …).
	const schemaV7 = `
CREATE TABLE IF NOT EXISTS page_tags (
    page_id    INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    tag        TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (page_id, tag)
);
CREATE INDEX IF NOT EXISTS idx_page_tags_tag ON page_tags(tag);`
	// schemaV8: tag the origin of each page so the UI can distinguish
	// passive dwell captures ("history") from explicit popup ingests
	// ("indexed"). Existing rows default to "history".
	const schemaV8 = `ALTER TABLE pages ADD COLUMN source TEXT NOT NULL DEFAULT 'history';`

	migrations := []struct {
		version    int
		sql        string
		ignoreErrs bool
	}{
		{1, schemaV1, false},
		{2, schemaV2, false},
		{3, schemaV3, false},
		{4, schemaV4, false},
		{5, schemaV5, true}, // column may already be absent on recent fresh DBs
		{6, schemaV6, false},
		{7, schemaV7, false},
		{8, schemaV8, false},
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			if m.ignoreErrs {
				slog.Warn("migration tolerated error", "version", m.version, "err", err)
			} else {
				return fmt.Errorf("migrate to v%d: %w", m.version, err)
			}
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

// RecordVisit upserts a page with metadata only (no text, no chunking, no embedding).
func (s *Store) RecordVisit(req VisitRequest) error {
	urlHash := chunk.Hash(req.URL)
	now := time.Now().UnixMilli()

	_, err := s.db.Exec(`
		INSERT INTO pages (url, url_hash, title, domain, visit_ts, dwell_ms, model_ver, indexed, meta_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'stub-v0', 0, ?, ?)
		ON CONFLICT(url_hash) DO UPDATE SET
			title=excluded.title,
			domain=excluded.domain,
			visit_ts=excluded.visit_ts,
			dwell_ms=excluded.dwell_ms,
			meta_json=excluded.meta_json,
			indexed=MAX(indexed, 0)
	`, req.URL, urlHash, req.Title, req.Domain, req.VisitTs, req.DwellMs, req.MetaJSON, now)
	if err != nil {
		return fmt.Errorf("record visit: %w", err)
	}
	return nil
}

// persistTags applies tag changes for a page inside the caller's transaction.
// SetTags=true makes req.Tags the authoritative list (DELETE + INSERT). Otherwise
// only INSERT OR IGNORE happens (merge mode). Tags are normalized; empty entries
// are dropped silently.
func persistTags(tx *sql.Tx, pageID int64, req IngestRequest, now int64) error {
	wantTags := make(map[string]struct{}, len(req.Tags))
	for _, t := range req.Tags {
		if nt := normalizeTag(t); nt != "" {
			wantTags[nt] = struct{}{}
		}
	}
	if req.SetTags {
		if len(wantTags) == 0 {
			if _, err := tx.Exec(`DELETE FROM page_tags WHERE page_id = ?`, pageID); err != nil {
				return fmt.Errorf("clear tags: %w", err)
			}
		} else {
			args := make([]any, 0, len(wantTags)+1)
			args = append(args, pageID)
			placeholders := make([]string, 0, len(wantTags))
			for t := range wantTags {
				placeholders = append(placeholders, "?")
				args = append(args, t)
			}
			delSQL := fmt.Sprintf(
				`DELETE FROM page_tags WHERE page_id = ? AND tag NOT IN (%s)`,
				strings.Join(placeholders, ","),
			)
			if _, err := tx.Exec(delSQL, args...); err != nil {
				return fmt.Errorf("delete stale tags: %w", err)
			}
		}
	}
	for t := range wantTags {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO page_tags (page_id, tag, created_at) VALUES (?, ?, ?)`,
			pageID, t, now,
		); err != nil {
			return fmt.Errorf("insert tag %q: %w", t, err)
		}
	}
	return nil
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

	source := req.Source
	if source == "" {
		source = "history"
	}
	// Upsert page — set indexed=1 since this is a full ingest with text.
	// Source is upgraded to "indexed" only when the request says so; a
	// passive history hit must never demote a previous "indexed" mark.
	if _, err := tx.Exec(`
		INSERT INTO pages (url, url_hash, title, domain, visit_ts, dwell_ms, model_ver, indexed, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(url_hash) DO UPDATE SET
			title=excluded.title,
			domain=excluded.domain,
			visit_ts=excluded.visit_ts,
			dwell_ms=excluded.dwell_ms,
			model_ver=excluded.model_ver,
			indexed=1,
			source=CASE WHEN excluded.source='indexed' THEN 'indexed' ELSE pages.source END
	`, req.URL, urlHash, req.Title, req.Domain, req.VisitTs, req.DwellMs, s.embedder.Version(), source, now); err != nil {
		return fmt.Errorf("upsert page: %w", err)
	}

	// LastInsertId() is unreliable across drivers after ON CONFLICT DO UPDATE
	// (modernc/sqlite may return a stale rowid). Always fetch by url_hash.
	var pageID int64
	if err := tx.QueryRow(`SELECT id FROM pages WHERE url_hash = ?`, urlHash).Scan(&pageID); err != nil {
		return fmt.Errorf("get page id: %w", err)
	}

	if err := persistTags(tx, pageID, req, now); err != nil {
		return err
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

// PreparedChunk is a chunk with its embedding already computed.
// Produced by queue workers; consumed by the single DB-writer goroutine.
type PreparedChunk struct {
	Index int
	Text  string
	Hash  string
	Vec   []float32
}

// PreparedIngest bundles a page + pre-embedded chunks for atomic DB write.
type PreparedIngest struct {
	Req    IngestRequest
	Chunks []PreparedChunk
}

// Embedder returns the active embedder — exposed for queue workers that need
// to embed chunks off the hot write path.
func (s *Store) Embedder() embed.Embedder { return s.embedder }

// IngestPrepared writes a pre-embedded PreparedIngest in a single transaction.
// No embedding happens here — this runs on the single DB-writer goroutine.
//
// Accepts Chunks=nil for tag-only updates (text was too short for chunking
// but the request carries a tag delta). In that case the page row is upserted
// without promoting `indexed` and the chunk loop is skipped — the goal is to
// land req.Tags / req.SetTags so they don't get silently dropped.
func (s *Store) IngestPrepared(p PreparedIngest) error {
	req := p.Req
	hasChunks := len(p.Chunks) > 0
	hasTagDelta := req.SetTags || len(req.Tags) > 0
	if !hasChunks && !hasTagDelta {
		return nil
	}
	urlHash := chunk.Hash(req.URL)
	now := time.Now().UnixMilli()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	source := req.Source
	if source == "" {
		source = "history"
	}
	insertIndexed := 0
	if hasChunks {
		insertIndexed = 1
	}
	// On conflict, never demote indexed (MAX with existing) and only set
	// excluded values when this run actually has chunks; otherwise keep prior.
	if _, err := tx.Exec(`
		INSERT INTO pages (url, url_hash, title, domain, visit_ts, dwell_ms, model_ver, indexed, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(url_hash) DO UPDATE SET
			title=excluded.title,
			domain=excluded.domain,
			visit_ts=excluded.visit_ts,
			dwell_ms=excluded.dwell_ms,
			model_ver=CASE WHEN excluded.indexed=1 THEN excluded.model_ver ELSE pages.model_ver END,
			indexed=CASE WHEN excluded.indexed=1 OR pages.indexed=1 THEN 1 ELSE 0 END,
			source=CASE WHEN excluded.source='indexed' THEN 'indexed' ELSE pages.source END
	`, req.URL, urlHash, req.Title, req.Domain, req.VisitTs, req.DwellMs, s.embedder.Version(), insertIndexed, source, now); err != nil {
		return fmt.Errorf("upsert page: %w", err)
	}
	// LastInsertId() is unreliable across drivers after ON CONFLICT DO UPDATE
	// (modernc/sqlite may return a stale rowid). Always fetch by url_hash.
	var pageID int64
	if err := tx.QueryRow(`SELECT id FROM pages WHERE url_hash = ?`, urlHash).Scan(&pageID); err != nil {
		return fmt.Errorf("get page id: %w", err)
	}

	if err := persistTags(tx, pageID, req, now); err != nil {
		return err
	}

	for _, c := range p.Chunks {
		blob := embed.EncodeEmbedding(c.Vec)
		cres, err := tx.Exec(`
			INSERT OR IGNORE INTO chunks (page_id, chunk_idx, text, text_hash, embedding, model_ver)
			VALUES (?, ?, ?, ?, ?, ?)
		`, pageID, c.Index, c.Text, c.Hash, blob, s.embedder.Version())
		if err != nil {
			return fmt.Errorf("insert chunk %d: %w", c.Index, err)
		}
		if n, _ := cres.RowsAffected(); n > 0 {
			chunkID, _ := cres.LastInsertId()
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

// SearchOpts collects optional filters for Search.
type SearchOpts struct {
	Limit         int      // capped server-side (default 5, max 20)
	Tags          []string // include AND: page must carry every tag
	NegTags       []string // exclude OR: page is dropped if it carries any of these
	MinConfidence float64  // [0,1] — keep only results with score/topScore >= this
	Source        string   // "indexed" | "history" | "" (= both)
}

// Search performs hybrid BM25 + cosine RRF search with the filters from opts.
func (s *Store) Search(query string, opts SearchOpts) ([]SearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	// Normalize tag filters.
	tags := normalizeTagSlice(opts.Tags)
	negTags := normalizeTagSlice(opts.NegTags)
	source := strings.TrimSpace(opts.Source)
	if source != "indexed" && source != "history" {
		source = ""
	}

	// Build a reusable page-id WHERE fragment for both BM25 and dense paths.
	pageFilterSQL, pageFilterArgs := buildPageFilter(tags, negTags, source)

	// Step 1: BM25 via FTS5
	bm25Chunks := []chunkRow{}
	bm25Ranks := map[int64]int{}

	bm25SQL := `
		SELECT c.id, c.text, c.page_id, bm25(chunks_fts) as bm25_score
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		WHERE chunks_fts MATCH ?`
	bm25Args := []any{query}
	if pageFilterSQL != "" {
		bm25SQL += " AND " + pageFilterSQL
		bm25Args = append(bm25Args, pageFilterArgs...)
	}
	bm25SQL += `
		ORDER BY bm25_score
		LIMIT 50`
	rows, err := s.db.Query(bm25SQL, bm25Args...)
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

		denseSQL := `SELECT id, text, page_id, embedding FROM chunks`
		var denseArgs []any
		if pageFilterSQL != "" {
			denseSQL += " WHERE " + strings.ReplaceAll(pageFilterSQL, "c.page_id", "page_id")
			denseArgs = append(denseArgs, pageFilterArgs...)
		}
		allRows, err := s.db.Query(denseSQL, denseArgs...)
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

	// Keep up to limit*5 chunks so page-level aggregation has enough material;
	// final cap applies after grouping by page_id.
	chunkCap := limit * 5
	if chunkCap < 50 {
		chunkCap = 50
	}
	if len(entries) > chunkCap {
		entries = entries[:chunkCap]
	}

	if len(entries) == 0 {
		return nil, nil
	}
	ids := make([]any, len(entries))
	for i, e := range entries {
		ids[i] = e.id
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	joinQuery := fmt.Sprintf(`
		SELECT c.id, c.page_id, c.text, p.url, p.title, p.domain, p.visit_ts, p.source
		FROM chunks c JOIN pages p ON c.page_id = p.id
		WHERE c.id IN (%s)
	`, placeholders)
	joinRows, err := s.db.Query(joinQuery, ids...)
	if err != nil {
		return nil, fmt.Errorf("build results: %w", err)
	}
	defer joinRows.Close()

	type rowData struct {
		pageID  int64
		text    string
		url     string
		title   string
		domain  string
		visitTs int64
		source  string
	}
	rowMap := make(map[int64]rowData, len(entries))
	for joinRows.Next() {
		var id int64
		var rd rowData
		if err := joinRows.Scan(&id, &rd.pageID, &rd.text, &rd.url, &rd.title, &rd.domain, &rd.visitTs, &rd.source); err != nil {
			continue
		}
		rowMap[id] = rd
	}

	// Group chunks by page_id, preserving RRF order within each page.
	type pageAgg struct {
		url, title, domain, source string
		visitTs                    int64
		bestScore                  float64
		extras                     int
		snippets                   []string
	}
	pages := make(map[int64]*pageAgg)
	pageOrder := []int64{}
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
		ag, exists := pages[rd.pageID]
		if !exists {
			pages[rd.pageID] = &pageAgg{
				url:       rd.url,
				title:     rd.title,
				domain:    rd.domain,
				source:    rd.source,
				visitTs:   rd.visitTs,
				bestScore: e.score,
				snippets:  []string{snippet},
			}
			pageOrder = append(pageOrder, rd.pageID)
			continue
		}
		// Same page already seen with a higher RRF score — keep best score
		// plus a small coverage bonus (10% of this chunk's score) per extra
		// match, capped so it cannot overtake a stronger top-1 page.
		ag.extras++
		ag.bestScore += 0.1 * e.score
		if len(ag.snippets) < 3 {
			ag.snippets = append(ag.snippets, snippet)
		}
	}

	// Sort pages by aggregated score, then truncate to limit.
	sort.SliceStable(pageOrder, func(i, j int) bool {
		return pages[pageOrder[i]].bestScore > pages[pageOrder[j]].bestScore
	})
	if len(pageOrder) > limit {
		pageOrder = pageOrder[:limit]
	}

	// Batch-fetch tags for the surviving pages.
	tagsByPage := make(map[int64][]string, len(pageOrder))
	if len(pageOrder) > 0 {
		pageIDArgs := make([]any, len(pageOrder))
		for i, pid := range pageOrder {
			pageIDArgs[i] = pid
		}
		tagPH := strings.Repeat("?,", len(pageIDArgs))
		tagPH = tagPH[:len(tagPH)-1]
		tagSQL := fmt.Sprintf(`SELECT page_id, tag FROM page_tags WHERE page_id IN (%s) ORDER BY tag ASC`, tagPH)
		tagRows, err := s.db.Query(tagSQL, pageIDArgs...)
		if err == nil {
			for tagRows.Next() {
				var pid int64
				var t string
				if err := tagRows.Scan(&pid, &t); err != nil {
					continue
				}
				tagsByPage[pid] = append(tagsByPage[pid], t)
			}
			tagRows.Close()
		}
	}

	results := make([]SearchResult, 0, len(pageOrder))
	for _, pid := range pageOrder {
		ag := pages[pid]
		first := ""
		if len(ag.snippets) > 0 {
			first = ag.snippets[0]
		}
		src := ag.source
		if src == "" {
			src = "history"
		}
		results = append(results, SearchResult{
			URL:      ag.url,
			Title:    ag.title,
			Snippet:  first,
			Snippets: ag.snippets,
			VisitTs:  ag.visitTs,
			Score:    ag.bestScore,
			Domain:   ag.domain,
			Tags:     tagsByPage[pid],
			Source:   src,
		})
	}

	// Apply user-controlled relative confidence cutoff. Top result is always
	// kept; the rest are dropped when score/topScore < opts.MinConfidence.
	if opts.MinConfidence > 0 && len(results) > 1 {
		top := results[0].Score
		if top > 0 {
			kept := results[:1]
			for _, r := range results[1:] {
				if r.Score/top >= opts.MinConfidence {
					kept = append(kept, r)
				}
			}
			results = kept
		}
	}
	return results, nil
}

// HistoryRow is a raw row returned by GetPageHistoryRows.
type HistoryRow struct {
	PageID  int64
	URL     string
	Title   string
	Domain  string
	VisitTs int64
	Text    string
}

// GetDailyPageCounts returns a UTC date-keyed (YYYY-MM-DD) count of pages
// visited in [fromMs, toMs). Independent of any page-list limit — used by the
// Timeline UI to render accurate per-day bar heights even when the page list
// is paginated.
func (s *Store) GetDailyPageCounts(fromMs, toMs int64) (map[string]int, error) {
	rows, err := s.db.Query(`
		SELECT strftime('%Y-%m-%d', visit_ts/1000, 'unixepoch') AS day, COUNT(*)
		FROM pages
		WHERE visit_ts >= ? AND visit_ts < ?
		GROUP BY day
	`, fromMs, toMs)
	if err != nil {
		return nil, fmt.Errorf("daily page counts: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var day string
		var count int
		if err := rows.Scan(&day, &count); err != nil {
			continue
		}
		out[day] = count
	}
	return out, nil
}

// GetPageHistoryRows returns chunk texts for the top `limit` pages visited in
// [fromMs, toMs), ordered by visit_ts DESC. The caller groups by PageID and
// extracts keywords per page.
func (s *Store) GetPageHistoryRows(fromMs, toMs int64, limit int) ([]HistoryRow, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.url, p.title, p.domain, p.visit_ts, COALESCE(c.text, '')
		FROM pages p
		LEFT JOIN chunks c ON c.page_id = p.id
		WHERE p.id IN (
			SELECT id FROM pages
			WHERE visit_ts >= ? AND visit_ts < ?
			ORDER BY visit_ts DESC
			LIMIT ?
		)
		ORDER BY p.visit_ts DESC
	`, fromMs, toMs, limit)
	if err != nil {
		return nil, fmt.Errorf("page history rows: %w", err)
	}
	defer rows.Close()
	var out []HistoryRow
	for rows.Next() {
		var r HistoryRow
		if err := rows.Scan(&r.PageID, &r.URL, &r.Title, &r.Domain, &r.VisitTs, &r.Text); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
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

// GetChunkTextByPeriod returns all chunk texts for pages visited in [fromMs, toMs).
func (s *Store) GetChunkTextByPeriod(fromMs, toMs int64) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT c.text FROM chunks c
		JOIN pages p ON c.page_id = p.id
		WHERE p.visit_ts >= ? AND p.visit_ts < ?
	`, fromMs, toMs)
	if err != nil {
		return nil, fmt.Errorf("chunk text by period: %w", err)
	}
	defer rows.Close()
	var texts []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			continue
		}
		texts = append(texts, t)
	}
	return texts, nil
}

// ChunkForReindex holds the minimal data needed to re-embed a chunk.
type ChunkForReindex struct {
	ID   int64
	Text string
}

// GetChunksForReindex returns all chunks that still carry stub-v0 embeddings.
func (s *Store) GetChunksForReindex() ([]ChunkForReindex, error) {
	rows, err := s.db.Query(`SELECT id, text FROM chunks WHERE model_ver = 'stub-v0'`)
	if err != nil {
		return nil, fmt.Errorf("get chunks for reindex: %w", err)
	}
	defer rows.Close()
	var chunks []ChunkForReindex
	for rows.Next() {
		var c ChunkForReindex
		if err := rows.Scan(&c.ID, &c.Text); err != nil {
			continue
		}
		chunks = append(chunks, c)
	}
	return chunks, nil
}

// UpdateChunkEmbedding replaces the embedding and model_ver for one chunk.
func (s *Store) UpdateChunkEmbedding(id int64, vec []float32, modelVer string) error {
	blob := embed.EncodeEmbedding(vec)
	_, err := s.db.Exec(`UPDATE chunks SET embedding = ?, model_ver = ? WHERE id = ?`, blob, modelVer, id)
	return err
}

// EmbedderVersion returns the active embedder's version string.
func (s *Store) EmbedderVersion() string { return s.embedder.Version() }

// ReindexChunks re-embeds all stub-v0 chunks using the current embedder.
// progress is called after each successful update with (done, total).
// Returns the count of successfully re-embedded chunks.
func (s *Store) ReindexChunks(progress func(done, total int)) (int, error) {
	chunks, err := s.GetChunksForReindex()
	if err != nil {
		return 0, err
	}
	total := len(chunks)
	done := 0
	for _, c := range chunks {
		vec, err := s.embedder.Embed(c.Text)
		if err != nil {
			slog.Warn("reindex embed failed", "chunk_id", c.ID, "err", err)
			continue
		}
		if err := s.UpdateChunkEmbedding(c.ID, vec, s.embedder.Version()); err != nil {
			slog.Warn("reindex update failed", "chunk_id", c.ID, "err", err)
			continue
		}
		done++
		if progress != nil {
			progress(done, total)
		}
	}
	return done, nil
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

// PageStatus returns whether a page exists and whether it has been fully indexed.
func (s *Store) PageStatus(rawURL string) (exists bool, indexed bool, err error) {
	hash := chunk.Hash(rawURL)
	var idx int
	err = s.db.QueryRow(`SELECT indexed FROM pages WHERE url_hash = ?`, hash).Scan(&idx)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, idx == 1, nil
}

// GetBlacklist returns all patterns in the user-managed domain blacklist.
func (s *Store) GetBlacklist() ([]string, error) {
	rows, err := s.db.Query(`SELECT pattern FROM blacklist ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("get blacklist: %w", err)
	}
	defer rows.Close()
	var patterns []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			continue
		}
		patterns = append(patterns, p)
	}
	return patterns, nil
}

// AddToBlacklist adds a pattern to the blacklist (idempotent).
func (s *Store) AddToBlacklist(pattern string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO blacklist (pattern, created_at) VALUES (?, ?)`,
		pattern, time.Now().UnixMilli(),
	)
	return err
}

// RemoveFromBlacklist removes a pattern from the blacklist.
func (s *Store) RemoveFromBlacklist(pattern string) error {
	_, err := s.db.Exec(`DELETE FROM blacklist WHERE pattern = ?`, pattern)
	return err
}

// GetStatus returns the number of indexed pages and pending queue items.
func (s *Store) GetStatus() (visited int, indexed int, pending int, err error) {
	row := s.db.QueryRow(`SELECT COUNT(*) FROM pages`)
	if err = row.Scan(&visited); err != nil {
		return
	}
	row = s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE indexed = 1`)
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

// NormalizeTag is the exported entry point for the same normalization rules
// used at ingest time. Callers outside the package (e.g. /tags/suggest in the
// server) use this so suggested tags match ingest semantics exactly.
func NormalizeTag(tag string) string { return normalizeTag(tag) }

// normalizeTagSlice runs every entry through normalizeTag, drops empties,
// and dedupes (preserving order). Used by Search filters.
func normalizeTagSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		nt := normalizeTag(t)
		if nt == "" {
			continue
		}
		if _, ok := seen[nt]; ok {
			continue
		}
		seen[nt] = struct{}{}
		out = append(out, nt)
	}
	return out
}

// buildPageFilter returns an SQL fragment (without leading AND/WHERE) and the
// args that filter chunks/pages by tag inclusion (AND), tag exclusion (NOT IN),
// and source. Returns ("", nil) when no filter is requested. The fragment uses
// `c.page_id` so callers can splice into BM25 (chunk-joined) or dense (chunks)
// queries via a simple ReplaceAll if needed.
func buildPageFilter(tags, negTags []string, source string) (string, []any) {
	clauses := []string{}
	args := []any{}
	if len(tags) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(tags)), ",")
		clauses = append(clauses, fmt.Sprintf(
			`c.page_id IN (SELECT page_id FROM page_tags WHERE tag IN (%s) GROUP BY page_id HAVING COUNT(DISTINCT tag) = %d)`,
			ph, len(tags),
		))
		for _, t := range tags {
			args = append(args, t)
		}
	}
	if len(negTags) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(negTags)), ",")
		clauses = append(clauses, fmt.Sprintf(
			`c.page_id NOT IN (SELECT page_id FROM page_tags WHERE tag IN (%s))`,
			ph,
		))
		for _, t := range negTags {
			args = append(args, t)
		}
	}
	if source != "" {
		clauses = append(clauses, `c.page_id IN (SELECT id FROM pages WHERE source = ?)`)
		args = append(args, source)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return strings.Join(clauses, " AND "), args
}

// normalizeTag lowercases, trims and validates a tag.
// Returns "" if the tag is empty or contains only invalid characters.
// Allowed: a-z, 0-9, '-', '_', spaces. Max length 64.
func normalizeTag(tag string) string {
	t := strings.ToLower(strings.TrimSpace(tag))
	if t == "" {
		return ""
	}
	if len(t) > 64 {
		t = t[:64]
	}
	b := make([]byte, 0, len(t))
	for i := 0; i < len(t); i++ {
		c := t[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == ' ' {
			b = append(b, c)
		}
	}
	return strings.TrimSpace(string(b))
}

// UpdatePageTags replaces the tag set of the page identified by URL with the
// supplied list (set-mode: DELETE existing not in new + INSERT OR IGNORE new).
// Returns sql.ErrNoRows when the page is not in the index. Returns the final
// (normalized, sorted) tags on success.
func (s *Store) UpdatePageTags(rawURL string, tags []string) ([]string, error) {
	urlHash := chunk.Hash(rawURL)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	var pageID int64
	if err := tx.QueryRow(`SELECT id FROM pages WHERE url_hash = ?`, urlHash).Scan(&pageID); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("lookup page: %w", err)
	}
	now := time.Now().UnixMilli()
	if err := persistTags(tx, pageID, IngestRequest{Tags: tags, SetTags: true}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return s.GetPageTags(rawURL)
}

// GetPageTags returns the tags currently assigned to the page identified by URL.
// Returns an empty slice (not nil) when the page does not exist.
func (s *Store) GetPageTags(rawURL string) ([]string, error) {
	urlHash := chunk.Hash(rawURL)
	rows, err := s.db.Query(`
		SELECT pt.tag
		FROM page_tags pt
		JOIN pages p ON p.id = pt.page_id
		WHERE p.url_hash = ?
		ORDER BY pt.tag
	`, urlHash)
	if err != nil {
		return nil, fmt.Errorf("get page tags: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTags returns all distinct tags with the count of pages per tag, sorted by tag.
func (s *Store) ListTags() ([]TagCount, error) {
	rows, err := s.db.Query(`
		SELECT tag, COUNT(*) AS n
		FROM page_tags
		GROUP BY tag
		ORDER BY tag ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// TaggedPage holds a page row enriched with its tags for the /pages?tag= listing.
type TaggedPage struct {
	URL     string   `json:"url"`
	Title   string   `json:"title"`
	Domain  string   `json:"domain"`
	VisitTs int64    `json:"visitTs"`
	Tags    []string `json:"tags"`
}

// ListPagesByTag returns pages carrying the given tag, newest first, with all their tags.
func (s *Store) ListPagesByTag(tag string, limit int) ([]TaggedPage, error) {
	tag = normalizeTag(tag)
	if tag == "" {
		return []TaggedPage{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT p.id, p.url, p.title, p.domain, p.visit_ts
		FROM pages p
		JOIN page_tags pt ON pt.page_id = p.id
		WHERE pt.tag = ?
		ORDER BY p.visit_ts DESC
		LIMIT ?
	`, tag, limit)
	if err != nil {
		return nil, fmt.Errorf("list pages by tag: %w", err)
	}
	defer rows.Close()

	type rowEntry struct {
		id      int64
		url     string
		title   string
		domain  string
		visitTs int64
	}
	var entries []rowEntry
	ids := []any{}
	for rows.Next() {
		var e rowEntry
		if err := rows.Scan(&e.id, &e.url, &e.title, &e.domain, &e.visitTs); err != nil {
			return nil, fmt.Errorf("scan tagged page: %w", err)
		}
		entries = append(entries, e)
		ids = append(ids, e.id)
	}
	if len(entries) == 0 {
		return []TaggedPage{}, nil
	}

	// Fetch all tags for the matched pages in one query.
	tagsByPage := make(map[int64][]string, len(entries))
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	tagRows, err := s.db.Query(
		fmt.Sprintf(`SELECT page_id, tag FROM page_tags WHERE page_id IN (%s) ORDER BY tag`, placeholders),
		ids...,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch tags: %w", err)
	}
	defer tagRows.Close()
	for tagRows.Next() {
		var pid int64
		var t string
		if err := tagRows.Scan(&pid, &t); err != nil {
			continue
		}
		tagsByPage[pid] = append(tagsByPage[pid], t)
	}

	out := make([]TaggedPage, 0, len(entries))
	for _, e := range entries {
		out = append(out, TaggedPage{
			URL:     e.url,
			Title:   e.title,
			Domain:  e.domain,
			VisitTs: e.visitTs,
			Tags:    tagsByPage[e.id],
		})
	}
	return out, nil
}
