# 3. Daily use

Two capture paths: **passive** (the default, runs in the background) and **manual** (the popup's **Index this site now** button, with finer control).

---

## 3.1 Passive capture

Browse normally. When a tab is visible for at least the dwell threshold (default **10 seconds**), the extension:

1. Extracts the title and HTML meta tags (description, keywords, og:*, author).
2. Sends a lightweight `/visit` payload to the daemon over `127.0.0.1`.
3. Skips capture if the hostname is in your user blacklist or the tab is incognito.

The badge cycles through these states:

| Color | Meaning |
|---|---|
| (no badge) | Tracking, dwell timer running |
| 🔵 blue | Visit recorded |
| 🟢 green | Page fully indexed (chunks + embeddings stored) |
| 🟡 yellow | Site is on your blacklist — capture skipped |
| 🔴 red | Daemon unreachable (see [04-troubleshooting.md](04-troubleshooting.md)) |

State never downgrades within a single navigation, so a revisit that's already indexed stays green.

---

## 3.2 Manual indexing — the popup

Click the extension icon. The popup is centered on a single button: **Index this site now**.

![Popup main view](../../assets/search.png)
*Popup — search tab*

Clicking the button opens an inline panel:

- **Tags input** (CSV) — pre-filled with the page's current tags if it's already indexed. The list you submit becomes the page's full tag set (set-mode).
- **Mode selector** — four options, see below.
- **Manual textarea** — appears only when mode is `manual`.
- **✨ Suggest tags** — calls the LLM with the page text and your existing tag taxonomy as context; merges up to 3 dedup'd suggestions into the input. Requires `VBM_LLM_MODEL` configured.
- **Confirm** — fires the ingest.

### The four ingest modes

| Mode | What it indexes | When to use |
|---|---|---|
| **`full_text`** *(default)* | Readability-extracted body + title + meta tags | Long-form articles, docs, blog posts |
| **`llm_summary`** | Same as full_text, then the daemon runs a summarize prompt and indexes only the summary | Pages where you want signal-not-noise (verbose tutorials, marketing copy) |
| **`manual`** | Title + meta + whatever you typed in the textarea | When the page text is bad (PDF rendered as images) or you want to add your own annotation |
| **`meta_only`** | Just title + meta tags | Pages whose body is mostly UI chrome (dashboards, video sites, gallery indexes) |

`llm_summary` and `meta_only` produce shorter, denser chunks — search precision goes up, recall on rare phrasing goes down.

### Three client-side intents (extra capture sources)

In addition to the modes above, the popup exposes three buttons that grab content from the *current page* in your browser before sending:

- **Selection** — `window.getSelection()`. Errors with "Nothing selected" if empty.
- **YouTube transcript** *(visible only on `youtube.com/watch`)* — opens the transcript panel and scrapes every `ytd-transcript-segment-renderer .segment-text`. Errors if the video has no captions.
- **YouTube comments** *(visible only on `youtube.com/watch`)* — reads the top 50 comment threads already loaded in the DOM (no auto-scroll).

All three feed into `mode: "manual"` automatically — the text is grabbed client-side and the daemon just stores it.

---

## 3.3 Tags

Tags are normalized to `[a-z0-9 \-_]`, max 64 chars per tag. They are page-level (not chunk-level).

- **Manual entry** — type in the popup's tag input, comma-separated.
- **LLM suggestion** — ✨ button. Reuses your existing taxonomy as context to avoid synonyms (e.g., suggests `react` if `react` already exists, not `reactjs`).
- **Set vs. merge** — when you submit ingest from the popup, the tag list is the page's *final* state (replace). Direct API calls without `setTags=true` merge instead.

### Browse by tag

Open the local UI at `http://127.0.0.1:7532/` → **Tags** tab.

![Tags list](../../assets/tags-list.png)
*Tags tab — alphabetical list with page counts*

![Tags cloud](../../assets/tags-cloud.png)
*Tags tab — cloud view, font size weighted by frequency*

Click any tag to see all pages tagged with it. Tag-filtered searches restrict candidates to that tag.

---

## 3.4 Search

### Via the popup

Open the popup, type into the search box. Hybrid search runs against everything you've indexed:

- BM25 over the FTS5 virtual table.
- Cosine similarity over the embeddings (brute-force, fine up to ~50k chunks).
- Reciprocal Rank Fusion (k=60) merges the two rankings.

![Search results](../../assets/search.png)
*Search — hybrid results with snippets, domain, and visit date*

### Filtering

The popup's search tab lets you:

- **Filter by tag** — restrict candidates before search.
- **Filter by domain** — same idea, hostname suffix match.
- **Set max results** — input box, 1 to 1000, default 20 (CR-0007).

![Filtered search](../../assets/search-filtered.png)
*Search with tag filter applied*

### Via the omnibox

Type `@recall` + space in the Chrome address bar, then your query in natural language:

```
@recall react hooks useeffect cleanup
@recall docker compose persistent volume
@recall comparison tokio async-std performance
```

Top 5 results appear as suggestions with snippet, domain, and visit date. Press Enter (or click) to open.

**Query tips.**

- Two to five words is the sweet spot for hybrid search.
- A single word reduces it to BM25 ranking — fine but not why you bought the ticket.
- If you remember the exact phrasing, use it; if you only remember the topic, describe it semantically (queries like "that article about why Postgres tuple visibility is slow" actually work once embeddings are configured).

---

## 3.5 Timeline

The local UI's **Timeline** tab shows a per-day chart of pages indexed across the full period, plus a list of pages for the selected day.

![Timeline](../../assets/timeline.png)
*Timeline — bar chart of pages per day; click a bar to drill into that day*

The chart aggregates the entire period (independent of any list `limit`); the list under it shows pages for the currently selected day. Default selection: today (UTC) if it has traffic; otherwise the most recent day with activity.

---

## 3.6 Hot words

The **Hot words** view ranks the most-frequent meaningful tokens across your index — a visual proxy for "what topics am I actually reading about?".

![Hot words](../../assets/hot-words.png)
*Hot words — frequency-weighted token cloud*

---

## 3.7 Exclusions (blacklist)

Add hostname patterns you don't want captured. The match is suffix-based, so `example.com` blocks `sub.example.com` too.

![Exclusions](../../assets/exclusions.png)
*Exclusions — manage your user blacklist*

You can also block a site quickly from the popup → **Block this site** while on it.

The user blacklist lives in `chrome.storage.local`. The daemon also keeps a separate blacklist table seeded with private/loopback IP patterns at first run.

---

## 3.8 Forgetting pages

Right of recall is right of erasure:

- **Popup** — "Forget URL" / "Forget domain" inputs.
- **Local UI** — `Forget` button next to each row.
- **API** — `DELETE /forget` with `{"type":"url|domain|timerange","value":"..."}`.

Removal is **physical**: `DELETE FROM pages` cascades to chunks, embeddings, and tags, and the FTS5 index is rebuilt. No soft-delete, no trash bin.
