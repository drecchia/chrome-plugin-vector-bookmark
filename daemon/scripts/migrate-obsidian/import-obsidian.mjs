#!/usr/bin/env node
// =============================================================================
// import-obsidian.mjs — Obsidian "To Deeply Check" .md → Vector Bookmarks daemon
// =============================================================================
//
// PURPOSE
//   Migrate a folder of Obsidian-clipped markdown bookmarks (different layout
//   than GetPocket — see SOURCE FRONTMATTER below) into the local `vbmd`
//   daemon. Per file the script:
//     1. parses YAML frontmatter      (URL, Last edited time, tags array)
//     2. (optional) checks /page      — skip if URL is already indexed
//     3. fetches the live URL         — strips boilerplate, extracts title+text
//                                       (the markdown body inside the .md is
//                                       intentionally ignored; we re-scrape
//                                       to get a fresh, clean text payload)
//     4. (optional) calls /tags/suggest — merges LLM-suggested tags with the
//                                       frontmatter tags (unique union)
//     5. POSTs /ingest                — body shown below
//
// SOURCE FRONTMATTER (one `*.md` per bookmark, all 160 files in this folder)
//   ---
//   Last edited time: 2025-07-14T20:24
//   URL: https://carbone.io/
//   tags:
//     - export pdf
//     - exportar
//     - pdf
//     - relatorio
//     - report
//     - report generator
//   ---
//
//   Differences from the Pocket layout (handled by parseFrontmatter):
//     - URL key is `URL:` (capitalised), not `url:`
//     - tags are a YAML array (multi-line `- value`), with spaces allowed
//       in values (e.g. `report generator`) — no pipes, no csv variant
//     - date is `Last edited time` in ISO 8601 (no timezone), not unix-epoch
//     - markdown body below the frontmatter contains the full clipped page;
//       we ignore it and re-scrape the live URL for consistency
//
// INGEST PAYLOAD (POST {BASE}/ingest)
//   {
//     url, title, text, visitTs, domain,
//     tags:    [...frontmatter, ...llmSuggested] uniq,
//     mode:    "llm_summary" (default; daemon embeds by summary),
//     setTags: true
//   }
//
// DEAD URLS (never POSTed, marked `dead` in state, optionally unlinked)
//   - DNS / network: ENOTFOUND, EAI_AGAIN, ECONNREFUSED, EHOSTUNREACH,
//                    ENETUNREACH, ECONNRESET, EHOSTDOWN
//   - TLS:           any CERT_* error
//   - HTTP:          400, 404, 410, 451
//   Transient (5xx, timeout) → recorded as `failed`; rerun retries.
//
// STATE FILE (resumable; default ./.import-obsidian.state.json)
//   { done: { "<url>": { at, via, ... } }, failed: { "<file>": { at, error } } }
//   `via` ∈ { "ingest", "already-indexed", "dead", "short-text" }.
//   Successful + skipped URLs are never reprocessed; failed files retry on rerun.
//
// USAGE
//   node import-obsidian.mjs \
//     --dir "/mnt/d/obsidian-vaults/.../My Bookmarks/To Deeply Check"  \  # required
//     [--base http://127.0.0.1:7532]   [--token $VBM_AUTH_TOKEN]       \
//     [--mode llm_summary|full_text|manual|meta_only]                  \
//     [--concurrency 3]    [--limit 0]    [--timeout-ms 20000]         \
//     [--state ./.import-obsidian.state.json]                          \
//     [--user-agent "..."]                                             \
//     [--dry-run]          [--interactive]                             \
//     [--skip-indexed=false]                                           \
//     [--no-suggest-tags]  [--min-text 100]                            \
//     [--delete-after-ingest]  [--delete-dead]  [--retry-short]
//
//   --interactive             y/n/q prompt per file; forces concurrency=1.
//                             Shows title, frontmatter tags, suggested tags,
//                             merged tags, text preview, full POST body.
//   --dry-run                 scrapes but does NOT POST (suggest is also skipped).
//   --skip-indexed=false      bypass the /page pre-check (default: enabled).
//   --no-suggest-tags         disable /tags/suggest call (default: enabled
//                             when scraped text ≥ 100 chars).
//   --min-text N              skip ingest when scraped text < N chars
//                             (recorded as via=short-text).
//   --retry-short             on rerun, reprocess URLs previously skipped as
//                             short-text (useful after fixing extraction bugs
//                             or meta-refresh victims).
//   --delete-after-ingest     unlink the source .md once /ingest returns OK.
//   --delete-dead             unlink .md files whose URL is dead.
//
// EXAMPLES
//   # safe dry run on first 5 files
//   node import-obsidian.mjs --dir "..." --dry-run --limit 5
//
//   # single-shot debugging with full body preview
//   node import-obsidian.mjs --dir "..." --interactive --limit 10
//
//   # full migration (keeps .md files; deletes only the truly-dead ones)
//   node import-obsidian.mjs --dir "..." --concurrency 3 \
//     --delete-dead --min-text 100
//
// REQUIREMENTS
//   Node 18+ (uses built-in fetch).
//   Deps: jsdom, @mozilla/readability   → run `npm install` in this folder first.
//
// EXTRACTION
//   HTML is parsed with jsdom and run through Mozilla's Readability
//   (same algorithm Firefox uses for Reader View). When Readability bails or
//   the page isn't reader-able, falls back to whole-document innerText after
//   removing script/style/nav/header/footer/form/svg.
//
// FETCH FINGERPRINT
//   Sends a Chrome-shaped header set (UA, Accept, Accept-Language, Sec-Fetch-*,
//   Sec-CH-UA, Referer = origin/) to defeat trivial bot blocks. Sites behind
//   real JS challenges (Cloudflare Turnstile, Akamai Bot Manager, PerimeterX)
//   will still return 403/503 and are recorded as `failed` — they need a real
//   headless browser (Playwright/Puppeteer) and are out of scope for this
//   plain-fetch importer.
// =============================================================================

import { promises as fs } from "node:fs";
import path from "node:path";
import readline from "node:readline";
import { setTimeout as sleep } from "node:timers/promises";
import { JSDOM, VirtualConsole } from "jsdom";
import { Readability, isProbablyReaderable } from "@mozilla/readability";

const args = parseArgs(process.argv.slice(2));
const DIR = args.dir || process.env.OBSIDIAN_DIR;
if (!DIR) die("Missing --dir <markdown folder>");
const BASE = (args.base || process.env.VBM_BASE || "http://127.0.0.1:7532").replace(/\/+$/, "");
const TOKEN = args.token || process.env.VBM_AUTH_TOKEN || "";
const MODE = args.mode || "llm_summary";
const CONCURRENCY = Math.max(1, parseInt(args.concurrency || "3", 10));
const LIMIT = parseInt(args.limit || "0", 10);
const DRY = !!args["dry-run"];
const INTERACTIVE = !!args.interactive || !!args.i;
const DELETE_AFTER = !!args["delete-after-ingest"] || !!args["delete-after"];
const DELETE_DEAD = !!args["delete-dead"];
const SUGGEST_TAGS = args["suggest-tags"] !== "false" && args["no-suggest-tags"] !== "true";
const MIN_TEXT = parseInt(args["min-text"] || "0", 10);
const RETRY_SHORT = !!args["retry-short"];
const SKIP_INDEXED = args["skip-indexed"] !== "false";
const STATE_PATH = args.state || path.join(process.cwd(), ".import-obsidian.state.json");
// Realistic Chrome UA — bot-detectors instantly block obvious automation strings.
const UA = args["user-agent"] ||
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
  "(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";
const TIMEOUT_MS = parseInt(args["timeout-ms"] || "20000", 10);

const HEADERS = {
  "Content-Type": "application/json",
  ...(TOKEN ? { Authorization: `Bearer ${TOKEN}` } : {}),
};

main().catch((e) => {
  console.error("fatal:", e?.stack || e);
  process.exit(1);
});

async function main() {
  const state = await loadState(STATE_PATH);
  const files = (await fs.readdir(DIR)).filter((f) => f.toLowerCase().endsWith(".md"));
  files.sort();
  const queue = files.slice(0, LIMIT > 0 ? LIMIT : files.length);

  const effectiveConc = INTERACTIVE ? 1 : CONCURRENCY;
  console.error(`[start] ${queue.length} files | base=${BASE} | mode=${MODE} | dry=${DRY} | interactive=${INTERACTIVE} | concurrency=${effectiveConc}`);

  const stats = { ok: 0, skipped: 0, failed: 0, noUrl: 0 };
  let idx = 0;

  async function worker(wid) {
    while (true) {
      const i = idx++;
      if (i >= queue.length) return;
      const file = queue[i];
      const fp = path.join(DIR, file);
      const tag = `[${i + 1}/${queue.length}]`;
      try {
        const meta = await parseFrontmatter(fp);
        if (!meta.url) {
          stats.noUrl++;
          console.error(`${tag} SKIP no-url ${file}`);
          continue;
        }
        if (state.done[meta.url]) {
          const prev = state.done[meta.url];
          if (RETRY_SHORT && prev.via === "short-text") {
            delete state.done[meta.url];
            await saveState(STATE_PATH, state);
            console.error(`${tag} RETRY ${meta.url} (was short-text:len=${prev.textLen ?? "?"})`);
          } else {
            stats.skipped++;
            const why = prev.via || "unknown";
            const extra = prev.reason ? `:${prev.reason}` : (prev.textLen != null ? `:len=${prev.textLen}` : "");
            console.error(`${tag} SKIP done(${why}${extra}) ${meta.url}`);
            continue;
          }
        }
        if (SKIP_INDEXED && !DRY) {
          const indexed = await isIndexed(meta.url);
          if (indexed) {
            state.done[meta.url] = { at: Date.now(), via: "already-indexed" };
            await saveState(STATE_PATH, state);
            stats.skipped++;
            console.error(`${tag} SKIP indexed ${meta.url}`);
            continue;
          }
        }
        const scraped = await scrape(meta.url).catch((e) => ({ error: e }));
        if (scraped.dead) {
          state.done[meta.url] = { at: Date.now(), via: "dead", reason: scraped.dead };
          await saveState(STATE_PATH, state);
          stats.skipped++;
          let extra = "";
          if (DELETE_DEAD && !DRY) {
            await fs.unlink(fp).catch(() => {});
            extra = " (file deleted)";
          }
          console.error(`${tag} DEAD ${meta.url} (${scraped.dead})${extra}`);
          continue;
        }
        if (scraped.error) {
          throw scraped.error;
        }
        const title = scraped.title || titleFromFilename(file);
        const text = scraped.text || "";
        if (MIN_TEXT > 0 && text.length < MIN_TEXT) {
          state.done[meta.url] = { at: Date.now(), via: "short-text", textLen: text.length };
          await saveState(STATE_PATH, state);
          stats.skipped++;
          console.error(`${tag} SHORT ${meta.url} textLen=${text.length} < ${MIN_TEXT}`);
          continue;
        }
        let suggested = [];
        let suggestNote = "";
        if (SUGGEST_TAGS && !DRY && text.length >= 100) {
          const s = await suggestTags({ url: meta.url, title, text });
          if (s.ok) suggested = s.tags;
          else suggestNote = ` (suggest: ${s.error})`;
        } else if (SUGGEST_TAGS && text.length < 100) {
          suggestNote = " (suggest: text<100)";
        }
        const mergedTags = Array.from(new Set([...meta.tags, ...suggested]));
        const payload = {
          url: meta.url,
          title,
          text,
          visitTs: meta.visitTs,
          domain: safeDomain(meta.url),
          tags: mergedTags,
          mode: MODE,
          setTags: true,
        };
        if (DRY) {
          console.error(`${tag} DRY  ${meta.url} title="${truncate(title, 80)}" textLen=${text.length} tags=${meta.tags.join(",")}`);
          stats.ok++;
          continue;
        }
        if (INTERACTIVE) {
          console.error("");
          console.error(`${tag} ${meta.url}`);
          console.error(`  title : ${truncate(title, 120)}`);
          console.error(`  tags  : ${meta.tags.join(", ") || "(none)"}`);
          console.error(`  +sugg : ${suggested.join(", ") || "(none)"}${suggestNote}`);
          console.error(`  merged: ${mergedTags.join(", ") || "(none)"}`);
          console.error(`  text  : ${text.length} chars  preview="${truncate(text, 160).replace(/\s+/g, " ")}"`);
          console.error(`  POST ${BASE}/ingest`);
          const preview = { ...payload, text: truncate(payload.text || "", 500) };
          console.error("  body  : " + JSON.stringify(preview, null, 2).split("\n").join("\n          "));
          const ans = await ask("  send? [y]es / [n]o / [q]uit > ");
          if (/^q/i.test(ans)) { console.error("aborted by user"); break; }
          if (!/^y/i.test(ans)) {
            stats.skipped++;
            console.error(`${tag} SKIP user-declined`);
            continue;
          }
        }
        const res = await postIngest(payload);
        if (!res.ok) throw new Error(`ingest ${res.status}: ${res.body}`);
        state.done[meta.url] = { at: Date.now(), via: "ingest", textLen: text.length };
        await saveState(STATE_PATH, state);
        stats.ok++;
        let extra = "";
        if (DELETE_AFTER) {
          try {
            await fs.unlink(fp);
            extra = " (file deleted)";
          } catch (e) {
            extra = ` (delete failed: ${e?.message || e})`;
          }
        }
        console.error(`${tag} OK   ${meta.url} textLen=${text.length} tags=${mergedTags.length}(+${suggested.length})${suggestNote}${extra}`);
      } catch (e) {
        stats.failed++;
        state.failed[file] = { at: Date.now(), error: String(e?.message || e) };
        await saveState(STATE_PATH, state);
        console.error(`${tag} FAIL ${file}: ${e?.message || e}`);
      }
    }
  }

  await Promise.all(Array.from({ length: effectiveConc }, (_, i) => worker(i)));
  if (rlInstance) rlInstance.close();
  console.error(`[done] ok=${stats.ok} skipped=${stats.skipped} failed=${stats.failed} no-url=${stats.noUrl}`);
}

// ---------- frontmatter ----------
// Parses the "To Deeply Check" layout:
//   Last edited time: 2025-07-14T20:24
//   URL: https://...
//   tags:
//     - tag with spaces
//     - another
// Key matching is case-insensitive so `url:` / `URL:` both work.
async function parseFrontmatter(fp) {
  const raw = await fs.readFile(fp, "utf8");
  const m = raw.match(/^---\r?\n([\s\S]*?)\r?\n---/);
  if (!m) return { url: null, tags: [], visitTs: Date.now() };
  const lines = m[1].split(/\r?\n/);

  let url = null;
  let lastEdited = null;
  const tags = [];
  let inTags = false;

  for (const line of lines) {
    if (inTags) {
      const item = line.match(/^\s+-\s+(.+?)\s*$/);
      if (item) { tags.push(item[1].trim()); continue; }
      inTags = false; // left the array block — fall through to key/value parsing
    }
    if (/^tags\s*:\s*$/i.test(line)) { inTags = true; continue; }
    const kv = line.match(/^([A-Za-z][A-Za-z0-9 _-]*)\s*:\s*(.*)$/);
    if (!kv) continue;
    const key = kv[1].trim().toLowerCase();
    const val = kv[2].trim();
    if (!val) continue;
    if (key === "url") url = val;
    else if (key === "last edited time") lastEdited = val;
  }

  let visitTs = Date.now();
  if (lastEdited) {
    const t = Date.parse(lastEdited);
    if (Number.isFinite(t)) visitTs = t;
  }
  return { url, tags: Array.from(new Set(tags)), visitTs };
}

// ---------- API ----------
async function isIndexed(url) {
  const u = `${BASE}/page?url=${encodeURIComponent(url)}`;
  try {
    const r = await fetchWithTimeout(u, { headers: HEADERS }, TIMEOUT_MS);
    if (!r.ok) return false;
    const j = await r.json().catch(() => ({}));
    return !!(j.exists && j.indexed);
  } catch {
    return false;
  }
}

async function suggestTags({ url, title, text }) {
  try {
    const r = await fetchWithTimeout(
      `${BASE}/tags/suggest`,
      {
        method: "POST",
        headers: HEADERS,
        body: JSON.stringify({ url, title: title || "", text: text.slice(0, 20_000) }),
      },
      TIMEOUT_MS,
    );
    if (!r.ok) return { ok: false, error: `http:${r.status}` };
    const j = await r.json().catch(() => ({}));
    const tags = Array.isArray(j.tags) ? j.tags.map((t) => String(t).trim()).filter(Boolean) : [];
    return { ok: true, tags };
  } catch (e) {
    return { ok: false, error: e?.message || String(e) };
  }
}

async function postIngest(payload) {
  const r = await fetchWithTimeout(
    `${BASE}/ingest`,
    { method: "POST", headers: HEADERS, body: JSON.stringify(payload) },
    TIMEOUT_MS,
  );
  const body = await r.text().catch(() => "");
  return { ok: r.ok, status: r.status, body };
}

// ---------- scraping ----------
const DEAD_STATUSES = new Set([400, 404, 410, 451]);
const DEAD_DNS_CODES = new Set([
  "ENOTFOUND", "EAI_AGAIN", "ECONNREFUSED", "EHOSTUNREACH",
  "ENETUNREACH", "ECONNRESET", "EHOSTDOWN",
]);

// Header set mirroring what Chrome sends on a top-level navigation.
// This defeats lightweight bot filters (UA blocklists, missing-Accept-Language,
// missing Sec-Fetch-*) but NOT JS challenges (Cloudflare Turnstile, etc.) —
// those require a real browser. Sec-CH-UA values match the UA major version.
function browserHeaders(url) {
  let referer = "";
  try { referer = new URL(url).origin + "/"; } catch {}
  return {
    "User-Agent": UA,
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
    "Accept-Language": "en-US,en;q=0.9,pt-BR;q=0.8,pt;q=0.7",
    "Accept-Encoding": "gzip, deflate, br",
    "Upgrade-Insecure-Requests": "1",
    "Sec-Fetch-Site": "none",
    "Sec-Fetch-Mode": "navigate",
    "Sec-Fetch-User": "?1",
    "Sec-Fetch-Dest": "document",
    "Sec-CH-UA": '"Chromium";v="131", "Google Chrome";v="131", "Not_A Brand";v="24"',
    "Sec-CH-UA-Mobile": "?0",
    "Sec-CH-UA-Platform": '"Windows"',
    "Cache-Control": "max-age=0",
    ...(referer ? { Referer: referer } : {}),
  };
}

const MAX_META_HOPS = 3;

// Parse <meta http-equiv="refresh" content="0; url=/foo"> and JS
// `window.location.href = '...'` redirects that plain fetch won't follow.
function findClientRedirect(html, baseUrl) {
  const meta = html.match(/<meta[^>]+http-equiv=["']?refresh["']?[^>]*content=["']([^"']+)["']/i);
  if (meta) {
    const m = meta[1].match(/url\s*=\s*(['"]?)([^'";\s]+)\1?/i);
    if (m && m[2]) {
      try { return new URL(m[2], baseUrl).href; } catch {}
    }
  }
  // common JS redirect patterns
  const js = html.match(/(?:window\.)?location(?:\.href)?\s*=\s*['"]([^'"]+)['"]/i);
  if (js && js[1] && !/^javascript:/i.test(js[1])) {
    try { return new URL(js[1], baseUrl).href; } catch {}
  }
  return null;
}

async function scrape(url, hop = 0) {
  let r;
  try {
    r = await fetchWithTimeout(
      url,
      { redirect: "follow", headers: browserHeaders(url) },
      TIMEOUT_MS,
    );
  } catch (e) {
    const code = e?.cause?.code || e?.code;
    if (DEAD_DNS_CODES.has(code)) return { dead: `dns:${code}` };
    if (typeof code === "string" && code.startsWith("CERT_")) return { dead: `tls:${code}` };
    throw e;
  }
  if (DEAD_STATUSES.has(r.status)) return { dead: `http:${r.status}` };
  if (!r.ok) throw new Error(`http ${r.status}`);
  const ct = (r.headers.get("content-type") || "").toLowerCase();
  if (!ct.includes("html") && !ct.includes("xml") && ct) {
    return { title: "", text: "" };
  }
  const html = await r.text();

  // Follow meta-refresh / JS redirects (short stub pages) up to MAX_META_HOPS.
  if (hop < MAX_META_HOPS && html.length < 4096) {
    const next = findClientRedirect(html, r.url);
    if (next && next !== r.url && next !== url) {
      return scrape(next, hop + 1);
    }
  }
  return extract(html, r.url);
}

// Parse with jsdom and run Mozilla's Readability (same algorithm Firefox uses
// for Reader View). Falls back to whole-document innerText if Readability bails.
function extract(html, url) {
  const virtualConsole = new VirtualConsole(); // swallow CSS/JS parse noise
  virtualConsole.on("error", () => {});
  virtualConsole.on("jsdomError", () => {});
  const dom = new JSDOM(html, { url, virtualConsole });
  const doc = dom.window.document;

  // Title fallbacks (Readability also returns one, but capture early in case it fails)
  const ogTitle = doc.querySelector('meta[property="og:title"]')?.getAttribute("content");
  const docTitle = doc.title || "";
  let title = (ogTitle || docTitle || "").trim();

  let text = "";
  try {
    if (isProbablyReaderable(doc)) {
      const article = new Readability(doc).parse();
      if (article) {
        if (article.title) title = article.title.trim();
        text = (article.textContent || "").trim();
      }
    }
  } catch {
    // fall through to fallback
  }

  if (!text) {
    // strip obvious chrome before grabbing body text
    for (const sel of ["script", "style", "noscript", "svg", "nav", "header", "footer", "form"]) {
      doc.querySelectorAll(sel).forEach((n) => n.remove());
    }
    const root = doc.querySelector("article") || doc.querySelector("main") || doc.body;
    text = (root?.textContent || "").trim();
  }

  text = text.replace(/\s+/g, " ").slice(0, 200_000);
  return { title, text };
}

// ---------- utils ----------
async function fetchWithTimeout(url, opts, ms) {
  const ctrl = new AbortController();
  const t = setTimeout(() => ctrl.abort(), ms);
  try {
    return await fetch(url, { ...opts, signal: ctrl.signal });
  } finally {
    clearTimeout(t);
  }
}

function safeDomain(u) {
  try { return new URL(u).hostname; } catch { return ""; }
}

function titleFromFilename(f) {
  return f.replace(/\.md$/i, "").trim();
}

function truncate(s, n) {
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

async function loadState(p) {
  try {
    const raw = await fs.readFile(p, "utf8");
    const j = JSON.parse(raw);
    return { done: j.done || {}, failed: j.failed || {} };
  } catch {
    return { done: {}, failed: {} };
  }
}

let saveQueued = false;
let savePending = null;
async function saveState(p, state) {
  savePending = state;
  if (saveQueued) return;
  saveQueued = true;
  await sleep(50);
  saveQueued = false;
  await fs.writeFile(p, JSON.stringify(savePending, null, 2), "utf8");
}

let rlInstance = null;
function ask(prompt) {
  if (!rlInstance) {
    rlInstance = readline.createInterface({ input: process.stdin, output: process.stderr });
  }
  return new Promise((resolve) => rlInstance.question(prompt, (a) => resolve((a || "").trim())));
}

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (!a.startsWith("--")) continue;
    const k = a.slice(2);
    const next = argv[i + 1];
    if (next === undefined || next.startsWith("--")) out[k] = "true";
    else { out[k] = next; i++; }
  }
  if (out["dry-run"] === "true") out["dry-run"] = true;
  return out;
}

function die(msg) {
  console.error(msg);
  process.exit(2);
}
