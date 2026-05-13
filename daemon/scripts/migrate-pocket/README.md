# migrate-pocket

Port GetPocket markdown bookmarks (Obsidian vault) into the Vector Bookmarks
daemon via `POST /ingest`.

## Source format

Each `*.md` file has frontmatter like:

```yaml
---
url: https://example.com/...
time_added: 1668098076          # unix seconds
tags: api|detran|governo        # legacy pipe form
tags fixed: api,detran,governo  # canonical comma form (preferred)
status: unread
---
```

The script reads `url`, `time_added`, and `tags fixed` (falling back to `tags`).
Body is empty in the source — the script **scrapes the live URL** to get the
title and text, then POSTs to `/ingest` with `mode=llm_summary` so the daemon
embeds by summary.

## Install

```bash
cd daemon/scripts/migrate-pocket
npm install        # pulls jsdom + @mozilla/readability
```

## Run

```bash
# dry run first
node import-pocket.mjs \
  --dir "/mnt/d/obsidian-vaults/danilo/Danilo/Notion Clippings/Get Pocket Raw" \
  --dry-run

# real run (assumes daemon on 127.0.0.1:7532; export VBM_AUTH_TOKEN if set)
node import-pocket.mjs \
  --dir "/mnt/d/obsidian-vaults/danilo/Danilo/Notion Clippings/Get Pocket Raw" \
  --concurrency 3
```

## Flags

| flag | default | notes |
|---|---|---|
| `--dir` | (required) | folder of `*.md` bookmarks |
| `--base` | `http://127.0.0.1:7532` | daemon base URL |
| `--token` | `$VBM_AUTH_TOKEN` | bearer token (omit if disabled) |
| `--mode` | `llm_summary` | also: `full_text`, `manual`, `meta_only` |
| `--concurrency` | `3` | parallel workers |
| `--limit` | `0` (all) | cap files processed |
| `--dry-run` | off | parse + scrape, no POST |
| `--interactive` | off | y/n prompt before each POST (forces concurrency=1); `q` quits |
| `--delete-after-ingest` | off | `unlink` the source `.md` once `/ingest` returns OK |
| `--delete-dead` | off | also `unlink` `.md` files whose URL is dead (DNS/404/410/451/TLS) |
| `--no-suggest-tags` | off | disable LLM tag suggestion. Default: call `POST /tags/suggest` when scraped text ≥ 100 chars and merge results (uniq) with frontmatter tags before ingest. Suggest failures (502/503/etc.) are non-fatal — frontmatter tags still flow through. |
| `--min-text <N>` | `0` | skip ingest when scraped text length < N chars (recorded in state as `short-text`). Use `--min-text 100` to align with the suggest API's minimum. |
| `--skip-indexed` | on | `GET /page?url=` first; skip if already indexed |
| `--state` | `./.import-pocket.state.json` | resume file (per-URL done/failed) |
| `--timeout-ms` | `20000` | per-fetch timeout |
| `--user-agent` | importer UA | override scrape UA |

## Behavior

- Resumable — successful URLs are written to the state file and skipped on rerun.
- Failures are also recorded; rerun retries them (they aren't in `done`).
- Scraping uses **jsdom + @mozilla/readability** (Firefox Reader View
  algorithm). Falls back to whole-body innerText when Readability bails.
- `setTags: true` ensures the imported tag set replaces whatever the daemon
  may already have for the URL.
