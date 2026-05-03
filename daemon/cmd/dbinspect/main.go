package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: dbinspect <path-to-vbm.db>")
		os.Exit(2)
	}
	path := os.Args[1]
	st, err := os.Stat(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("file: %s  size: %.2f MB\n\n", path, float64(st.Size())/1024/1024)

	db, err := sql.Open("sqlite", path+"?_pragma=query_only(1)")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()

	// dbstat — bytes per object.
	fmt.Println("=== Bytes per object (top 20) ===")
	rows, err := db.Query(`SELECT name, SUM(pgsize) AS bytes
		FROM dbstat GROUP BY name ORDER BY bytes DESC LIMIT 20`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dbstat:", err)
	} else {
		for rows.Next() {
			var name string
			var bytes int64
			rows.Scan(&name, &bytes)
			fmt.Printf("  %-30s %10.2f MB\n", name, float64(bytes)/1024/1024)
		}
		rows.Close()
	}

	// Row counts and avg/total text size for major tables.
	fmt.Println("\n=== Row counts & text bytes ===")
	queries := []struct {
		label string
		sql   string
	}{
		{"pages rows", `SELECT COUNT(*) FROM pages`},
		{"chunks rows", `SELECT COUNT(*) FROM chunks`},
		{"chunks total text bytes", `SELECT COALESCE(SUM(LENGTH(text)),0) FROM chunks`},
		{"chunks total embedding bytes", `SELECT COALESCE(SUM(LENGTH(embedding)),0) FROM chunks`},
		{"pages total meta_json bytes", `SELECT COALESCE(SUM(LENGTH(meta_json)),0) FROM pages`},
		{"queue rows pending", `SELECT COUNT(*) FROM queue WHERE status='pending'`},
		{"queue total text bytes (all)", `SELECT COALESCE(SUM(LENGTH(text)),0) FROM queue`},
	}
	for _, q := range queries {
		var v int64
		if err := db.QueryRow(q.sql).Scan(&v); err != nil {
			fmt.Printf("  %-32s ERR: %v\n", q.label, err)
			continue
		}
		if q.sql[len(q.sql)-7:] == "(text)" || (len(q.label) > 4 && q.label[len(q.label)-5:] == "bytes") {
			fmt.Printf("  %-32s %10.2f MB\n", q.label, float64(v)/1024/1024)
		} else {
			fmt.Printf("  %-32s %10d\n", q.label, v)
		}
	}

	// Top 10 pages by chunk count and total text size.
	fmt.Println("\n=== Top 10 pages by total chunk bytes ===")
	rows, err = db.Query(`
		SELECT p.url, COUNT(c.id) AS n_chunks, SUM(LENGTH(c.text)) AS total_text
		FROM pages p JOIN chunks c ON c.page_id = p.id
		GROUP BY p.id ORDER BY total_text DESC LIMIT 10`)
	if err == nil {
		for rows.Next() {
			var url string
			var n, total int64
			rows.Scan(&url, &n, &total)
			if len(url) > 70 {
				url = url[:70] + "…"
			}
			fmt.Printf("  %5d chunks  %7.2f KB  %s\n", n, float64(total)/1024, url)
		}
		rows.Close()
	}

	// FTS5 size estimate (chunks_fts_data + idx).
	fmt.Println("\n=== FTS5 internal tables ===")
	rows, err = db.Query(`SELECT name, SUM(pgsize) AS bytes
		FROM dbstat WHERE name LIKE 'chunks_fts%' GROUP BY name ORDER BY bytes DESC`)
	if err == nil {
		for rows.Next() {
			var name string
			var bytes int64
			rows.Scan(&name, &bytes)
			fmt.Printf("  %-30s %10.2f MB\n", name, float64(bytes)/1024/1024)
		}
		rows.Close()
	}
}
