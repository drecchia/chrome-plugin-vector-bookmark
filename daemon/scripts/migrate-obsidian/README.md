# migrate-obsidian

Port Obsidian "To Deeply Check" markdown bookmarks into the Vector Bookmarks
daemon via `POST /ingest`. Sibling of `migrate-pocket/` — same pipeline,
different frontmatter layout.

## Source format

Each `*.md` file has frontmatter like:

```yaml
---
Last edited time: 2025-07-14T20:24
URL: https://carbone.io/
tags:
  - export pdf
  - exportar
  - pdf
  - relatorio
  - report
  - report generator
---
```

The script reads `URL`, `Last edited time`, and the `tags` YAML array.
The markdown body inside the file (often a full clipped page) is **ignored** —
the script re-scrapes the live URL with jsdom + Readability for a fresh,
clean text payload, identical to the Pocket migrator.

## Differences from `migrate-pocket`

| Aspect | Pocket | Obsidian "To Deeply Check" |
|---|---|---|
| URL key | `url:` | `URL:` (case-insensitive parser) |
| Tags | `tags fixed: a,b,c` / `tags: a\|b\|c` | YAML array (multi-line `- value`), spaces allowed |
| Date | `time_added: 1668098076` (unix s) | `Last edited time: 2025-07-14T20:24` (ISO 8601) |
| Body | empty stub | full markdown body (ignored — we re-scrape) |
| State file | `.import-pocket.state.json` | `.import-obsidian.state.json` |
| Env override | `POCKET_DIR` | `OBSIDIAN_DIR` |

Everything else — scrape, dead-URL classification, meta-refresh hops, Chrome
header fingerprint, `/tags/suggest`, `/ingest`, resumable state, all CLI flags
— is byte-identical to `migrate-pocket`.

## Install

```bash
cd daemon/scripts/migrate-obsidian
npm install        # pulls jsdom + @mozilla/readability
```

## Run

```bash
# dry run first
node import-obsidian.mjs \
  --dir "/mnt/d/obsidian-vaults/danilo/Danilo/Notion Clippings/My Bookmarks/To Deeply Check" \
  --dry-run --limit 5

# interactive single-shot debugging
node import-obsidian.mjs --dir "..." --interactive --limit 10

# full migration (preserve .md files, only delete the truly-dead URLs)
node import-obsidian.mjs --dir "..." --concurrency 3 --delete-dead --min-text 100
```

## Flags

See the header doc-block inside `import-obsidian.mjs` for the full flag table —
identical set to the Pocket migrator (`--interactive`, `--dry-run`,
`--skip-indexed`, `--no-suggest-tags`, `--min-text`, `--retry-short`,
`--delete-after-ingest`, `--delete-dead`, `--concurrency`, `--limit`,
`--timeout-ms`, `--base`, `--token`, `--mode`, `--state`, `--user-agent`).
