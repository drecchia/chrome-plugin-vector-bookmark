# Chrome Web Store — Listing Copy

Copy/paste fields for the Chrome Web Store developer dashboard.

## Title

Vector Bookmark

## Summary (≤132 chars)

Semantic recall for everything you've read. 100% local hybrid BM25 + vector search across your browsing history.

## Category

Productivity

## Language

English (US) — primary

## Detailed description

Vector Bookmark turns your browsing history into a searchable second brain — without sending a single byte to anyone else's server.

While you read, a tiny local daemon (vbmd) running on your own machine indexes the pages you actually spend time on. When you need to find something later — a half-remembered article, a tutorial you skimmed last month, a Stack Overflow answer — you search by meaning, not just keywords.

KEY FEATURES

• Hybrid search: BM25 full-text + vector embeddings combined with Reciprocal Rank Fusion. Finds the page even when you forget the exact words.
• 100% local by default: SQLite database on your disk. No accounts, no cloud, no telemetry.
• Passive capture: pages you spend at least 10 seconds on (configurable) are auto-indexed. No clicks needed.
• Manual indexing on demand: open the popup, pick a mode (full text, LLM summary, manual notes, or meta-only), tag, and confirm.
• Tag-aware: assign tags from the popup, get LLM-suggested tags from the existing taxonomy, browse a tag cloud, filter searches by tag.
• YouTube-aware: extract the transcript or top comments of a video as the indexed content.
• Timeline view: see your reading volume per day, drill into any day's pages.
• Omnibox: type @recall <query> in the address bar to search.
• Privacy-first: incognito tabs are never touched, password fields are never read, your blacklist is yours.
• Bring your own LLM (optional): point the daemon at any OpenAI-compatible endpoint for embeddings and summaries — local (Ollama, llama.cpp) or cloud, your choice.

REQUIREMENTS

You need to install and run the companion daemon `vbmd` on your machine. Setup instructions and the full source code are at https://github.com/drecchia/chrome-plugin-vector-bookmark.

WHY <all_urls>?

The single purpose of this extension is to remember pages across your entire browsing — so it must observe all sites you visit. You stay in control via the user-managed blacklist and the dwell threshold.

PRIVACY

Read the full privacy policy at https://raw.githubusercontent.com/drecchia/chrome-plugin-vector-bookmark/main/PRIVACY.md.

## Privacy policy URL

https://raw.githubusercontent.com/drecchia/chrome-plugin-vector-bookmark/main/PRIVACY.md

## Homepage URL

https://github.com/drecchia/chrome-plugin-vector-bookmark

## Support URL

https://github.com/drecchia/chrome-plugin-vector-bookmark/issues

## Single-purpose justification

Build a private, local semantic index of pages the user reads, and let them recall those pages later by meaning. Every feature (capture, tagging, search, timeline) serves that single purpose.

## Permission justifications

- **tabs / webNavigation**: detect navigation events so the content script can start measuring dwell time on the active tab.
- **storage**: persist user settings (daemon host/port, dwell threshold) in `chrome.storage.local`.
- **scripting**: inject the on-demand content script that extracts page body text via Mozilla Readability when the user clicks "Index this site now".
- **omnibox**: register the `@recall` keyword so the user can search their index from the address bar.
- **idle**: pause dwell tracking when the OS is idle so background tabs don't accumulate fake reading time.
- **host_permissions `<all_urls>`**: the extension's purpose is recall across the user's entire browsing; capture is gated by their blacklist and dwell threshold.

## Assets

| Slot | File | Size |
|---|---|---|
| Store icon | `extension/icons/icon128.png` | 128×128 |
| Screenshot 1 | `store/screenshots/search.png` | 1280×800 |
| Screenshot 2 | `store/screenshots/search-filtered.png` | 1280×800 |
| Screenshot 3 | `store/screenshots/tags-cloud.png` | 1280×800 |
| Screenshot 4 | `store/screenshots/timeline.png` | 1280×800 |
| Screenshot 5 | `store/screenshots/hot-words.png` | 1280×800 |
| Small promo tile | `store/promo-tile-440x280.png` | 440×280 |
| Marquee promo tile | `store/marquee-1400x560.png` | 1400×560 |

## Submission checklist

- [ ] Bump `extension/manifest.json` and `extension/package.json` to release version
- [ ] Run `npm run typecheck` and `npm run test` in `extension/` — both green
- [ ] Run `npm run build` in `extension/` — produces `extension/dist/`
- [ ] Zip the contents of `extension/dist/` (not the folder itself): `cd extension/dist && zip -r ../vector-bookmark-1.0.0.zip .`
- [ ] Confirm zip contains: `manifest.json`, `icons/`, built `assets/` JS/CSS, `src/popup/index.html` — and no `node_modules/`, source maps, or `.env`
- [ ] Push `PRIVACY.md` to the `main` branch on GitHub (the privacy URL must be live)
- [ ] In the Chrome Web Store Developer Dashboard:
  - [ ] Create new item, upload zip
  - [ ] Paste fields from this document into the listing
  - [ ] Upload the 5 screenshots, store icon, promo tile, marquee
  - [ ] Set distribution: Public
  - [ ] Set visibility: World (or your preferred regions)
  - [ ] Submit for review
