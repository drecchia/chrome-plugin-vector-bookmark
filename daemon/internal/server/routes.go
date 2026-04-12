package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	"github.com/vbm/daemon/internal/queue"
	"github.com/vbm/daemon/internal/store"
)

type ingestRequest struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	VisitTs  int64  `json:"visitTs"`
	DwellMs  int64  `json:"dwellMs"`
	Domain   string `json:"domain"`
	StarRank bool   `json:"starRank"`
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
.wrap{max-width:680px;margin:0 auto;padding:32px 20px}
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
/* blocklist */
.bl-row{display:flex;gap:8px;margin-bottom:4px}
.bl-entry{display:flex;align-items:center;justify-content:space-between;font-size:13px;color:#374151;background:#f9fafb;border:1px solid #e5e7eb;border-radius:5px;padding:6px 10px}
.bl-remove{background:none;border:none;cursor:pointer;font-size:16px;color:#9ca3af;line-height:1;padding:0 0 0 10px}
.bl-remove:hover{color:#ef4444}
.kw-count{font-size:11px;color:#9ca3af;width:32px;text-align:right;flex-shrink:0}
.tl-meta{font-size:11px;color:#9ca3af;margin-top:12px}
/* history timeline */
.hist-chart{margin-bottom:18px;overflow:hidden;border-radius:4px}
.hist-day-label{display:flex;justify-content:space-between;font-size:10px;color:#9ca3af;margin-top:4px;padding:0 1px}
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
    <button class="tab" data-panel="hotwords-panel">Hot Words</button>
    <button class="tab" data-panel="hist-panel">Timeline</button>
    <button class="tab" data-panel="blocklist-panel">Blacklist</button>
  </div>

  <div id="search-panel" class="panel active">
    <div class="search-row">
      <input id="q" type="search" placeholder="Search your reading history..." autofocus>
      <button id="search-btn">Search</button>
    </div>
    <div id="results"></div>
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

  <div id="blocklist-panel" class="panel">
    <div class="bl-row">
      <input id="bl-input" type="text" placeholder="example.com or /regex/" style="flex:1;padding:8px 12px;border:1px solid #d1d5db;border-radius:6px;font-size:13px;outline:none;background:#fff">
      <button id="bl-add" style="padding:8px 16px;border:none;border-radius:6px;font-size:13px;font-weight:500;cursor:pointer;background:#111;color:#fff;white-space:nowrap">Block</button>
    </div>
    <div id="bl-list" style="margin-top:14px;display:flex;flex-direction:column;gap:4px"></div>
    <div id="bl-empty" class="empty" style="display:none">No blacklisted domains</div>
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
var q=document.getElementById('q'),btn=document.getElementById('search-btn'),res=document.getElementById('results'),stat=document.getElementById('stat')
fetch('/status').then(function(r){return r.json()}).then(function(d){stat.textContent=d.indexed+' pages indexed'}).catch(function(){})
function esc(s){return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;')}
function fmt(ts){var d=new Date(ts),diff=Date.now()-ts;if(diff<86400000)return d.toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'});if(diff<604800000)return d.toLocaleDateString([],{weekday:'short'});return d.toLocaleDateString([],{month:'short',day:'numeric'})}
function search(){
  var v=q.value.trim();if(!v)return
  btn.disabled=true;btn.textContent='...'
  fetch('/search?q='+encodeURIComponent(v)+'&limit=10').then(function(r){return r.json()}).then(function(data){
    var list=data.results||[]
    if(!list.length){res.innerHTML='<div class="empty">No results</div>';return}
    res.innerHTML='<div class="count">'+list.length+' result'+(list.length>1?'s':'')+'</div>'+list.map(function(r){
      return '<div class="result">'+
        '<button class="forget-btn" data-url="'+esc(r.url)+'">forget</button>'+
        '<div class="result-meta"><span class="domain">'+esc(r.domain)+'</span><span class="date">'+fmt(r.visitTs)+'</span></div>'+
        '<a class="result-title" href="'+esc(r.url)+'" target="_blank">'+esc(r.title||r.url)+'</a>'+
        '<div class="snippet">'+esc(r.snippet)+'</div>'+
      '</div>'
    }).join('')
  }).catch(function(){res.innerHTML='<div class="empty">Search failed</div>'})
  .finally(function(){btn.disabled=false;btn.textContent='Search'})
}
btn.addEventListener('click',search)
q.addEventListener('keydown',function(e){if(e.key==='Enter')search()})
res.addEventListener('click',function(e){
  var b=e.target.closest('.forget-btn');if(!b)return
  b.textContent='...'
  fetch('/forget',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({type:'url',value:b.dataset.url})})
    .then(function(){b.closest('.result').remove()}).catch(function(){b.textContent='err'})
})

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
function tlRender(){
  var p=tlPeriod()
  document.getElementById('tl-label').textContent=tlLabel()
  document.getElementById('tl-results').innerHTML='<div class="empty" style="padding:20px 0">Loading…</div>'
  fetch('/topics?from='+p.from+'&to='+p.to+'&limit=20')
    .then(function(r){return r.json()})
    .then(function(d){
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
          return '<div class="kw-row">'+
            '<span class="kw-rank">'+(i+1)+'</span>'+
            '<span class="kw-word">'+esc(k.word)+'</span>'+
            '<div class="kw-bar-wrap"><div class="kw-bar" style="width:'+pct+'%"></div></div>'+
            '<span class="kw-count">'+k.count+'</span>'+
          '</div>'
        }).join('')+
        '</div>'+
        '<div class="tl-meta">'+kws.length+' terms &middot; '+d.total_chunks+' chunks analyzed</div>'
    })
    .catch(function(){document.getElementById('tl-results').innerHTML='<div class="empty">Failed to load</div>'})
}
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
function htBuildChart(daily,fromMs,toMs){
  var days=[],d=new Date(fromMs)
  while(d.getTime()<toMs){days.push(new Date(d));d.setDate(d.getDate()+1)}
  if(htMode==='month'&&days.length>31)days=days.slice(0,31)
  var maxVal=0
  days.forEach(function(day){
    var k=day.toISOString().slice(0,10)
    if((daily[k]||0)>maxVal)maxVal=daily[k]||0
  })
  if(maxVal===0)return''
  var W=640,H=56,n=days.length,bw=Math.floor((W-n)/(n||1)),gap=1
  var bars=days.map(function(day,i){
    var k=day.toISOString().slice(0,10),v=daily[k]||0
    var h=maxVal>0?Math.max(2,Math.round(v/maxVal*(H-4))):0
    var x=i*(bw+gap),y=H-h
    return'<rect x="'+x+'" y="'+y+'" width="'+bw+'" height="'+h+'" rx="1" fill="'+(v>0?'#6366f1':'#e5e7eb')+'"><title>'+k+': '+v+' page'+(v!==1?'s':'')+'</title></rect>'
  }).join('')
  var firstLabel=days[0].toLocaleDateString([],{month:'short',day:'numeric'})
  var lastLabel=days[days.length-1].toLocaleDateString([],{month:'short',day:'numeric'})
  return'<div class="hist-chart"><svg viewBox="0 0 '+W+' '+H+'" width="100%" height="'+H+'" xmlns="http://www.w3.org/2000/svg">'+bars+'</svg>'+
    '<div class="hist-day-label"><span>'+esc(firstLabel)+'</span><span>'+esc(lastLabel)+'</span></div></div>'
}
function htRender(){
  var p=htPeriod()
  document.getElementById('ht-label').textContent=htLabel()
  document.getElementById('ht-results').innerHTML='<div class="empty" style="padding:20px 0">Loading…</div>'
  document.getElementById('ht-chart').innerHTML=''
  fetch('/history?from='+p.from+'&to='+p.to+'&limit=100')
    .then(function(r){return r.json()})
    .then(function(d){
      var pages=d.pages||[]
      document.getElementById('ht-chart').innerHTML=htBuildChart(d.daily||{},p.from,p.to)
      if(!pages.length){
        document.getElementById('ht-results').innerHTML='<div class="empty">No pages indexed in this period</div>'
        return
      }
      // Group by date string
      var groups={},order=[]
      pages.forEach(function(pg){
        var k=new Date(pg.visitTs).toLocaleDateString([],{weekday:'short',month:'short',day:'numeric'})
        if(!groups[k]){groups[k]=[];order.push(k)}
        groups[k].push(pg)
      })
      document.getElementById('ht-results').innerHTML=order.map(function(dateKey){
        return'<div class="hist-group">'+
          '<div class="hist-date">'+esc(dateKey)+'</div>'+
          groups[dateKey].map(function(pg){
            var star=pg.starRank?'&#11088; ':''
            var kws=(pg.keywords||[]).map(function(w){return'<span class="hist-kw">'+esc(w)+'</span>'}).join('')
            return'<div class="hist-page">'+
              '<div class="hist-page-meta"><span class="hist-domain">'+esc(pg.domain)+'</span><span style="font-size:11px;color:#9ca3af">'+new Date(pg.visitTs).toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'})+'</span></div>'+
              '<a class="hist-page-title" href="'+esc(pg.url)+'" target="_blank">'+star+esc(pg.title||pg.url)+'</a>'+
              (kws?'<div class="hist-kws">'+kws+'</div>':'')+
            '</div>'
          }).join('')+
        '</div>'
      }).join('')
    })
    .catch(function(){document.getElementById('ht-results').innerHTML='<div class="empty">Failed to load</div>'})
}
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
// Blocklist panel
var blInput=document.getElementById('bl-input'),blList=document.getElementById('bl-list'),blEmpty=document.getElementById('bl-empty')
function blRender(patterns){
  blList.innerHTML=''
  if(!patterns||!patterns.length){blEmpty.style.display='';return}
  blEmpty.style.display='none'
  patterns.forEach(function(p){
    var d=document.createElement('div');d.className='bl-entry'
    var t=document.createElement('span');t.textContent=p
    var b=document.createElement('button');b.className='bl-remove';b.textContent='×';b.title='Remove'
    b.addEventListener('click',function(){
      fetch('/blocklist',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({pattern:p})})
        .then(function(){return fetch('/blocklist')}).then(function(r){return r.json()}).then(function(d){blRender(d.patterns)})
    })
    d.appendChild(t);d.appendChild(b);blList.appendChild(d)
  })
}
function blLoad(){fetch('/blocklist').then(function(r){return r.json()}).then(function(d){blRender(d.patterns)}).catch(function(){})}
document.getElementById('bl-add').addEventListener('click',function(){
  var v=blInput.value.trim();if(!v)return
  fetch('/blocklist',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({pattern:v})})
    .then(function(){blInput.value='';return fetch('/blocklist')}).then(function(r){return r.json()}).then(function(d){blRender(d.patterns)})
})
blInput.addEventListener('keydown',function(e){if(e.key==='Enter')document.getElementById('bl-add').click()})
document.querySelectorAll('.tab').forEach(function(t){t.addEventListener('click',function(){
  if(t.dataset.panel==='blocklist-panel')blLoad()
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
func newRouter(s *store.Store, q *queue.Queue, ver string, extraOrigins []string) http.Handler {
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

		r.Post("/ingest", func(w http.ResponseWriter, req *http.Request) {
			var ir ingestRequest
			if err := json.NewDecoder(req.Body).Decode(&ir); err != nil {
				http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
				return
			}
			ireq := store.IngestRequest{
				URL:      ir.URL,
				Title:    ir.Title,
				Text:     ir.Text,
				VisitTs:  ir.VisitTs,
				DwellMs:  ir.DwellMs,
				Domain:   ir.Domain,
				StarRank: ir.StarRank,
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
			query := req.URL.Query().Get("q")
			if query == "" {
				http.Error(w, `{"error":"q required"}`, http.StatusBadRequest)
				return
			}
			limitStr := req.URL.Query().Get("limit")
			limit := 5
			if limitStr != "" {
				if n, err := strconv.Atoi(limitStr); err == nil {
					limit = n
				}
			}
			if limit > 20 {
				limit = 20
			}
			results, err := s.Search(query, limit)
			if err != nil {
				http.Error(w, `{"error":"search failed"}`, http.StatusInternalServerError)
				return
			}
			m.searchTotal.Add(1)
			type searchResultJSON struct {
				URL     string  `json:"url"`
				Title   string  `json:"title"`
				Snippet string  `json:"snippet"`
				VisitTs int64   `json:"visitTs"`
				Score   float64 `json:"score"`
				Domain  string  `json:"domain"`
			}
			type searchResponse struct {
				Results []searchResultJSON `json:"results"`
				Total   int                `json:"total"`
			}
			resp := searchResponse{Results: make([]searchResultJSON, 0, len(results)), Total: len(results)}
			for _, res := range results {
				resp.Results = append(resp.Results, searchResultJSON{
					URL:     res.URL,
					Title:   res.Title,
					Snippet: res.Snippet,
					VisitTs: res.VisitTs,
					Score:   res.Score,
					Domain:  res.Domain,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})

		r.Delete("/forget", func(w http.ResponseWriter, req *http.Request) {
			var fr forgetRequest
			if err := json.NewDecoder(req.Body).Decode(&fr); err != nil {
				http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
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
			indexed, pending, err := s.GetStatus()
			if err != nil {
				http.Error(w, `{"error":"status failed"}`, http.StatusInternalServerError)
				return
			}
			type statusResponse struct {
				Indexed        int    `json:"indexed"`
				Pending        int    `json:"pending"`
				Version        string `json:"version"`
				EmbedderVersion string `json:"embedder_version"`
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(statusResponse{
				Indexed:        indexed,
				Pending:        pending,
				Version:        ver,
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
			exists, err := s.PageExists(rawURL)
			if err != nil {
				http.Error(w, `{"error":"lookup failed"}`, http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(struct {
				Indexed bool `json:"indexed"`
			}{Indexed: exists})
		})

		// CR-010: user-managed domain blocklist endpoints.
		r.Get("/blocklist", func(w http.ResponseWriter, req *http.Request) {
			patterns, err := s.GetBlocklist()
			if err != nil {
				http.Error(w, `{"error":"get blocklist failed"}`, http.StatusInternalServerError)
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

		r.Post("/blocklist", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				Pattern string `json:"pattern"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Pattern == "" {
				http.Error(w, `{"error":"pattern required"}`, http.StatusBadRequest)
				return
			}
			if err := s.AddToBlocklist(body.Pattern); err != nil {
				http.Error(w, `{"error":"add failed"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))
		})

		r.Delete("/blocklist", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				Pattern string `json:"pattern"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Pattern == "" {
				http.Error(w, `{"error":"pattern required"}`, http.StatusBadRequest)
				return
			}
			if err := s.RemoveFromBlocklist(body.Pattern); err != nil {
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
					indexed, pending, err := s.GetStatus()
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
				pageID   int64
				url      string
				title    string
				domain   string
				visitTs  int64
				starRank int
				texts    []string
			}
			pageMap := make(map[int64]*pageEntry)
			pageOrder := make([]int64, 0)
			for _, row := range histRows {
				if _, ok := pageMap[row.PageID]; !ok {
					pageMap[row.PageID] = &pageEntry{
						pageID:   row.PageID,
						url:      row.URL,
						title:    row.Title,
						domain:   row.Domain,
						visitTs:  row.VisitTs,
						starRank: row.StarRank,
					}
					pageOrder = append(pageOrder, row.PageID)
				}
				if row.Text != "" {
					pageMap[row.PageID].texts = append(pageMap[row.PageID].texts, row.Text)
				}
			}

			// Build daily activity map and page list.
			daily := make(map[string]int)
			type histPageJSON struct {
				URL      string   `json:"url"`
				Title    string   `json:"title"`
				Domain   string   `json:"domain"`
				VisitTs  int64    `json:"visitTs"`
				StarRank int      `json:"starRank"`
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
				dateKey := time.UnixMilli(pe.visitTs).UTC().Format("2006-01-02")
				daily[dateKey]++
				pages = append(pages, histPageJSON{
					URL:      pe.url,
					Title:    pe.title,
					Domain:   pe.domain,
					VisitTs:  pe.visitTs,
					StarRank: pe.starRank,
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

func authMiddleware(token string, extraOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(extraOrigins))
	for _, o := range extraOrigins {
		if o != "" {
			allowed[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handle CORS preflight without auth check
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Check Origin header — allow chrome-extension:// and any extraOrigins (P0-NEW: fix ordering bug)
			origin := r.Header.Get("Origin")
			if origin != "" && !strings.HasPrefix(origin, "chrome-extension://") && !allowed[origin] {
				http.Error(w, `{"error":"forbidden origin"}`, http.StatusUnauthorized)
				return
			}

			// Check Authorization header
			authHeader := r.Header.Get("Authorization")
			expected := "Bearer " + token
			if authHeader != expected {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
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
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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
