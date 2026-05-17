package server

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	"github.com/vbm/daemon/internal/llm"
	"github.com/vbm/daemon/internal/queue"
	"github.com/vbm/daemon/internal/store"
)

// maxRequestBody caps POST/DELETE bodies at 4 MiB to prevent local DoS from
// unbounded payloads (a 30 MB HTML page produces ~200 chunks of ~500 tokens,
// which serialized is ≲ 3 MiB — 4 MiB is a comfortable ceiling).
const maxRequestBody = 4 << 20

// decodeJSONBody wraps req.Body with a size-limited reader, then JSON-decodes
// into v. Writes a 400/413 response on failure and returns false.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "request body too large") {
			http.Error(w, `{"error":"request too large"}`, http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return false
	}
	return true
}

type ingestRequest struct {
	URL     string   `json:"url"`
	Title   string   `json:"title"`
	Text    string   `json:"text"`
	VisitTs int64    `json:"visitTs"`
	DwellMs int64    `json:"dwellMs"`
	Domain  string   `json:"domain"`
	Tags    []string `json:"tags"`
	// Mode: "full_text" (default) | "llm_summary" | "manual" | "meta_only".
	// Affects only how Text is post-processed before chunking.
	Mode string `json:"mode"`
	// SetTags=true means the Tags slice is the authoritative final list for
	// this page (additions + removals applied). When false, Tags is merged
	// with existing tags (legacy accumulate behavior, kept for back-compat).
	SetTags bool `json:"setTags"`
}

type forgetRequest struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Validated by auth middleware already
		return true
	},
}

const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Vector Bookmark</title>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;font-size:14px;color:#111;background:#f9fafb;line-height:1.5}
.wrap{max-width:1100px;margin:0 auto;padding:32px 20px}
header{display:flex;align-items:baseline;justify-content:space-between;margin-bottom:16px}
h1{font-size:15px;font-weight:600}
#stat{font-size:12px;color:#9ca3af}
.tabs{display:flex;gap:0;border-bottom:1px solid #e5e7eb;margin-bottom:20px}
.tab{font-size:13px;font-weight:500;padding:7px 16px;cursor:pointer;border:none;background:none;color:#6b7280;border-bottom:2px solid transparent;margin-bottom:-1px}
.tab.active{color:#111;border-bottom-color:#111}
.tab:hover:not(.active){color:#374151}
.panel{display:none}.panel.active{display:block}
.search-row{display:flex;gap:8px;margin-bottom:20px}
#q{flex:1;padding:8px 12px;border:1px solid #d1d5db;border-radius:6px;font-size:14px;outline:none;background:#fff}
#q:focus{border-color:#6366f1;box-shadow:0 0 0 3px rgba(99,102,241,.12)}
#search-btn{padding:8px 18px;border:none;border-radius:6px;font-size:13px;font-weight:500;cursor:pointer;background:#111;color:#fff;white-space:nowrap}
#search-btn:hover{background:#374151}
#search-btn:disabled{background:#d1d5db;cursor:default}
.count{font-size:12px;color:#9ca3af;margin-bottom:10px}
.result{padding:12px 0;border-bottom:1px solid #f3f4f6}
.result:last-child{border-bottom:none}
.result-meta{display:flex;gap:8px;align-items:baseline;margin-bottom:3px}
.domain{font-size:11px;color:#6366f1;font-weight:500}
.date{font-size:11px;color:#9ca3af}
.result-title{font-size:14px;font-weight:500;color:#111;text-decoration:none;display:block;margin-bottom:3px}
.result-title:hover{color:#6366f1}
.snippet{font-size:12px;color:#6b7280;line-height:1.5}
.empty{color:#9ca3af;font-size:13px;padding:40px 0;text-align:center}
.forget-btn{float:right;font-size:11px;padding:2px 8px;background:none;border:1px solid #e5e7eb;color:#9ca3af;border-radius:4px;cursor:pointer;margin-top:2px}
.forget-btn:hover{border-color:#ef4444;color:#ef4444}
/* timeline */
.tl-nav{display:flex;align-items:center;justify-content:space-between;margin-bottom:18px}
.tl-nav-btns{display:flex;gap:4px}
.tl-arrow{font-size:16px;padding:4px 10px;border:1px solid #e5e7eb;border-radius:4px;cursor:pointer;background:#fff;color:#374151;line-height:1}
.tl-arrow:hover{background:#f3f4f6}
.tl-label{font-size:13px;font-weight:600;color:#111}
.tl-mode{display:flex;gap:0;border:1px solid #e5e7eb;border-radius:5px;overflow:hidden}
.tl-mode-btn{font-size:11px;font-weight:500;padding:4px 10px;border:none;cursor:pointer;background:#fff;color:#6b7280}
.tl-mode-btn.active{background:#111;color:#fff}
.kw-list{margin-top:4px}
.kw-row{display:flex;align-items:center;gap:8px;padding:5px 0;border-bottom:1px solid #f3f4f6}
.kw-row:last-child{border-bottom:none}
.kw-rank{font-size:11px;color:#9ca3af;width:18px;text-align:right;flex-shrink:0}
.kw-word{font-size:13px;font-weight:500;color:#111;width:120px;flex-shrink:0}
.kw-bar-wrap{flex:1;background:#f3f4f6;border-radius:3px;height:6px;overflow:hidden}
.kw-bar{height:100%;background:#6366f1;border-radius:3px;transition:width .3s}
/* blacklist */
.bl-row{display:flex;gap:8px;margin-bottom:4px}
.bl-entry{display:flex;align-items:center;justify-content:space-between;font-size:13px;color:#374151;background:#f9fafb;border:1px solid #e5e7eb;border-radius:5px;padding:6px 10px}
.bl-remove{background:none;border:none;cursor:pointer;font-size:16px;color:#9ca3af;line-height:1;padding:0 0 0 10px}
.bl-remove:hover{color:#ef4444}
.kw-count{font-size:11px;color:#9ca3af;width:32px;text-align:right;flex-shrink:0}
.tl-meta{font-size:11px;color:#9ca3af;margin-top:12px}
.kw-row{cursor:pointer;flex-wrap:wrap;border-radius:4px;margin:0 -4px;padding:5px 4px}
.kw-row:hover{background:#f9fafb}
.kw-arrow{font-size:9px;color:#9ca3af;width:12px;flex-shrink:0;display:inline-block;transition:transform .15s;user-select:none;margin-top:1px}
.kw-row.open>.kw-arrow{transform:rotate(90deg)}
.kw-pages{width:100%;margin-top:6px;padding-left:30px;display:none;border-top:1px solid #f3f4f6;padding-top:6px}
.kw-row.open>.kw-pages{display:block}
.kw-pages-empty{font-size:12px;color:#9ca3af;padding:4px 0}
.kw-page-item{padding:5px 0;border-bottom:1px solid #f9fafb}
.kw-page-item:last-child{border-bottom:none}
.kw-page-meta{display:flex;gap:6px;align-items:baseline;margin-bottom:1px}
.kw-page-domain{font-size:11px;color:#6366f1;font-weight:500}
.kw-page-time{font-size:11px;color:#9ca3af}
.kw-page-title{font-size:12px;color:#374151;text-decoration:none;display:block;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.kw-page-title:hover{color:#6366f1}
/* history timeline */
.hist-chart{margin-bottom:18px;overflow:hidden;border-radius:4px}
.hist-chart svg rect{cursor:pointer}
.hist-day-label{display:flex;justify-content:space-between;font-size:10px;color:#9ca3af;margin-top:4px;padding:0 1px}
.hist-selected-day{font-size:13px;font-weight:600;color:#111;margin-bottom:10px;padding-bottom:6px;border-bottom:1px solid #f3f4f6}
.hist-group{margin-bottom:16px}
.hist-date{font-size:11px;font-weight:600;color:#9ca3af;text-transform:uppercase;letter-spacing:.04em;margin-bottom:6px;padding-bottom:4px;border-bottom:1px solid #f3f4f6}
.hist-page{padding:8px 0;border-bottom:1px solid #f9fafb}
.hist-page:last-child{border-bottom:none}
.hist-page-meta{display:flex;gap:6px;align-items:baseline;margin-bottom:2px}
.hist-domain{font-size:11px;color:#6366f1;font-weight:500}
.hist-page-title{font-size:13px;font-weight:500;color:#111;text-decoration:none;display:block;margin-bottom:4px}
.hist-page-title:hover{color:#6366f1}
.hist-kws{display:flex;flex-wrap:wrap;gap:4px}
.hist-kw{font-size:11px;color:#6b7280;background:#f3f4f6;border-radius:3px;padding:1px 6px}
.hist-kw.active{background:#111;color:#fff}
.confidence{display:inline-flex;align-items:center;gap:6px;font-size:11px;color:#6b7280;margin-left:auto;margin-right:10px}
.confidence-bar{display:inline-block;width:60px;height:6px;background:#f3f4f6;border-radius:3px;overflow:hidden;vertical-align:middle}
.confidence-fill{display:block;height:100%;background:linear-gradient(90deg,#ef4444 0%,#f59e0b 50%,#10b981 100%)}
.source-badge{font-size:10px;font-weight:600;letter-spacing:.3px;text-transform:uppercase;padding:1px 6px;border-radius:3px}
.source-badge.indexed{background:#dcfce7;color:#166534}
.source-badge.history{background:#e0e7ff;color:#3730a3}
.snippet+.snippet{margin-top:4px;padding-top:4px;border-top:1px dashed #f3f4f6}
/* pending tag chip on result cards */
.tag-chip.pending{outline:2px dashed #6366f1;outline-offset:1px}
/* edit + apply controls per result */
.edit-btn{font-size:11px;padding:2px 8px;background:none;border:1px solid #e5e7eb;color:#9ca3af;border-radius:4px;cursor:pointer;margin-right:6px;float:right;margin-top:2px}
.edit-btn:hover{border-color:#6366f1;color:#6366f1}
.apply-btn{display:inline-block;margin-top:8px;font-size:11px;padding:4px 12px;background:#6366f1;color:#fff;border:none;border-radius:4px;cursor:pointer;font-weight:500;float:right}
.apply-btn:hover{background:#4f46e5}
.apply-row{margin-top:6px;display:flex;justify-content:flex-end}
/* edit panel inside a result */
.edit-panel{margin-top:10px;padding:10px;background:#f9fafb;border:1px solid #e5e7eb;border-radius:5px}
.edit-panel-title{font-size:11px;font-weight:600;color:#374151;text-transform:uppercase;letter-spacing:.04em;margin-bottom:6px}
.edit-panel input[type=text]{width:100%;padding:6px 8px;border:1px solid #d1d5db;border-radius:4px;font-size:12px;outline:none;background:#fff}
.edit-panel input[type=text]:focus{border-color:#6366f1;box-shadow:0 0 0 2px rgba(99,102,241,.12)}
.edit-actions{display:flex;gap:6px;justify-content:flex-end;margin-top:8px}
.edit-actions button{font-size:11px;padding:4px 10px;border-radius:4px;cursor:pointer;font-weight:500;border:1px solid transparent}
.edit-save{background:#111;color:#fff}
.edit-save:hover{background:#374151}
.edit-cancel{background:#fff;color:#6b7280;border-color:#d1d5db}
.edit-cancel:hover{border-color:#9ca3af;color:#374151}
/* search sidebar layout */
.search-layout{display:flex;gap:24px;align-items:flex-start}
.search-sidebar{width:240px;flex-shrink:0;position:sticky;top:16px;background:#fff;border:1px solid #e5e7eb;border-radius:6px;padding:14px}
.search-main{flex:1;min-width:0}
.filter-group{margin-bottom:18px}
.filter-group:last-of-type{margin-bottom:12px}
.filter-title{font-size:11px;font-weight:600;color:#374151;text-transform:uppercase;letter-spacing:.04em;margin-bottom:8px;display:flex;justify-content:space-between;align-items:baseline}
.filter-title .v{color:#9ca3af;font-weight:500;text-transform:none;letter-spacing:0;font-size:11px}
.filter-empty{font-size:12px;color:#9ca3af;font-style:italic}
.filter-search{width:100%;box-sizing:border-box;padding:7px 12px;margin-bottom:8px;border:1px solid #e5e7eb;border-radius:6px;font-size:13px;outline:none;background:#fff;color:#111}
.filter-search:focus{border-color:#111}
.filter-chips{display:flex;flex-wrap:wrap;gap:4px}
.tag-chip{font-size:11px;padding:2px 8px;border-radius:11px;cursor:pointer;border:1px solid transparent;background:#f3f4f6;color:#374151;user-select:none;line-height:1.5}
.tag-chip:hover{background:#e5e7eb}
.tag-chip.include{background:#111;color:#fff;border-color:#111}
.tag-chip.exclude{background:#fff;color:#dc2626;border-color:#dc2626;text-decoration:line-through}
.tag-chip.exclude::before{content:"−";margin-right:3px}
.tag-chip .count{margin-left:4px;opacity:.6;font-size:10px}
input[type=range]#conf-slider{width:100%;accent-color:#111}
.source-options{display:flex;flex-direction:column;gap:5px}
.source-options label{font-size:12px;color:#374151;cursor:pointer;display:flex;align-items:center;gap:6px}
.source-options input{accent-color:#111;cursor:pointer}
#filter-clear{font-size:11px;padding:5px 10px;background:#fff;border:1px solid #d1d5db;border-radius:4px;color:#6b7280;cursor:pointer;width:100%}
#filter-clear:hover{border-color:#111;color:#111}
@media (max-width:900px){.search-layout{flex-direction:column}.search-sidebar{width:100%;position:static}}
/* tag sidebar list (alphabetical, vertical) */
.tag-side{display:flex;justify-content:space-between;align-items:center;width:100%;font-size:12px;padding:5px 8px;border:1px solid transparent;background:transparent;color:#374151;cursor:pointer;border-radius:4px;text-align:left;line-height:1.4}
.tag-side+.tag-side{margin-top:1px}
.tag-side:hover{background:#f3f4f6}
.tag-side.active{background:#111;color:#fff}
.tag-side .count{opacity:.55;font-size:11px;margin-left:8px;flex-shrink:0}
.tags-list-scroll{max-height:70vh;overflow-y:auto}
/* word cloud */
.cloud-hero{font-size:12px;color:#9ca3af;text-align:center;padding:8px 0 4px}
.tag-cloud{display:flex;flex-wrap:wrap;justify-content:center;align-items:center;gap:6px 14px;padding:32px 16px;line-height:1.1}
.cloud-word{font-weight:700;cursor:pointer;display:inline-block;transition:transform .12s,opacity .12s;opacity:.85;letter-spacing:-.01em}
.cloud-word:hover{transform:scale(1.06);opacity:1}
</style>
</head>
<body>
<div class="wrap">
  <header>
    <h1>Vector Bookmark</h1>
    <span id="stat"></span>
  </header>
  <div class="tabs">
    <button class="tab active" data-panel="search-panel">Search</button>
    <button class="tab" data-panel="tags-panel">Tags</button>
    <button class="tab" data-panel="hotwords-panel">Hot Words</button>
    <button class="tab" data-panel="hist-panel">Timeline</button>
    <button class="tab" data-panel="blacklist-panel">Exclusions</button>
  </div>

  <div id="search-panel" class="panel active">
    <div class="search-row">
      <input id="q" type="search" placeholder="Search your reading history..." autofocus>
      <button id="search-btn">Search</button>
    </div>
    <div class="search-layout">
      <aside class="search-sidebar">
        <div class="filter-group">
          <div class="filter-title">Tags</div>
          <input id="tag-chips-filter" class="filter-search" type="search" placeholder="Filter tags…" aria-label="Filter tags">
          <div id="tag-chips" class="filter-chips">
            <div class="filter-empty">Run a search to filter by tags</div>
          </div>
        </div>
        <div class="filter-group">
          <div class="filter-title">Min confidence <span id="conf-value" class="v">0%</span></div>
          <input id="conf-slider" type="range" min="0" max="100" value="0" step="5">
        </div>
        <div class="filter-group">
          <div class="filter-title">Max results</div>
          <input id="limit-input" type="number" min="1" max="1000" value="20" step="1" style="width:100%;padding:6px 10px;border:1px solid #d1d5db;border-radius:6px;font-size:13px;outline:none;background:#fff;box-sizing:border-box">
        </div>
        <div class="filter-group">
          <div class="filter-title">Source</div>
          <div class="source-options">
            <label><input type="radio" name="source" value="" checked>Both</label>
            <label><input type="radio" name="source" value="indexed">Indexed only</label>
            <label><input type="radio" name="source" value="history">History only</label>
          </div>
        </div>
        <button id="filter-clear" type="button">Clear filters</button>
      </aside>
      <div class="search-main">
        <div id="results"></div>
      </div>
    </div>
  </div>

  <div id="tags-panel" class="panel">
    <div class="search-layout">
      <aside class="search-sidebar">
        <div class="filter-group" style="margin-bottom:0">
          <div class="filter-title">Tags <span id="tags-count" class="v"></span></div>
          <input id="tags-list-filter" class="filter-search" type="search" placeholder="Filter tags…" aria-label="Filter tags">
          <div id="tags-list" class="tags-list-scroll"></div>
        </div>
      </aside>
      <div class="search-main">
        <div id="tags-pages"></div>
      </div>
    </div>
  </div>

  <div id="hotwords-panel" class="panel">
    <div class="tl-nav">
      <div class="tl-nav-btns">
        <button class="tl-arrow" id="tl-prev">&#8592;</button>
        <button class="tl-arrow" id="tl-next">&#8594;</button>
      </div>
      <span class="tl-label" id="tl-label"></span>
      <div class="tl-mode">
        <button class="tl-mode-btn active" id="tl-week">Week</button>
        <button class="tl-mode-btn" id="tl-month">Month</button>
      </div>
    </div>
    <div id="tl-results"></div>
  </div>

  <div id="blacklist-panel" class="panel">
    <div class="bl-row">
      <input id="bl-input" type="text" placeholder="example.com or /regex/" style="flex:1;padding:8px 12px;border:1px solid #d1d5db;border-radius:6px;font-size:13px;outline:none;background:#fff">
      <button id="bl-add" style="padding:8px 16px;border:none;border-radius:6px;font-size:13px;font-weight:500;cursor:pointer;background:#111;color:#fff;white-space:nowrap">Exclude</button>
    </div>
    <input id="bl-search" type="search" placeholder="Filter exclusions…" style="width:100%;margin-top:10px;padding:7px 12px;border:1px solid #e5e7eb;border-radius:6px;font-size:13px;outline:none;background:#fff;display:none">
    <div id="bl-list" style="margin-top:8px;display:flex;flex-direction:column;gap:4px"></div>
    <div id="bl-empty" class="empty" style="display:none">No exclusions</div>
  </div>

  <div id="hist-panel" class="panel">
    <div class="tl-nav">
      <div class="tl-nav-btns">
        <button class="tl-arrow" id="ht-prev">&#8592;</button>
        <button class="tl-arrow" id="ht-next">&#8594;</button>
      </div>
      <span class="tl-label" id="ht-label"></span>
      <div class="tl-mode">
        <button class="tl-mode-btn active" id="ht-week">Week</button>
        <button class="tl-mode-btn" id="ht-month">Month</button>
      </div>
    </div>
    <div id="ht-chart"></div>
    <div id="ht-results"></div>
  </div>
</div>
<script>
// Auth bootstrap: capture ?token=... from URL into sessionStorage, strip it
// from the address bar, and inject Authorization on every fetch. On 401,
// prompt once for the token and reload. No-op when daemon has no auth.
(function(){
  try {
    var u = new URL(window.location.href);
    var t = u.searchParams.get('token');
    if (t) {
      sessionStorage.setItem('vbmAuthToken', t);
      u.searchParams.delete('token');
      window.history.replaceState({}, '', u.pathname + (u.search ? u.search : '') + u.hash);
    }
  } catch(e) {}
  var origFetch = window.fetch.bind(window);
  var prompted = false;
  window.fetch = function(input, init) {
    init = init || {};
    var token = sessionStorage.getItem('vbmAuthToken');
    if (token) {
      var hdrs = new Headers(init.headers || {});
      if (!hdrs.has('Authorization')) hdrs.set('Authorization', 'Bearer ' + token);
      init.headers = hdrs;
    }
    return origFetch(input, init).then(function(res){
      if (res.status === 401 && !prompted) {
        prompted = true;
        var current = sessionStorage.getItem('vbmAuthToken') || '';
        var entered = window.prompt('VBM auth token required:', current);
        if (entered && entered !== current) {
          sessionStorage.setItem('vbmAuthToken', entered);
          window.location.reload();
        }
      }
      return res;
    });
  };
})();
var q=document.getElementById('q'),btn=document.getElementById('search-btn'),res=document.getElementById('results'),stat=document.getElementById('stat')
fetch('/status').then(function(r){return r.json()}).then(function(d){stat.textContent=d.indexed+' pages indexed'}).catch(function(){})
function esc(s){return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;')}
function fmt(ts){var d=new Date(ts),diff=Date.now()-ts;if(diff<86400000)return d.toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'});if(diff<604800000)return d.toLocaleDateString([],{weekday:'short'});return d.toLocaleDateString([],{month:'short',day:'numeric'})}
// ── search filter state ───────────────────────────────────────────────────────
// Single source of truth for the sidebar filters. Mutating any of these and
// calling fireSearch() re-runs the query with the current state.
var filterState={tags:{},minConf:0,source:''}
// Per-result tag clicks STAGE here without firing a search. Apply button on a
// result merges these into filterState and fires the search.
var pendingTagChanges={}
// Cache last results so we can repaint after staging changes without re-fetching.
var lastResults=[],lastTopScore=0
var knownTagsForQuery={} // tag -> count seen in the most recent unfiltered run
var tagChips=document.getElementById('tag-chips')
var confSlider=document.getElementById('conf-slider')
var confValue=document.getElementById('conf-value')
var filterClearBtn=document.getElementById('filter-clear')
var sourceRadios=document.querySelectorAll('input[name="source"]')
var lastQuery=''

function getMaxResults(){
  var el=document.getElementById('limit-input')
  var n=parseInt(el&&el.value,10)
  if(!isFinite(n)||n<1)n=20
  if(n>1000)n=1000
  return n
}
function buildSearchURL(query){
  var url='/search?q='+encodeURIComponent(query)+'&limit='+getMaxResults()
  Object.keys(filterState.tags).forEach(function(t){
    var st=filterState.tags[t]
    if(st==='include')url+='&tag='+encodeURIComponent(t)
    else if(st==='exclude')url+='&neg_tag='+encodeURIComponent(t)
  })
  if(filterState.minConf>0)url+='&min_confidence='+(filterState.minConf/100).toFixed(2)
  if(filterState.source)url+='&source='+filterState.source
  return url
}

var tagChipsFilter=document.getElementById('tag-chips-filter')
function renderTagChips(){
  var keys=Object.keys(knownTagsForQuery).sort()
  if(!keys.length){
    tagChips.innerHTML='<div class="filter-empty">Run a search to filter by tags</div>'
    return
  }
  var q=(tagChipsFilter&&tagChipsFilter.value||'').trim().toLowerCase()
  if(q)keys=keys.filter(function(t){return t.toLowerCase().indexOf(q)!==-1})
  if(!keys.length){
    tagChips.innerHTML='<div class="filter-empty">No tags match</div>'
    return
  }
  tagChips.innerHTML=keys.map(function(t){
    var st=filterState.tags[t]||''
    var cls='tag-chip'+(st?(' '+st):'')
    var title=st==='include'?'Click to exclude':st==='exclude'?'Click to clear':'Click to require'
    return '<span class="'+cls+'" data-tag="'+esc(t)+'" title="'+title+'">'+esc(t)+'<span class="count">'+knownTagsForQuery[t]+'</span></span>'
  }).join('')
}
if(tagChipsFilter)tagChipsFilter.addEventListener('input',renderTagChips)

tagChips.addEventListener('click',function(e){
  var c=e.target.closest('.tag-chip');if(!c)return
  var tag=c.dataset.tag
  var st=filterState.tags[tag]
  // Cycle: neutral → include → exclude → neutral
  if(!st)filterState.tags[tag]='include'
  else if(st==='include')filterState.tags[tag]='exclude'
  else delete filterState.tags[tag]
  renderTagChips();fireSearch()
})

var slDebounce=null
confSlider.addEventListener('input',function(){
  filterState.minConf=parseInt(confSlider.value,10)||0
  confValue.textContent=filterState.minConf+'%'
  if(slDebounce)clearTimeout(slDebounce)
  slDebounce=setTimeout(fireSearch,200)
})
sourceRadios.forEach(function(r){r.addEventListener('change',function(){
  filterState.source=r.value;fireSearch()
})})
var limitInput=document.getElementById('limit-input')
var limitDebounce=null
function clampLimitInput(){
  var n=parseInt(limitInput.value,10)
  if(!isFinite(n)||n<1)n=20
  if(n>1000)n=1000
  limitInput.value=n
}
limitInput.addEventListener('input',function(){
  if(limitDebounce)clearTimeout(limitDebounce)
  limitDebounce=setTimeout(function(){clampLimitInput();fireSearch()},400)
})
limitInput.addEventListener('change',function(){clampLimitInput();fireSearch()})
filterClearBtn.addEventListener('click',function(){
  filterState={tags:{},minConf:0,source:''}
  confSlider.value=0;confValue.textContent='0%'
  limitInput.value=20
  document.querySelector('input[name="source"][value=""]').checked=true
  renderTagChips();fireSearch()
})

function effectiveTagState(t){
  if(pendingTagChanges.hasOwnProperty(t))return pendingTagChanges[t]||''
  return filterState.tags[t]||''
}

// paint renders results from the cached lastResults without re-fetching.
// Called after staging tag changes (in-result clicks) so the user sees the
// pending state without an immediate fetch.
function paint(){
  var list=lastResults,top=lastTopScore
  if(!list||!list.length){res.innerHTML='<div class="empty">No results</div>';return}
  res.innerHTML='<div class="count">'+list.length+' result'+(list.length>1?'s':'')+'</div>'+list.map(function(r,idx){
    var pct=top>0?Math.round((r.score/top)*100):0
    var snips=(r.snippets&&r.snippets.length?r.snippets:[r.snippet||''])
      .map(function(s){return '<div class="snippet">'+esc(s)+'</div>'}).join('')
    var resultTags=r.tags||[]
    var hasPendingForThis=resultTags.some(function(t){return pendingTagChanges.hasOwnProperty(t)})
    var tagsHTML=resultTags.map(function(t){
      var st=effectiveTagState(t)
      var dirty=pendingTagChanges.hasOwnProperty(t)?' pending':''
      var cls='tag-chip'+(st?(' '+st):'')+dirty
      var title=dirty?'Pending — click Apply filter to commit':st==='include'?'Click to exclude':st==='exclude'?'Click to clear':'Click to require'
      return '<span class="'+cls+'" data-tag="'+esc(t)+'" title="'+title+'">'+esc(t)+'</span>'
    }).join('')
    var conf=top>0?'<span class="confidence" title="'+pct+'% relative to top match"><span class="confidence-bar"><span class="confidence-fill" style="width:'+pct+'%"></span></span><span>'+pct+'%</span></span>':''
    var src=r.source==='indexed'?'indexed':'history'
    var srcLabel=src==='indexed'?'indexed':'history'
    var badge='<span class="source-badge '+src+'" title="'+(src==='indexed'?'Manually indexed via the popup':'Captured passively from your browsing history')+'">'+srcLabel+'</span>'
    var applyRow=hasPendingForThis?'<div class="apply-row"><button class="apply-btn" type="button">Apply filter</button></div>':''
    return '<div class="result" data-idx="'+idx+'" data-url="'+esc(r.url)+'">'+
      '<button class="forget-btn" data-url="'+esc(r.url)+'">forget</button>'+
      '<button class="edit-btn" type="button" title="Edit tags">edit</button>'+
      '<div class="result-meta"><span class="domain">'+esc(r.domain)+'</span><span class="date">'+fmt(r.visitTs)+'</span>'+badge+conf+'</div>'+
      '<a class="result-title" href="'+esc(r.url)+'" target="_blank">'+esc(r.title||r.url)+'</a>'+
      snips+
      (tagsHTML?'<div class="hist-kws" style="margin-top:6px">'+tagsHTML+'</div>':'')+
      applyRow+
    '</div>'
  }).join('')
}

function applyPending(){
  Object.keys(pendingTagChanges).forEach(function(t){
    var v=pendingTagChanges[t]
    if(v)filterState.tags[t]=v
    else delete filterState.tags[t]
  })
  pendingTagChanges={}
  renderTagChips();fireSearch()
}

function search(isNewQuery){
  var v=q.value.trim();if(!v)return
  // New query: reset filter state so chips reflect this query's tag universe.
  if(isNewQuery && v!==lastQuery){
    filterState={tags:{},minConf:0,source:''}
    pendingTagChanges={}
    knownTagsForQuery={}
    confSlider.value=0;confValue.textContent='0%'
    document.querySelector('input[name="source"][value=""]').checked=true
  }
  lastQuery=v
  btn.disabled=true;btn.textContent='...'
  fetch(buildSearchURL(v)).then(function(r){return r.json()}).then(function(data){
    var list=data.results||[]
    // First unfiltered run for this query: harvest tag universe for the chips.
    var anyFilterActive=Object.keys(filterState.tags).length>0||filterState.minConf>0||filterState.source
    if(!anyFilterActive){
      knownTagsForQuery={}
      list.forEach(function(r){(r.tags||[]).forEach(function(t){
        knownTagsForQuery[t]=(knownTagsForQuery[t]||0)+1
      })})
      renderTagChips()
    }
    lastResults=list;lastTopScore=list.length?(list[0].score||0):0
    paint()
  }).catch(function(){res.innerHTML='<div class="empty">Search failed</div>'})
  .finally(function(){btn.disabled=false;btn.textContent='Search'})
}
function fireSearch(){search(false)}
btn.addEventListener('click',function(){search(true)})
q.addEventListener('keydown',function(e){if(e.key==='Enter')search(true)})
res.addEventListener('click',function(e){
  var f=e.target.closest('.forget-btn')
  if(f){
    if(!confirm('Forget this page? This permanently deletes its chunks, embeddings, and tags. This cannot be undone.'))return
    f.textContent='...'
    fetch('/forget',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({type:'url',value:f.dataset.url})})
      .then(function(){f.closest('.result').remove()}).catch(function(){f.textContent='err'})
    return
  }
  // Apply staged tag changes: merge into filterState and re-fetch.
  if(e.target.closest('.apply-btn')){applyPending();return}
  // Edit button: toggle the inline tag editor on this result.
  var ed=e.target.closest('.edit-btn')
  if(ed){toggleEditPanel(ed.closest('.result'));return}
  // Save / Cancel inside the edit panel.
  if(e.target.closest('.edit-save')){saveEditPanel(e.target.closest('.result'));return}
  if(e.target.closest('.edit-cancel')){closeEditPanel(e.target.closest('.result'));return}
  // Per-result tag chip: STAGE the change in pendingTagChanges (does NOT fire
  // the search). User clicks Apply filter when satisfied with the staging.
  var c=e.target.closest('.tag-chip[data-tag]')
  if(c){
    var tag=c.dataset.tag
    var cur=effectiveTagState(tag)
    var nxt=cur===''?'include':cur==='include'?'exclude':''
    // If the new state matches the live filterState, drop from pending (no-op).
    if((filterState.tags[tag]||'')===nxt){
      delete pendingTagChanges[tag]
    } else {
      pendingTagChanges[tag]=nxt||null
    }
    paint()
  }
})

// ── inline edit panel (per-result tag editor) ────────────────────────────────
function toggleEditPanel(card){
  if(!card)return
  var existing=card.querySelector('.edit-panel')
  if(existing){existing.remove();return}
  var idx=parseInt(card.dataset.idx,10)
  var r=lastResults[idx];if(!r)return
  var initial=(r.tags||[]).join(', ')
  var html=
    '<div class="edit-panel">'+
      '<div class="edit-panel-title">Edit tags</div>'+
      '<input type="text" class="edit-tags-input" value="'+esc(initial)+'" placeholder="comma-separated tags">'+
      '<div class="edit-actions">'+
        '<button type="button" class="edit-cancel">Cancel</button>'+
        '<button type="button" class="edit-save">Save</button>'+
      '</div>'+
    '</div>'
  card.insertAdjacentHTML('beforeend',html)
  var inp=card.querySelector('.edit-tags-input');if(inp)inp.focus()
}
function closeEditPanel(card){
  if(!card)return
  var p=card.querySelector('.edit-panel');if(p)p.remove()
}
function saveEditPanel(card){
  if(!card)return
  var idx=parseInt(card.dataset.idx,10)
  var r=lastResults[idx];if(!r)return
  var inp=card.querySelector('.edit-tags-input');if(!inp)return
  var saveBtn=card.querySelector('.edit-save');if(saveBtn){saveBtn.disabled=true;saveBtn.textContent='Saving…'}
  var newTags=inp.value.split(',').map(function(s){return s.trim()}).filter(Boolean)
  fetch('/page/tags',{
    method:'PUT',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify({url:r.url,tags:newTags})
  }).then(function(resp){
    if(!resp.ok)throw new Error('http '+resp.status)
    return resp.json()
  }).then(function(d){
    lastResults[idx].tags=d.tags||[]
    closeEditPanel(card);paint()
  }).catch(function(err){
    if(saveBtn){saveBtn.disabled=false;saveBtn.textContent='Save'}
    alert('Failed to save tags: '+err.message)
  })
}

// ── tabs ──────────────────────────────────────────────────────────────────────
document.querySelectorAll('.tab').forEach(function(t){
  t.addEventListener('click',function(){
    document.querySelectorAll('.tab').forEach(function(x){x.classList.remove('active')})
    document.querySelectorAll('.panel').forEach(function(x){x.classList.remove('active')})
    t.classList.add('active')
    document.getElementById(t.dataset.panel).classList.add('active')
    if(t.dataset.panel==='hotwords-panel') tlRender()
    if(t.dataset.panel==='hist-panel') htRender()
  })
})

// ── timeline ──────────────────────────────────────────────────────────────────
var tlMode='week', tlAnchor=tlWeekStart(new Date())
function tlWeekStart(d){var x=new Date(d);x.setHours(0,0,0,0);x.setDate(x.getDate()-x.getDay());return x}
function tlMonthStart(d){var x=new Date(d);x.setDate(1);x.setHours(0,0,0,0);return x}
function tlPeriod(){
  var from,to
  if(tlMode==='week'){
    from=new Date(tlAnchor);to=new Date(tlAnchor);to.setDate(to.getDate()+7)
  } else {
    from=new Date(tlAnchor);to=new Date(tlAnchor);to.setMonth(to.getMonth()+1)
  }
  return{from:from.getTime(),to:to.getTime()}
}
function tlLabel(){
  if(tlMode==='week'){
    var a=tlAnchor,b=new Date(tlAnchor);b.setDate(b.getDate()+6)
    var mo=a.toLocaleDateString([],{month:'short'}),y=a.getFullYear()
    return 'Week of '+mo+' '+a.getDate()+'–'+b.getDate()+', '+y
  } else {
    return tlAnchor.toLocaleDateString([],{month:'long',year:'numeric'})
  }
}
var tlHistoryCache=null
function tlRender(){
  var p=tlPeriod()
  tlHistoryCache=null
  document.getElementById('tl-label').textContent=tlLabel()
  document.getElementById('tl-results').innerHTML='<div class="empty" style="padding:20px 0">Loading…</div>'
  Promise.all([
    fetch('/topics?from='+p.from+'&to='+p.to+'&limit=20').then(function(r){return r.json()}),
    fetch('/history?from='+p.from+'&to='+p.to+'&limit=100').then(function(r){return r.json()})
  ]).then(function(res){
    var d=res[0],h=res[1]
    tlHistoryCache=h.pages||[]
    var kws=d.keywords||[]
    if(!kws.length){
      document.getElementById('tl-results').innerHTML='<div class="empty">No pages indexed in this period</div>'
      return
    }
    var max=kws[0].count
    document.getElementById('tl-results').innerHTML=
      '<div class="kw-list">'+
      kws.map(function(k,i){
        var pct=Math.round(k.count/max*100)
        return '<div class="kw-row" data-word="'+esc(k.word)+'">'+
          '<span class="kw-arrow">&#9658;</span>'+
          '<span class="kw-rank">'+(i+1)+'</span>'+
          '<span class="kw-word">'+esc(k.word)+'</span>'+
          '<div class="kw-bar-wrap"><div class="kw-bar" style="width:'+pct+'%"></div></div>'+
          '<span class="kw-count">'+k.count+'</span>'+
          '<div class="kw-pages"></div>'+
        '</div>'
      }).join('')+
      '</div>'+
      '<div class="tl-meta">'+kws.length+' terms &middot; '+d.total_chunks+' chunks analyzed</div>'
  })
  .catch(function(){document.getElementById('tl-results').innerHTML='<div class="empty">Failed to load</div>'})
}
// Delegated click handler — added once, survives tlRender() re-renders
document.getElementById('tl-results').addEventListener('click',function(e){
  var row=e.target.closest('.kw-row');if(!row)return
  var wasOpen=row.classList.contains('open')
  document.querySelectorAll('#tl-results .kw-row.open').forEach(function(r){
    r.classList.remove('open');r.querySelector('.kw-pages').innerHTML=''
  })
  if(wasOpen)return
  row.classList.add('open')
  var word=row.dataset.word
  var el=row.querySelector('.kw-pages')
  el.innerHTML='<div class="kw-pages-empty">Loading…</div>'
  var p=tlPeriod()
  fetch('/search?q='+encodeURIComponent(word)+'&limit=20')
    .then(function(r){return r.json()})
    .then(function(data){
      // Cross-filter by current period — search returns all time, we scope to period
      var results=(data.results||[]).filter(function(r){return r.visitTs>=p.from&&r.visitTs<p.to})
      if(!results.length){el.innerHTML='<div class="kw-pages-empty">No pages found in this period</div>';return}
      el.innerHTML=results.map(function(r){
        var d=new Date(r.visitTs)
        var t=d.toLocaleDateString([],{month:'short',day:'numeric'})+' '+d.toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'})
        return '<div class="kw-page-item">'+
          '<div class="kw-page-meta"><span class="kw-page-domain">'+esc(r.domain)+'</span><span class="kw-page-time">'+t+'</span></div>'+
          '<a class="kw-page-title" href="'+esc(r.url)+'" target="_blank">'+esc(r.title||r.url)+'</a>'+
        '</div>'
      }).join('')
    })
    .catch(function(){el.innerHTML='<div class="kw-pages-empty">Failed to load</div>'})
})
document.getElementById('tl-prev').addEventListener('click',function(){
  if(tlMode==='week') tlAnchor.setDate(tlAnchor.getDate()-7)
  else tlAnchor.setMonth(tlAnchor.getMonth()-1)
  tlRender()
})
document.getElementById('tl-next').addEventListener('click',function(){
  if(tlMode==='week') tlAnchor.setDate(tlAnchor.getDate()+7)
  else tlAnchor.setMonth(tlAnchor.getMonth()+1)
  tlRender()
})
document.getElementById('tl-week').addEventListener('click',function(){
  tlMode='week';tlAnchor=tlWeekStart(new Date())
  document.getElementById('tl-week').classList.add('active')
  document.getElementById('tl-month').classList.remove('active')
  tlRender()
})
document.getElementById('tl-month').addEventListener('click',function(){
  tlMode='month';tlAnchor=tlMonthStart(new Date())
  document.getElementById('tl-week').classList.remove('active')
  document.getElementById('tl-month').classList.add('active')
  tlRender()
})
// ── history timeline ──────────────────────────────────────────────────────────
var htMode='week', htAnchor=tlWeekStart(new Date())
var htSelectedDay=null  // 'YYYY-MM-DD' (UTC) — the day whose pages are listed
var htDailyCache={}     // last fetched daily counts (for re-render without refetch)
function htPeriod(){
  var from,to
  if(htMode==='week'){from=new Date(htAnchor);to=new Date(htAnchor);to.setDate(to.getDate()+7)}
  else{from=new Date(htAnchor);to=new Date(htAnchor);to.setMonth(to.getMonth()+1)}
  return{from:from.getTime(),to:to.getTime()}
}
function htLabel(){
  if(htMode==='week'){
    var a=htAnchor,b=new Date(htAnchor);b.setDate(b.getDate()+6)
    return 'Week of '+a.toLocaleDateString([],{month:'short'})+' '+a.getDate()+'–'+b.getDate()+', '+a.getFullYear()
  }
  return htAnchor.toLocaleDateString([],{month:'long',year:'numeric'})
}
// Build the list of UTC YYYY-MM-DD day keys covering [fromMs, toMs).
function htDaysInPeriod(fromMs,toMs){
  var days=[],d=new Date(fromMs)
  while(d.getTime()<toMs){days.push(new Date(d));d.setDate(d.getDate()+1)}
  if(htMode==='month'&&days.length>31)days=days.slice(0,31)
  return days
}
function htDayKey(d){return d.toISOString().slice(0,10)}
function htTodayKey(){return new Date().toISOString().slice(0,10)}
// Pick a sensible default day for the current period:
//   today if it falls in the period; else last day with traffic;
//   else the last day of the period.
function htClampSelectedDay(daily,fromMs,toMs){
  var days=htDaysInPeriod(fromMs,toMs)
  if(!days.length)return null
  var todayK=htTodayKey()
  for(var i=0;i<days.length;i++)if(htDayKey(days[i])===todayK)return todayK
  for(var j=days.length-1;j>=0;j--){var k=htDayKey(days[j]);if((daily[k]||0)>0)return k}
  return htDayKey(days[days.length-1])
}
function htBuildChart(daily,fromMs,toMs){
  var days=htDaysInPeriod(fromMs,toMs)
  var maxVal=0
  days.forEach(function(day){
    var k=htDayKey(day)
    if((daily[k]||0)>maxVal)maxVal=daily[k]||0
  })
  var W=640,H=56,n=days.length,bw=Math.floor((W-n)/(n||1)),gap=1
  var bars=days.map(function(day,i){
    var k=htDayKey(day),v=daily[k]||0
    var h=maxVal>0?Math.max(2,Math.round(v/maxVal*(H-4))):2
    var x=i*(bw+gap),y=H-h
    var fill=k===htSelectedDay?'#4338ca':(v>0?'#6366f1':'#e5e7eb')
    return'<rect data-day="'+k+'" x="'+x+'" y="'+y+'" width="'+bw+'" height="'+h+'" rx="1" fill="'+fill+'"><title>'+k+': '+v+' page'+(v!==1?'s':'')+'</title></rect>'
  }).join('')
  var firstLabel=days[0].toLocaleDateString([],{month:'short',day:'numeric'})
  var lastLabel=days[days.length-1].toLocaleDateString([],{month:'short',day:'numeric'})
  return'<div class="hist-chart"><svg viewBox="0 0 '+W+' '+H+'" width="100%" height="'+H+'" xmlns="http://www.w3.org/2000/svg">'+bars+'</svg>'+
    '<div class="hist-day-label"><span>'+esc(firstLabel)+'</span><span>'+esc(lastLabel)+'</span></div></div>'
}
// Bounds of a UTC YYYY-MM-DD day, in unix ms.
function htDayBounds(dayKey){
  var parts=dayKey.split('-')
  var from=Date.UTC(+parts[0],+parts[1]-1,+parts[2])
  return{from:from,to:from+86400000}
}
function htRenderChart(){
  var p=htPeriod()
  document.getElementById('ht-chart').innerHTML=htBuildChart(htDailyCache,p.from,p.to)
}
function htRenderListForSelectedDay(){
  var resEl=document.getElementById('ht-results')
  if(!htSelectedDay){resEl.innerHTML='<div class="empty">No day selected</div>';return}
  resEl.innerHTML='<div class="empty" style="padding:20px 0">Loading…</div>'
  var b=htDayBounds(htSelectedDay)
  var headerD=new Date(b.from)
  // toLocaleDateString in UTC keeps the label aligned with the UTC day key.
  var headerLabel=headerD.toLocaleDateString([],{weekday:'long',year:'numeric',month:'long',day:'numeric',timeZone:'UTC'})
  fetch('/history?from='+b.from+'&to='+b.to+'&limit=500')
    .then(function(r){return r.json()})
    .then(function(d){
      var pages=d.pages||[]
      var header='<div class="hist-selected-day">'+esc(headerLabel)+'</div>'
      if(!pages.length){resEl.innerHTML=header+'<div class="empty">No pages on this day</div>';return}
      resEl.innerHTML=header+pages.map(function(pg){
        var kws=(pg.keywords||[]).map(function(w){return'<span class="hist-kw">'+esc(w)+'</span>'}).join('')
        return'<div class="hist-page">'+
          '<div class="hist-page-meta"><span class="hist-domain">'+esc(pg.domain)+'</span><span style="font-size:11px;color:#9ca3af">'+new Date(pg.visitTs).toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'})+'</span></div>'+
          '<a class="hist-page-title" href="'+esc(pg.url)+'" target="_blank">'+esc(pg.title||pg.url)+'</a>'+
          (kws?'<div class="hist-kws">'+kws+'</div>':'')+
        '</div>'
      }).join('')
    })
    .catch(function(){resEl.innerHTML='<div class="empty">Failed to load</div>'})
}
function htSelectDay(day){
  if(!day||day===htSelectedDay)return
  htSelectedDay=day
  htRenderChart()
  htRenderListForSelectedDay()
}
function htRender(){
  var p=htPeriod()
  document.getElementById('ht-label').textContent=htLabel()
  document.getElementById('ht-chart').innerHTML=''
  document.getElementById('ht-results').innerHTML='<div class="empty" style="padding:20px 0">Loading…</div>'
  // Fetch 1: daily counts for the chart (limit=1 — pages list is unused here).
  fetch('/history?from='+p.from+'&to='+p.to+'&limit=1')
    .then(function(r){return r.json()})
    .then(function(d){
      htDailyCache=d.daily||{}
      htSelectedDay=htClampSelectedDay(htDailyCache,p.from,p.to)
      htRenderChart()
      htRenderListForSelectedDay()
    })
    .catch(function(){
      document.getElementById('ht-results').innerHTML='<div class="empty">Failed to load</div>'
    })
}
document.getElementById('ht-chart').addEventListener('click',function(e){
  var rect=e.target.closest('rect[data-day]');if(!rect)return
  htSelectDay(rect.getAttribute('data-day'))
})
document.getElementById('ht-prev').addEventListener('click',function(){
  if(htMode==='week')htAnchor.setDate(htAnchor.getDate()-7)
  else htAnchor.setMonth(htAnchor.getMonth()-1)
  htRender()
})
document.getElementById('ht-next').addEventListener('click',function(){
  if(htMode==='week')htAnchor.setDate(htAnchor.getDate()+7)
  else htAnchor.setMonth(htAnchor.getMonth()+1)
  htRender()
})
document.getElementById('ht-week').addEventListener('click',function(){
  htMode='week';htAnchor=tlWeekStart(new Date())
  document.getElementById('ht-week').classList.add('active')
  document.getElementById('ht-month').classList.remove('active')
  htRender()
})
document.getElementById('ht-month').addEventListener('click',function(){
  htMode='month';htAnchor=tlMonthStart(new Date())
  document.getElementById('ht-week').classList.remove('active')
  document.getElementById('ht-month').classList.add('active')
  htRender()
})
document.querySelectorAll('.tab').forEach(function(t){t.addEventListener('click',function(){
  if(t.dataset.panel==='hist-panel')htRender()
})})
// Blacklist panel
var blInput=document.getElementById('bl-input'),blList=document.getElementById('bl-list'),blEmpty=document.getElementById('bl-empty'),blSearch=document.getElementById('bl-search')
var blAll=[]
function blRender(patterns){
  blAll=patterns||[]
  blSearch.style.display=blAll.length?'block':'none'
  blFilter()
}
function blFilter(){
  var q=(blSearch.value||'').toLowerCase().trim()
  var filtered=q?blAll.filter(function(p){return p.toLowerCase().indexOf(q)!==-1}):blAll
  blList.innerHTML=''
  if(!filtered.length){blEmpty.style.display='';return}
  blEmpty.style.display='none'
  filtered.forEach(function(p){
    var d=document.createElement('div');d.className='bl-entry'
    var t=document.createElement('span');t.textContent=p
    var b=document.createElement('button');b.className='bl-remove';b.textContent='×';b.title='Remove'
    b.addEventListener('click',function(){
      fetch('/blacklist',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({pattern:p})})
        .then(function(){return fetch('/blacklist')}).then(function(r){return r.json()}).then(function(d){blRender(d.patterns)})
    })
    d.appendChild(t);d.appendChild(b);blList.appendChild(d)
  })
}
function blLoad(){fetch('/blacklist').then(function(r){return r.json()}).then(function(d){blRender(d.patterns)}).catch(function(){})}
blSearch.addEventListener('input',blFilter)
document.getElementById('bl-add').addEventListener('click',function(){
  var v=blInput.value.trim();if(!v)return
  fetch('/blacklist',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({pattern:v})})
    .then(function(){blInput.value='';return fetch('/blacklist')}).then(function(r){return r.json()}).then(function(d){blRender(d.patterns)})
})
blInput.addEventListener('keydown',function(e){if(e.key==='Enter')document.getElementById('bl-add').click()})
document.querySelectorAll('.tab').forEach(function(t){t.addEventListener('click',function(){
  if(t.dataset.panel==='blacklist-panel')blLoad()
})})

// ── tags ──────────────────────────────────────────────────────────────────────
var tagsList=document.getElementById('tags-list'),tagsPages=document.getElementById('tags-pages'),activeTag=null
var tagsListFilter=document.getElementById('tags-list-filter')
var tagsCache=[]
function paintTagsList(){
  var q=(tagsListFilter&&tagsListFilter.value||'').trim().toLowerCase()
  var sorted=tagsCache.slice().sort(function(a,b){return a.tag.localeCompare(b.tag)})
  if(q)sorted=sorted.filter(function(t){return t.tag.toLowerCase().indexOf(q)!==-1})
  if(!sorted.length){
    tagsList.innerHTML='<div class="filter-empty">'+(q?'No tags match':'No tags yet.')+'</div>'
    return
  }
  tagsList.innerHTML=sorted.map(function(t){
    var on=t.tag===activeTag
    return '<button class="tag-side'+(on?' active':'')+'" data-tag="'+esc(t.tag)+'">'+
      '<span>'+esc(t.tag)+'</span><span class="count">'+t.count+'</span></button>'
  }).join('')
}
function tagsRender(){
  fetch('/tags').then(function(r){return r.json()}).then(function(d){
    tagsCache=d.tags||[]
    var tagsCountEl=document.getElementById('tags-count')
    if(tagsCountEl)tagsCountEl.textContent=tagsCache.length?'('+tagsCache.length+')':''
    if(!tagsCache.length){
      tagsList.innerHTML='<div class="filter-empty">No tags yet.</div>'
      tagsPages.innerHTML='<div class="empty" style="padding:40px 0">No tags yet — open the popup, click <b>Index this site now</b> and fill the <b>Tags</b> field before <b>Confirm</b>.</div>'
      return
    }
    paintTagsList()
    if(activeTag)tagsLoadPages(activeTag)
    else renderTagCloud(tagsCache)
  }).catch(function(){tagsList.innerHTML='<div class="filter-empty">Failed to load tags</div>'})
}
if(tagsListFilter)tagsListFilter.addEventListener('input',paintTagsList)

// renderTagCloud shows a font-size-weighted cloud of all tags as the default
// view of the Tags tab when no tag is selected. Click on a word selects it.
function renderTagCloud(tags){
  var counts=tags.map(function(t){return t.count})
  var max=Math.max.apply(null,counts),min=Math.min.apply(null,counts)
  // Shuffle for organic placement.
  var arr=tags.slice().sort(function(){return Math.random()-0.5})
  var palette=['#ef4444','#f59e0b','#10b981','#3b82f6','#8b5cf6','#ec4899','#6366f1','#14b8a6','#f97316','#0ea5e9']
  var html=arr.map(function(t,i){
    var norm=max>min?(t.count-min)/(max-min):0.5
    var size=14+Math.round(norm*42) // 14px → 56px
    var color=palette[i%palette.length]
    return '<span class="cloud-word" data-tag="'+esc(t.tag)+'" title="'+esc(t.tag)+' — '+t.count+' page'+(t.count>1?'s':'')+'" style="font-size:'+size+'px;color:'+color+'">'+esc(t.tag)+'</span>'
  }).join('')
  tagsPages.innerHTML='<div class="cloud-hero">Click a tag to list its pages — bigger words = more pages</div><div class="tag-cloud">'+html+'</div>'
}
function tagsLoadPages(t){
  tagsPages.innerHTML='<div class="empty" style="padding:20px 0">Loading…</div>'
  fetch('/pages?tag='+encodeURIComponent(t)+'&limit=200').then(function(r){return r.json()}).then(function(d){
    var pages=d.pages||[]
    if(!pages.length){tagsPages.innerHTML='<div class="empty">No pages with this tag</div>';return}
    tagsPages.innerHTML='<div class="count">'+pages.length+' page'+(pages.length>1?'s':'')+'</div>'+pages.map(function(p){
      var allTags=(p.tags||[]).map(function(x){
        var cls='hist-kw'+(x===t?' active':'')
        return '<span class="'+cls+'">'+esc(x)+'</span>'
      }).join('')
      return '<div class="result">'+
        '<div class="result-meta"><span class="domain">'+esc(p.domain)+'</span><span class="date">'+fmt(p.visitTs)+'</span></div>'+
        '<a class="result-title" href="'+esc(p.url)+'" target="_blank">'+esc(p.title||p.url)+'</a>'+
        (allTags?'<div class="hist-kws" style="margin-top:4px">'+allTags+'</div>':'')+
      '</div>'
    }).join('')
  }).catch(function(){tagsPages.innerHTML='<div class="empty">Failed to load</div>'})
}
tagsList.addEventListener('click',function(e){
  var b=e.target.closest('.tag-side');if(!b)return
  activeTag=(activeTag===b.dataset.tag)?null:b.dataset.tag
  tagsRender()
})
// Word cloud click: same toggle behavior as the sidebar.
tagsPages.addEventListener('click',function(e){
  var b=e.target.closest('.cloud-word');if(!b)return
  activeTag=b.dataset.tag
  tagsRender()
})
document.querySelectorAll('.tab').forEach(function(t){t.addEventListener('click',function(){
  if(t.dataset.panel==='tags-panel')tagsRender()
})})
</script>
</body>
</html>`

// ── topic keyword extraction ──────────────────────────────────────────────────

var stopWords = map[string]bool{
	// English
	"the": true, "and": true, "for": true, "that": true, "this": true,
	"are": true, "was": true, "were": true, "been": true, "have": true,
	"has": true, "had": true, "will": true, "would": true, "could": true,
	"should": true, "may": true, "might": true, "can": true, "not": true,
	"from": true, "with": true, "they": true, "their": true, "there": true,
	"when": true, "where": true, "which": true, "what": true, "how": true,
	"also": true, "just": true, "like": true, "more": true, "some": true,
	"into": true, "over": true, "only": true, "other": true, "than": true,
	"then": true, "these": true, "those": true, "about": true, "after": true,
	"before": true, "between": true, "through": true, "each": true,
	"used": true, "using": true, "being": true, "make": true, "made": true,
	"such": true, "very": true, "well": true, "need": true, "here": true,
	"your": true, "our": true, "its": true, "you": true, "him": true,
	"her": true, "she": true, "his": true, "all": true, "any": true,
	// Portuguese
	"que": true, "para": true, "com": true, "por": true, "uma": true,
	"como": true, "mais": true, "muito": true, "isso": true, "isto": true,
	"seu": true, "sua": true, "seus": true, "suas": true, "este": true,
	"esta": true, "esse": true, "essa": true, "eles": true, "elas": true,
	"nos": true, "nas": true, "dos": true, "das": true, "pelo": true,
	"pela": true, "pelos": true, "pelas": true, "mas": true, "nem": true,
	"quando": true, "onde": true, "porque": true, "mesmo": true,
	"tambem": true, "ainda": true, "bem": true, "pode": true, "podem": true,
}

type keywordCount struct {
	Word  string `json:"word"`
	Count int    `json:"count"`
}

func topKeywords(texts []string, limit int) ([]keywordCount, int) {
	freq := make(map[string]int, 256)
	for _, text := range texts {
		word := []byte{}
		for i := 0; i < len(text); i++ {
			c := text[i]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
				if c >= 'A' && c <= 'Z' {
					c += 32 // to lower
				}
				word = append(word, c)
			} else {
				if len(word) >= 4 {
					w := string(word)
					if !stopWords[w] {
						freq[w]++
					}
				}
				word = word[:0]
			}
		}
		if len(word) >= 4 {
			w := string(word)
			if !stopWords[w] {
				freq[w]++
			}
		}
	}

	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(freq))
	for k, v := range freq {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})

	if limit > len(pairs) {
		limit = len(pairs)
	}
	result := make([]keywordCount, limit)
	for i := 0; i < limit; i++ {
		result[i] = keywordCount{Word: pairs[i].k, Count: pairs[i].v}
	}
	return result, len(texts)
}

type reindexState struct {
	mu      sync.Mutex
	running bool
	done    int
	total   int
}

// newRouter builds the HTTP router. extraOrigins are additional CORS-allowed origins
// beyond chrome-extension:// (e.g. a local dashboard, configured via VBM_CORS_ORIGIN).
// llmClient may be nil; when nil, mode=llm_summary returns 503.
func newRouter(s *store.Store, q *queue.Queue, ver string, extraOrigins []string, llmClient *llm.Client, authToken string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// P2-08: metrics counters — no external dependency, plain Prometheus text format.
	m := &serverMetrics{}

	// Healthz — no auth, but checks DB connectivity (P1-04).
	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := s.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"ok":false,"error":"database unavailable"}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})

	// P2-08: /metrics in Prometheus text format — no auth required for scraping.
	r.Get("/metrics", m.handler(s))

	r.Group(func(r chi.Router) {
		r.Use(corsMiddleware(extraOrigins))
		r.Use(authMiddleware(authToken, extraOrigins))

		// POST /visit — passive history record + lightweight meta indexing.
		r.Post("/visit", func(w http.ResponseWriter, req *http.Request) {
			var vr struct {
				URL     string          `json:"url"`
				Title   string          `json:"title"`
				VisitTs int64           `json:"visitTs"`
				DwellMs int64           `json:"dwellMs"`
				Domain  string          `json:"domain"`
				Meta    json.RawMessage `json:"meta"`
			}
			if !decodeJSONBody(w, req, &vr) {
				return
			}
			metaJSON := ""
			if len(vr.Meta) > 0 && string(vr.Meta) != "null" {
				metaJSON = string(vr.Meta)
			}
			if err := s.RecordVisit(store.VisitRequest{
				URL:      vr.URL,
				Title:    vr.Title,
				VisitTs:  vr.VisitTs,
				DwellMs:  vr.DwellMs,
				Domain:   vr.Domain,
				MetaJSON: metaJSON,
			}); err != nil {
				slog.Warn("record visit error", "url", vr.URL, "err", err)
				http.Error(w, `{"error":"visit failed"}`, http.StatusInternalServerError)
				return
			}

			// Build lightweight index text from title + meta tags so they are
			// searchable via FTS5/vector without requiring a manual force-index.
			var meta struct {
				Description   string `json:"description"`
				Keywords      string `json:"keywords"`
				OgDescription string `json:"ogDescription"`
				Author        string `json:"author"`
			}
			if metaJSON != "" {
				_ = json.Unmarshal([]byte(metaJSON), &meta)
			}
			var parts []string
			if vr.Title != "" {
				parts = append(parts, vr.Title)
			}
			for _, v := range []string{meta.Description, meta.Keywords, meta.OgDescription, meta.Author} {
				if v != "" {
					parts = append(parts, v)
				}
			}
			if len(parts) > 0 {
				metaText := strings.Join(parts, "\n")
				ir := store.IngestRequest{
					URL:     vr.URL,
					Title:   vr.Title,
					Text:    metaText,
					VisitTs: vr.VisitTs,
					DwellMs: vr.DwellMs,
					Domain:  vr.Domain,
					Source:  "history",
				}
				q.Enqueue(ir)
			}

			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))
		})

		// POST /ingest — manual full-index with text extraction + embedding.
		// Mode dispatch: full_text (default) | llm_summary | manual | meta_only.
		// llm_summary blocks the request while the LLM is called so the user
		// sees errors immediately. Other modes just enqueue.
		r.Post("/ingest", func(w http.ResponseWriter, req *http.Request) {
			var ir ingestRequest
			if !decodeJSONBody(w, req, &ir) {
				return
			}
			text := ir.Text
			switch ir.Mode {
			case "", "full_text", "manual", "meta_only":
				// Use provided text as-is. Popup is responsible for shaping
				// "manual" (typed) and "meta_only" (title + meta) before send.
			case "llm_summary":
				if llmClient == nil {
					http.Error(w, `{"error":"LLM not configured (set VBM_EMBED_API_KEY and VBM_EMBED_URL)"}`, http.StatusServiceUnavailable)
					return
				}
				summary, err := llmClient.Summarize(req.Context(), text)
				if err != nil {
					slog.Warn("llm summarize failed", "url", ir.URL, "err", err)
					http.Error(w, `{"error":"LLM call failed"}`, http.StatusBadGateway)
					return
				}
				text = summary
			default:
				http.Error(w, `{"error":"unknown mode"}`, http.StatusBadRequest)
				return
			}

			ireq := store.IngestRequest{
				URL:     ir.URL,
				Title:   ir.Title,
				Text:    text,
				VisitTs: ir.VisitTs,
				DwellMs: ir.DwellMs,
				Domain:  ir.Domain,
				Tags:    ir.Tags,
				SetTags: ir.SetTags,
				Source:  "indexed",
			}
			q.Enqueue(ireq)
			// P2-02: persist to queue table so pending count is accurate and processed items get cleaned up.
			if err := s.AddQueueItem(ireq); err != nil {
				slog.Warn("queue persist error", "url", ir.URL, "err", err)
			}
			m.ingestTotal.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"queued":true}`))
		})

		r.Get("/search", func(w http.ResponseWriter, req *http.Request) {
			q := req.URL.Query()
			query := q.Get("q")
			if query == "" {
				http.Error(w, `{"error":"q required"}`, http.StatusBadRequest)
				return
			}
			limit := 20
			if v := q.Get("limit"); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					limit = n
				}
			}
			if limit < 1 {
				limit = 1
			}
			if limit > 1000 {
				limit = 1000
			}
			minConf := 0.0
			if v := q.Get("min_confidence"); v != "" {
				if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
					minConf = f
				}
			}
			source := q.Get("source")
			results, err := s.Search(query, store.SearchOpts{
				Limit:         limit,
				Tags:          q["tag"],
				NegTags:       q["neg_tag"],
				MinConfidence: minConf,
				Source:        source,
			})
			if err != nil {
				http.Error(w, `{"error":"search failed"}`, http.StatusInternalServerError)
				return
			}
			m.searchTotal.Add(1)
			type searchResultJSON struct {
				URL      string   `json:"url"`
				Title    string   `json:"title"`
				Snippet  string   `json:"snippet"`
				Snippets []string `json:"snippets"`
				VisitTs  int64    `json:"visitTs"`
				Score    float64  `json:"score"`
				Domain   string   `json:"domain"`
				Tags     []string `json:"tags"`
				Source   string   `json:"source"`
			}
			type searchResponse struct {
				Results []searchResultJSON `json:"results"`
				Total   int                `json:"total"`
			}
			// Hard absolute sanity floor. The relative floor is now driven by
			// SearchOpts.MinConfidence (set by the UI sidebar slider).
			const absFloor = 0.005
			resp := searchResponse{Results: make([]searchResultJSON, 0, len(results))}
			for i, res := range results {
				if i > 0 && res.Score < absFloor {
					continue
				}
				snippets := res.Snippets
				if snippets == nil {
					snippets = []string{}
				}
				tags := res.Tags
				if tags == nil {
					tags = []string{}
				}
				src := res.Source
				if src == "" {
					src = "history"
				}
				resp.Results = append(resp.Results, searchResultJSON{
					URL:      res.URL,
					Title:    res.Title,
					Snippet:  res.Snippet,
					Snippets: snippets,
					VisitTs:  res.VisitTs,
					Score:    res.Score,
					Domain:   res.Domain,
					Tags:     tags,
					Source:   src,
				})
			}
			resp.Total = len(resp.Results)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})

		r.Delete("/forget", func(w http.ResponseWriter, req *http.Request) {
			var fr forgetRequest
			if !decodeJSONBody(w, req, &fr) {
				return
			}
			if err := s.Forget(store.ForgetRequest{Type: fr.Type, Value: fr.Value}); err != nil {
				http.Error(w, `{"error":"forget failed"}`, http.StatusInternalServerError)
				return
			}
			m.forgetTotal.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"forgotten":true}`))
		})

		r.Get("/status", func(w http.ResponseWriter, req *http.Request) {
			visited, indexed, pending, err := s.GetStatus()
			if err != nil {
				http.Error(w, `{"error":"status failed"}`, http.StatusInternalServerError)
				return
			}
			type statusResponse struct {
				Visited         int    `json:"visited"`
				Indexed         int    `json:"indexed"`
				Pending         int    `json:"pending"`
				Version         string `json:"version"`
				EmbedderVersion string `json:"embedder_version"`
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(statusResponse{
				Visited:         visited,
				Indexed:         indexed,
				Pending:         pending,
				Version:         ver,
				EmbedderVersion: s.EmbedderVersion(),
			})
		})

		r.Get("/page", func(w http.ResponseWriter, req *http.Request) {
			rawURL := req.URL.Query().Get("url")
			w.Header().Set("Content-Type", "application/json")
			if rawURL == "" {
				http.Error(w, `{"error":"url param required"}`, http.StatusBadRequest)
				return
			}
			exists, indexed, err := s.PageStatus(rawURL)
			if err != nil {
				http.Error(w, `{"error":"lookup failed"}`, http.StatusInternalServerError)
				return
			}
			tags := []string{}
			if exists {
				tags, _ = s.GetPageTags(rawURL)
				if tags == nil {
					tags = []string{}
				}
			}
			json.NewEncoder(w).Encode(struct {
				Exists  bool     `json:"exists"`
				Indexed bool     `json:"indexed"`
				Tags    []string `json:"tags"`
			}{Exists: exists, Indexed: indexed, Tags: tags})
		})

		// PUT /page/tags — replace the tag set of an indexed page (set-mode).
		// Body: {url, tags}. Returns the final normalized tag list. 404 if the
		// page is not in the index.
		r.Put("/page/tags", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				URL  string   `json:"url"`
				Tags []string `json:"tags"`
			}
			if !decodeJSONBody(w, req, &body) {
				return
			}
			if strings.TrimSpace(body.URL) == "" {
				http.Error(w, `{"error":"url required"}`, http.StatusBadRequest)
				return
			}
			tags, err := s.UpdatePageTags(body.URL, body.Tags)
			if err != nil {
				if err == sql.ErrNoRows {
					http.Error(w, `{"error":"page not found"}`, http.StatusNotFound)
					return
				}
				slog.Warn("update page tags failed", "url", body.URL, "err", err)
				http.Error(w, `{"error":"update failed"}`, http.StatusInternalServerError)
				return
			}
			if tags == nil {
				tags = []string{}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(struct {
				Tags []string `json:"tags"`
			}{Tags: tags})
		})

		// CR-010: user-managed domain blacklist endpoints.
		r.Get("/blacklist", func(w http.ResponseWriter, req *http.Request) {
			patterns, err := s.GetBlacklist()
			if err != nil {
				http.Error(w, `{"error":"get blacklist failed"}`, http.StatusInternalServerError)
				return
			}
			if patterns == nil {
				patterns = []string{}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(struct {
				Patterns []string `json:"patterns"`
			}{Patterns: patterns})
		})

		r.Post("/blacklist", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				Pattern string `json:"pattern"`
			}
			if !decodeJSONBody(w, req, &body) {
				return
			}
			if body.Pattern == "" {
				http.Error(w, `{"error":"pattern required"}`, http.StatusBadRequest)
				return
			}
			if err := s.AddToBlacklist(body.Pattern); err != nil {
				http.Error(w, `{"error":"add failed"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))
		})

		r.Delete("/blacklist", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				Pattern string `json:"pattern"`
			}
			if !decodeJSONBody(w, req, &body) {
				return
			}
			if body.Pattern == "" {
				http.Error(w, `{"error":"pattern required"}`, http.StatusBadRequest)
				return
			}
			if err := s.RemoveFromBlacklist(body.Pattern); err != nil {
				http.Error(w, `{"error":"remove failed"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))
		})

		// P1-15: export endpoint for LGPD portability.
		r.Get("/export", func(w http.ResponseWriter, req *http.Request) {
			pages, err := s.Export()
			if err != nil {
				http.Error(w, `{"error":"export failed"}`, http.StatusInternalServerError)
				return
			}
			type exportResponse struct {
				Pages []store.ExportPage `json:"pages"`
				Total int                `json:"total"`
			}
			if pages == nil {
				pages = []store.ExportPage{}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(exportResponse{Pages: pages, Total: len(pages)})
		})

		// P1-10: WebSocket with ping/pong and write deadlines.
		r.Get("/ws", func(w http.ResponseWriter, req *http.Request) {
			conn, err := upgrader.Upgrade(w, req, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			m.wsActive.Add(1)
			defer m.wsActive.Add(-1)

			const readDeadline = 60 * time.Second
			const writeDeadline = 10 * time.Second
			const pingInterval = 30 * time.Second

			conn.SetReadDeadline(time.Now().Add(readDeadline))
			conn.SetPongHandler(func(string) error {
				conn.SetReadDeadline(time.Now().Add(readDeadline))
				return nil
			})

			// Read goroutine required by gorilla/websocket to process control frames.
			readDone := make(chan struct{})
			go func() {
				defer close(readDone)
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}()

			statusTicker := time.NewTicker(5 * time.Second)
			pingTicker := time.NewTicker(pingInterval)
			defer statusTicker.Stop()
			defer pingTicker.Stop()

			for {
				select {
				case <-readDone:
					return
				case <-statusTicker.C:
					_, indexed, pending, err := s.GetStatus()
					if err != nil {
						return
					}
					type wsStatus struct {
						Type    string `json:"type"`
						Indexed int    `json:"indexed"`
						Pending int    `json:"pending"`
					}
					conn.SetWriteDeadline(time.Now().Add(writeDeadline))
					msg := wsStatus{Type: "status", Indexed: indexed, Pending: pending}
					if err := conn.WriteJSON(msg); err != nil {
						return
					}
				case <-pingTicker.C:
					conn.SetWriteDeadline(time.Now().Add(writeDeadline))
					if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
						return
					}
				}
			}
		})

		// /topics — top keywords for a time window (interest evolution timeline).
		r.Get("/topics", func(w http.ResponseWriter, req *http.Request) {
			fromStr := req.URL.Query().Get("from")
			toStr := req.URL.Query().Get("to")
			limitStr := req.URL.Query().Get("limit")
			if fromStr == "" || toStr == "" {
				http.Error(w, `{"error":"from and to required (unix ms)"}`, http.StatusBadRequest)
				return
			}
			fromMs, err1 := strconv.ParseInt(fromStr, 10, 64)
			toMs, err2 := strconv.ParseInt(toStr, 10, 64)
			if err1 != nil || err2 != nil {
				http.Error(w, `{"error":"invalid from/to"}`, http.StatusBadRequest)
				return
			}
			limit := 20
			if limitStr != "" {
				if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
					limit = n
				}
			}
			if limit > 50 {
				limit = 50
			}
			texts, err := s.GetChunkTextByPeriod(fromMs, toMs)
			if err != nil {
				http.Error(w, `{"error":"topics query failed"}`, http.StatusInternalServerError)
				return
			}
			keywords, totalChunks := topKeywords(texts, limit)
			type topicsResponse struct {
				Keywords    []keywordCount `json:"keywords"`
				TotalChunks int            `json:"total_chunks"`
				From        int64          `json:"from"`
				To          int64          `json:"to"`
			}
			if keywords == nil {
				keywords = []keywordCount{}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(topicsResponse{
				Keywords:    keywords,
				TotalChunks: totalChunks,
				From:        fromMs,
				To:          toMs,
			})
		})

		// /history — chronological browsing history with per-page keywords and daily activity.
		r.Get("/history", func(w http.ResponseWriter, req *http.Request) {
			fromStr := req.URL.Query().Get("from")
			toStr := req.URL.Query().Get("to")
			limitStr := req.URL.Query().Get("limit")
			if fromStr == "" || toStr == "" {
				http.Error(w, `{"error":"from and to required (unix ms)"}`, http.StatusBadRequest)
				return
			}
			fromMs, err1 := strconv.ParseInt(fromStr, 10, 64)
			toMs, err2 := strconv.ParseInt(toStr, 10, 64)
			if err1 != nil || err2 != nil {
				http.Error(w, `{"error":"invalid from/to"}`, http.StatusBadRequest)
				return
			}
			limit := 100
			if limitStr != "" {
				if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
					limit = n
				}
			}
			if limit > 500 {
				limit = 500
			}
			histRows, err := s.GetPageHistoryRows(fromMs, toMs, limit)
			if err != nil {
				http.Error(w, `{"error":"history query failed"}`, http.StatusInternalServerError)
				return
			}

			// Group rows by pageID preserving DESC order, collect chunk texts per page.
			type pageEntry struct {
				pageID  int64
				url     string
				title   string
				domain  string
				visitTs int64
				texts   []string
			}
			pageMap := make(map[int64]*pageEntry)
			pageOrder := make([]int64, 0)
			for _, row := range histRows {
				if _, ok := pageMap[row.PageID]; !ok {
					pageMap[row.PageID] = &pageEntry{
						pageID:  row.PageID,
						url:     row.URL,
						title:   row.Title,
						domain:  row.Domain,
						visitTs: row.VisitTs,
					}
					pageOrder = append(pageOrder, row.PageID)
				}
				if row.Text != "" {
					pageMap[row.PageID].texts = append(pageMap[row.PageID].texts, row.Text)
				}
			}

			// Daily counts come from a SQL aggregate over the full [from, to)
			// range — independent of `limit`. The page list below is paginated;
			// without this, days outside the truncated window would show 0.
			daily, err := s.GetDailyPageCounts(fromMs, toMs)
			if err != nil {
				http.Error(w, `{"error":"history query failed"}`, http.StatusInternalServerError)
				return
			}

			type histPageJSON struct {
				URL      string   `json:"url"`
				Title    string   `json:"title"`
				Domain   string   `json:"domain"`
				VisitTs  int64    `json:"visitTs"`
				Keywords []string `json:"keywords"`
			}
			pages := make([]histPageJSON, 0, len(pageOrder))
			for _, pid := range pageOrder {
				pe := pageMap[pid]
				kws, _ := topKeywords(pe.texts, 5)
				kwStrs := make([]string, len(kws))
				for i, k := range kws {
					kwStrs[i] = k.Word
				}
				pages = append(pages, histPageJSON{
					URL:      pe.url,
					Title:    pe.title,
					Domain:   pe.domain,
					VisitTs:  pe.visitTs,
					Keywords: kwStrs,
				})
			}

			type histResponse struct {
				Pages []histPageJSON  `json:"pages"`
				Total int             `json:"total"`
				Daily map[string]int  `json:"daily"`
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(histResponse{
				Pages: pages,
				Total: len(pages),
				Daily: daily,
			})
		})

		// /admin/reindex — re-embeds stub-v0 chunks with the active embedder.
		var ri reindexState
		r.Post("/admin/reindex", func(w http.ResponseWriter, req *http.Request) {
			ri.mu.Lock()
			if ri.running {
				ri.mu.Unlock()
				http.Error(w, `{"error":"reindex already running"}`, http.StatusConflict)
				return
			}
			ri.running = true
			ri.done = 0
			ri.total = 0
			ri.mu.Unlock()

			go func() {
				n, err := s.ReindexChunks(func(done, total int) {
					ri.mu.Lock()
					ri.done = done
					ri.total = total
					ri.mu.Unlock()
				})
				ri.mu.Lock()
				ri.running = false
				ri.done = n
				ri.mu.Unlock()
				if err != nil {
					slog.Error("reindex failed", "err", err)
				} else {
					slog.Info("reindex complete", "reindexed", n)
				}
			}()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"started":true}`))
		})

		r.Get("/admin/reindex/status", func(w http.ResponseWriter, req *http.Request) {
			ri.mu.Lock()
			running, done, total := ri.running, ri.done, ri.total
			ri.mu.Unlock()
			type statusResp struct {
				Running bool `json:"running"`
				Done    int  `json:"done"`
				Total   int  `json:"total"`
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(statusResp{Running: running, Done: done, Total: total})
		})

		// POST /tags/suggest — runs the LLM over the supplied page context and
		// returns up to VBM_LLM_SUGGEST_TAGS_MAX tags (default 3, clamped 1-25).
		// Reuses the same provider as the embedder/llm summary path. Returns
		// 503 when no LLM is configured.
		r.Post("/tags/suggest", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				URL   string `json:"url"`
				Title string `json:"title"`
				Text  string `json:"text"`
			}
			if !decodeJSONBody(w, req, &body) {
				return
			}
			if llmClient == nil {
				http.Error(w, `{"error":"LLM not configured"}`, http.StatusServiceUnavailable)
				return
			}
			if len(strings.TrimSpace(body.Text)) < 100 {
				http.Error(w, `{"error":"not enough content"}`, http.StatusBadRequest)
				return
			}

			existing, _ := s.ListTags()
			existingNames := make([]string, 0, len(existing))
			for _, t := range existing {
				existingNames = append(existingNames, t.Tag)
			}

			raw, err := llmClient.SuggestTags(req.Context(), body.Title, body.Text, existingNames)
			if err != nil {
				slog.Warn("llm suggest tags failed", "url", body.URL, "err", err)
				http.Error(w, `{"error":"LLM call failed"}`, http.StatusBadGateway)
				return
			}

			// Normalize via the same rules used at ingest, drop empties + dedup
			// preserving order. Cap at the configured SuggestTagsMax so the
			// HTTP layer respects VBM_LLM_SUGGEST_TAGS_MAX (was hardcoded 3).
			tagCap := llmClient.SuggestTagsMax()
			seen := make(map[string]struct{}, len(raw))
			out := make([]string, 0, tagCap)
			for _, t := range raw {
				nt := store.NormalizeTag(t)
				if nt == "" {
					continue
				}
				if _, ok := seen[nt]; ok {
					continue
				}
				seen[nt] = struct{}{}
				out = append(out, nt)
				if len(out) >= tagCap {
					break
				}
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(struct {
				Tags []string `json:"tags"`
			}{Tags: out})
		})

		// /tags — list all tags with page counts (feeds popup autocomplete + UI tab).
		r.Get("/tags", func(w http.ResponseWriter, req *http.Request) {
			tags, err := s.ListTags()
			if err != nil {
				http.Error(w, `{"error":"list tags failed"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(struct {
				Tags []store.TagCount `json:"tags"`
			}{Tags: tags})
		})

		// /pages?tag=xyz&limit=N — list pages carrying the given tag, newest first.
		r.Get("/pages", func(w http.ResponseWriter, req *http.Request) {
			tag := req.URL.Query().Get("tag")
			if tag == "" {
				http.Error(w, `{"error":"tag required"}`, http.StatusBadRequest)
				return
			}
			limit := 100
			if ls := req.URL.Query().Get("limit"); ls != "" {
				if n, err := strconv.Atoi(ls); err == nil && n > 0 {
					limit = n
				}
			}
			pages, err := s.ListPagesByTag(tag, limit)
			if err != nil {
				http.Error(w, `{"error":"list pages failed"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(struct {
				Pages []store.TaggedPage `json:"pages"`
				Total int                `json:"total"`
			}{Pages: pages, Total: len(pages)})
		})

		r.Get("/ui", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(uiHTML))
		})
		r.Get("/ui/*", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(uiHTML))
		})
	})

	return r
}

// authMiddleware enforces a Bearer token when one is configured. When token is
// empty the middleware is a no-op (open access — preserves local-only setups).
// HTTP clients pass the token via Authorization: Bearer <token>; browser
// WebSocket clients can't set custom headers on handshake, so ?token=<token>
// query string is also accepted.
func authMiddleware(token string, extraOrigins []string) func(http.Handler) http.Handler {
	if token == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	allowed := make(map[string]bool, len(extraOrigins))
	for _, o := range extraOrigins {
		if o != "" {
			allowed[o] = true
		}
	}
	expected := "Bearer " + token
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin != "" && !strings.HasPrefix(origin, "chrome-extension://") && !allowed[origin] {
				// Same-origin requests (browsers send Origin on non-GET even
				// when same-origin) must not be blocked — the UI served by this
				// daemon issues DELETE/PUT to itself for edit/forget.
				if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
					http.Error(w, `{"error":"forbidden origin"}`, http.StatusUnauthorized)
					return
				}
			}

			if r.Header.Get("Authorization") == expected {
				next.ServeHTTP(w, r)
				return
			}
			if r.URL.Query().Get("token") == token {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		})
	}
}

// corsMiddleware sets CORS headers for chrome-extension:// origins and any extraOrigins.
// P1-07: extraOrigins allows external dashboards (e.g. VBM_CORS_ORIGIN=http://localhost:3000).
func corsMiddleware(extraOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(extraOrigins))
	for _, o := range extraOrigins {
		if o != "" {
			allowed[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if strings.HasPrefix(origin, "chrome-extension://") || allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
