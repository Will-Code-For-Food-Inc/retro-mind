package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const sharedCSS = `
  *, *::before, *::after { box-sizing: border-box; }
  body { font-family: monospace; background: #111; color: #ddd; margin: 0; }
  /* ── sticky header ── */
  #site-header {
    position: fixed; top: 0; left: 0; right: 0; height: 42px; z-index: 50;
    background: #161622; border-bottom: 1px solid #2a2a40;
    display: flex; align-items: center; padding: 0 1.2rem; gap: 1.4rem;
  }
  #site-header .logo { color: #adf; font-weight: bold; font-size: .9rem; text-decoration: none; letter-spacing: .03em; }
  #site-header nav { display: flex; gap: 1.2rem; }
  #site-header nav a { color: #778; font-size: .82rem; text-decoration: none; }
  #site-header nav a:hover { color: #adf; }
  #site-header nav a.active { color: #adf; }
`

const pageTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>rom-tagger</title>
<style>
` + sharedCSS + `
  .page { max-width: 1100px; margin: 0 auto; padding: 4rem 1rem 2rem; }
  h1 { color: #fff; font-size: 1.1rem; margin-bottom: 1.2rem; }
  .tag-cloud { display: flex; flex-wrap: wrap; gap: .4rem; margin-bottom: 2rem; }
  .tag { background: #222; border: 1px solid #444; border-radius: 3px; padding: .2rem .5rem; font-size: .85rem; }
  .tag a { color: #adf; text-decoration: none; }
  table { width: 100%; border-collapse: collapse; }
  th { text-align: left; border-bottom: 1px solid #444; padding: .3rem .5rem; color: #aaa; }
  td { padding: .3rem .5rem; border-bottom: 1px solid #222; vertical-align: top; }
  td.tags { font-size: .8rem; color: #999; }
  td.tags span { background: #1a1a2e; border: 1px solid #333; border-radius: 2px; padding: .1rem .3rem; margin-right: .2rem; display: inline-block; }
  input[type=search] { background: #222; border: 1px solid #555; color: #ddd; padding: .3rem .6rem; width: 300px; border-radius: 3px; }
  .count { color: #666; font-size: .85rem; }
  footer { margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #333; font-size: .8rem; }
  footer a { color: #555; }
</style>
</head>
<body>
<header id="site-header">
  <a class="logo" href="/">rom-tagger</a>
  <nav>
    <a href="/" class="active">games</a>
    <a href="/tags">tags</a>
    <a href="/viz">viz</a>
    <a href="/views">saved views</a>
  </nav>
</header>
<div class="page">

{{if .TagFilter}}<p>Filtered by tag: <strong>{{.TagFilter}}</strong> &mdash; <a href="/" style="color:#7af">clear</a></p>{{end}}

<form method="get" action="/">
  <input type="search" name="q" value="{{.Query}}" placeholder="search by name or tag…">
  &nbsp;<span class="count">{{len .Games}} game{{if ne (len .Games) 1}}s{{end}}</span>
</form>
<br>

<table>
  <tr><th>Name</th><th>Platform</th><th>Tags</th></tr>
  {{range .Games}}
  <tr>
    <td><a href="/game/{{.Name | urlquery}}" style="color:#ddd;text-decoration:none;">{{.Name}}</a></td>
    <td>{{.Platform}}</td>
    <td class="tags">{{range .Tags}}<span>{{.}}</span>{{end}}</td>
  </tr>
  {{end}}
</table>
<footer><a href="/about">about this library</a></footer>
</div>
</body>
</html>`

const tagsTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>tags — rom-tagger</title>
<style>
` + sharedCSS + `
  .page { max-width: 900px; margin: 0 auto; padding: 4rem 1rem 2rem; }
  h1 { color: #fff; font-size: 1.1rem; margin-bottom: 1.2rem; }
  table { width: 100%; border-collapse: collapse; }
  th { text-align: left; border-bottom: 1px solid #444; padding: .3rem .5rem; color: #aaa; }
  td { padding: .3rem .5rem; border-bottom: 1px solid #222; }
  td a { color: #adf; text-decoration: none; }
  td a:hover { text-decoration: underline; }
  footer { margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #333; font-size: .8rem; }
  footer a { color: #555; }
</style>
</head>
<body>
<header id="site-header">
  <a class="logo" href="/">rom-tagger</a>
  <nav>
    <a href="/">games</a>
    <a href="/tags" class="active">tags</a>
    <a href="/viz">viz</a>
    <a href="/views">saved views</a>
  </nav>
</header>
<div class="page">
<h1>tag cloud</h1>
<table>
  <tr><th>Tag</th><th>Count</th></tr>
  {{range .}}
  <tr>
    <td><a href="/?tag={{.Tag}}">{{.Tag}}</a></td>
    <td>{{.Count}}</td>
  </tr>
  {{end}}
</table>
<footer><a href="/about">about this library</a></footer>
</div>
</body>
</html>`

const gameTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{.Game.Name}} — rom-tagger</title>
<style>
` + sharedCSS + `
  .page { max-width: 900px; margin: 0 auto; padding: 4rem 1rem 2rem; }
  h1 { color: #fff; margin-bottom: .25rem; font-size: 1.3rem; }
  .meta { color: #888; font-size: .85rem; margin-bottom: 1.5rem; }
  .section { margin-bottom: 1.5rem; }
  .section h2 { color: #aaa; font-size: .9rem; text-transform: uppercase; letter-spacing: .05em; border-bottom: 1px solid #333; padding-bottom: .25rem; margin-bottom: .75rem; }
  .tag-cloud { display: flex; flex-wrap: wrap; gap: .4rem; }
  .tag { background: #222; border: 1px solid #444; border-radius: 3px; padding: .2rem .5rem; font-size: .85rem; }
  .tag a { color: #adf; text-decoration: none; }
  .rawg-tag { background: #1a1a2e; border: 1px solid #333; }
  .rawg-tag span { color: #99a; }
  .description { line-height: 1.6; color: #ccc; white-space: pre-wrap; }
  .not-found { color: #666; font-style: italic; }
  .scores { display: flex; gap: 2rem; margin-bottom: .5rem; }
  .score-box { background: #1a1a2e; border: 1px solid #333; padding: .4rem .8rem; border-radius: 3px; }
  .score-box .label { font-size: .75rem; color: #666; }
  .score-box .value { font-size: 1.2rem; color: #adf; }
  .hero { width: 100%; max-height: 300px; object-fit: cover; border-radius: 4px; margin-bottom: 1.5rem; }
  footer { margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #333; font-size: .8rem; }
  footer a { color: #555; }
</style>
</head>
<body>
<header id="site-header">
  <a class="logo" href="/">rom-tagger</a>
  <nav>
    <a href="/">games</a>
    <a href="/tags">tags</a>
    <a href="/viz">viz</a>
    <a href="/views">saved views</a>
  </nav>
</header>
<div class="page">
{{if and .ShowArt .Meta .Meta.BackgroundImage}}<img class="hero" src="{{.Meta.BackgroundImage}}" alt="{{.Game.Name}}">{{end}}
<h1>{{.Game.Name}}</h1>
<p class="meta">{{.Game.Platform}}{{if .Meta.Released}} &mdash; released {{.Meta.Released}}{{end}}</p>

<div class="section">
  <h2>Vibe Tags</h2>
  {{if .Game.Tags}}
  <div class="tag-cloud">
    {{range .Game.Tags}}<span class="tag"><a href="/?tag={{.}}">{{.}}</a></span>{{end}}
  </div>
  {{else}}<p class="not-found">No vibe tags yet.</p>{{end}}
</div>

{{if .Meta}}
  {{if .Meta.NotFound}}
    <p class="not-found">No RAWG metadata found for this game.</p>
  {{else}}
    {{if .Meta.Metacritic}}
    <div class="section">
      <h2>Scores</h2>
      <div class="scores">
        <div class="score-box"><div class="label">Metacritic</div><div class="value">{{.Meta.Metacritic}}</div></div>
      </div>
    </div>
    {{end}}
    {{if .Meta.Description}}
    <div class="section">
      <h2>Description</h2>
      <p class="description">{{.Meta.Description}}</p>
    </div>
    {{end}}
    {{if .Meta.Tags}}
    <div class="section">
      <h2>RAWG Community Tags</h2>
      <div class="tag-cloud">
        {{range .Meta.Tags}}<span class="tag rawg-tag"><span>{{.Name}}</span></span>{{end}}
      </div>
    </div>
    {{end}}
  {{end}}
{{end}}

<footer><a href="/about">about this library</a></footer>
</div>
</body>
</html>`

const aboutTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>about — retro library</title>
<style>
` + sharedCSS + `
  .page { max-width: 720px; margin: 0 auto; padding: 4rem 1.5rem 2rem; line-height: 1.7; }
  h1 { color: #adf; font-size: 1.2rem; margin-bottom: .2rem; }
  h2 { color: #7af; font-size: .95rem; margin: 1.5rem 0 .4rem; border-bottom: 1px solid #333; padding-bottom: .2rem; }
  a { color: #7af; }
  p { margin: .5rem 0; color: #bbc; }
  .mistake { background: #1a1a1a; border-left: 3px solid #445; padding: .4rem .7rem; margin: .4rem 0; border-radius: 0 3px 3px 0; font-size: .85rem; }
  .mistake .rom { color: #ddd; }
  .mistake .got { color: #f88; }
  footer { margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #333; font-size: .8rem; }
  footer a { color: #555; }
</style>
</head>
<body>
<header id="site-header">
  <a class="logo" href="/">rom-tagger</a>
  <nav>
    <a href="/">games</a>
    <a href="/tags">tags</a>
    <a href="/viz">viz</a>
    <a href="/views">saved views</a>
  </nav>
</header>
<div class="page">
<h1>about this library</h1>
<p>A personal retro game library with semantic vibe tags — built to answer the question <em>"what do I feel like playing right now?"</em> rather than <em>"what platform is this on?"</em></p>

<h2>where the data comes from</h2>
<p>Game metadata (titles, descriptions, release dates, community tags) is fetched from <a href="https://rawg.io" target="_blank">RAWG.io</a>, a community-curated video game database. Results are cached locally — each game hits the API at most once.</p>
<p>Semantic vibe tags are generated by locally-deployed language models analysing each game's description and community tag set. Tags describe the <em>experience</em> of playing: emotional feel, session shape, engagement style — not genre or franchise.</p>
<p>Embedding vectors (used for the <a href="/viz">viz</a> scatter plot) are computed locally using <strong>BAAI/bge-m3</strong>, a multilingual sentence embedding model.</p>

<h2>the misidentification problem</h2>
<p>RAWG's search API matches by name similarity with no platform or era awareness. Modern indie games on itch.io with retro-sounding names frequently beat the 1980s originals in search results. We filter matches by requiring that the returned title <em>actually corresponds</em> to the ROM name — not just shares a word.</p>
<p>Some of the more spectacular misidentifications we've caught and corrected:</p>
<div class="mistake"><span class="rom">VR Troopers (Game Gear, 1995)</span> → matched <span class="got">Starship Troopers: Extermination (2023)</span></div>
<div class="mistake"><span class="rom">The Immortal (NES, 1990)</span> → matched <span class="got">Diablo: Immortal (2022)</span></div>
<div class="mistake"><span class="rom">Halley Wars (Game Gear, 1992)</span> → matched <span class="got">Star Wars Jedi: Fallen Order (2019)</span></div>
<div class="mistake"><span class="rom">Madden 96 (Game Gear, 1995)</span> → matched <span class="got">Road 96 (2021)</span></div>
<p>Games that can't be reliably identified are hidden from the library rather than shown with bad data. We're working on <a href="https://github.com/b4ux1t3/infrastructure/issues/87">OpenVGDB integration</a> to use ROM checksums for exact identification.</p>

<h2>the viz</h2>
<p>The <a href="/viz">scatter plot</a> projects every game onto two axes defined by "lighthouse tags" you choose. Each axis is the cosine similarity between a game's description embedding and the embedding of that tag — so games cluster toward the tags that genuinely fit their vibe. It's a surprisingly honest map of a collection.</p>

<footer><a href="/">← back to library</a></footer>
</div>
</body>
</html>`

const vizTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>viz — rom-tagger</title>
<style>
` + sharedCSS + `
  body { overflow: hidden; }

  /* controls panel */
  #controls { position: fixed; top: 52px; left: 1rem; z-index: 10; background: #1a1a1a; border: 1px solid #333; padding: .75rem 1rem; border-radius: 4px; min-width: 220px; }
  #controls label { color: #aaa; font-size: .8rem; display: block; margin-bottom: .25rem; }
  #controls button { background: #2a2a4a; border: 1px solid #445; color: #adf; padding: .3rem .7rem; border-radius: 3px; cursor: pointer; font-family: monospace; margin-top: .5rem; width: 100%; }
  #controls button:hover { background: #3a3a6a; }

  /* combobox */
  .combo { position: relative; width: 180px; }
  .combo input {
    background: #222; border: 1px solid #555; color: #ddd;
    padding: .2rem .4rem; width: 100%; box-sizing: border-box;
    border-radius: 3px; font-family: monospace; font-size: .85rem;
  }
  .combo input:focus { outline: none; border-color: #7af; }
  .combo .dropdown {
    display: none; position: absolute; top: 100%; left: 0; right: 0;
    background: #1e1e2e; border: 1px solid #445; border-top: none;
    max-height: 180px; overflow-y: auto; z-index: 100;
    border-radius: 0 0 3px 3px;
  }
  .combo .dropdown.open { display: block; }
  .combo .dropdown div {
    padding: .25rem .4rem; font-size: .8rem; cursor: pointer; color: #ccd;
  }
  .combo .dropdown div:hover, .combo .dropdown div.active { background: #2a2a4a; color: #adf; }

  canvas { display: block; }

  /* tooltip */
  #tooltip { position: fixed; background: #1a1a2e; border: 1px solid #445; padding: .4rem .7rem; border-radius: 3px; font-size: .8rem; pointer-events: none; display: none; max-width: 260px; }
  #tooltip .name { color: #fff; font-weight: bold; margin-bottom: .2rem; }
  #tooltip .tags { color: #99a; font-size: .75rem; }

  /* status */
  #status { position: fixed; bottom: 1rem; left: 1rem; color: #555; font-size: .75rem; }

  /* platform filter */
  #platforms { position: fixed; top: 52px; right: 1rem; z-index: 10; background: #1a1a1a; border: 1px solid #333; padding: .6rem .9rem; border-radius: 4px; font-size: .78rem; }
  #platforms h4 { margin: 0 0 .4rem; color: #aaa; font-size: .75rem; font-weight: normal; text-transform: uppercase; letter-spacing: .05em; }
  .plat-row { display: flex; align-items: center; gap: .4rem; margin: .2rem 0; cursor: pointer; }
  .plat-row input { cursor: pointer; accent-color: #7af; }
  .plat-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
  .plat-label { color: #ccd; }

  /* save view */
  #saveRow { display: flex; gap: .4rem; margin-top: .5rem; }
  #saveRow input { background: #222; border: 1px solid #555; color: #ddd; padding: .2rem .4rem; border-radius: 3px; font-family: monospace; font-size: .8rem; flex: 1; min-width: 0; }
  #saveRow input:focus { outline: none; border-color: #7af; }
  #saveRow button { background: #1a3a2a; border: 1px solid #3a6a4a; color: #6df; padding: .2rem .5rem; border-radius: 3px; cursor: pointer; font-family: monospace; font-size: .8rem; white-space: nowrap; }
  #saveRow button:hover { background: #2a4a3a; }
  #saveMsg { font-size: .75rem; color: #6d8; margin-top: .3rem; display: none; }

  /* help button + card */
  #helpBtn {
    position: fixed; bottom: 1rem; right: 1rem; z-index: 20;
    width: 1.6rem; height: 1.6rem; border-radius: 50%;
    background: #1a1a2e; border: 1px solid #445; color: #7af;
    font-size: .9rem; cursor: pointer; display: flex; align-items: center; justify-content: center;
  }
  #helpBtn:hover { background: #2a2a4a; }
  #helpCard {
    display: none; position: fixed; bottom: 3rem; right: 1rem; z-index: 20;
    background: #1a1a2e; border: 1px solid #445; border-radius: 4px;
    padding: .8rem 1rem; max-width: 260px; font-size: .78rem; line-height: 1.5;
  }
  #helpCard.open { display: block; }
  #helpCard h3 { margin: 0 0 .4rem; font-size: .82rem; color: #adf; }
  #helpCard p { margin: .3rem 0; color: #aab; }
  #helpCard kbd { background: #2a2a4a; border: 1px solid #445; border-radius: 2px; padding: 0 .3rem; color: #ddf; }
</style>
</head>
<body>
<header id="site-header">
  <a class="logo" href="/">rom-tagger</a>
  <nav>
    <a href="/">games</a>
    <a href="/tags">tags</a>
    <a href="/viz" class="active">viz</a>
    <a href="/views">saved views</a>
  </nav>
</header>
<div id="controls">
  <label>X axis</label>
  <div class="combo" id="comboX">
    <input id="xTag" value="{{.XTag}}" placeholder="tag…" autocomplete="off">
    <div class="dropdown" id="dropX"></div>
  </div>
  <label style="margin-top:.5rem">Y axis</label>
  <div class="combo" id="comboY">
    <input id="yTag" value="{{.YTag}}" placeholder="tag…" autocomplete="off">
    <div class="dropdown" id="dropY"></div>
  </div>
  <button onclick="replot()">Plot</button>
  <button onclick="lucky()">I'm feeling lucky</button>
  <hr style="border:none;border-top:1px solid #333;margin:.5rem 0">
  <div style="margin-bottom:.3rem">
    <input id="viewName" placeholder="name this view…" style="width:100%;box-sizing:border-box;background:#222;border:1px solid #555;color:#ddd;padding:.2rem .4rem;border-radius:3px;font-family:monospace;font-size:.8rem">
  </div>
  <div id="saveRow">
    <button onclick="saveView()" style="width:100%">Save</button>
  </div>
  <div id="saveMsg">✓ saved</div>
</div>
<div id="platforms"><h4>Platforms</h4><div id="platList"></div></div>
<canvas id="c"></canvas>
<div id="tooltip"><div class="name"></div><div class="tags"></div></div>
<div id="status"></div>
<button id="helpBtn" title="How to use">?</button>
<div id="helpCard">
  <h3>How the viz works</h3>
  <p><strong>X / Y axes</strong> are "lighthouse tags" — each axis measures how similar a game's description embedding is to that tag. Games cluster toward tags that fit their vibe.</p>
  <p><strong>Hover</strong> a dot to see the game name and its tags.</p>
  <p><strong>Click</strong> a dot to open the game page.</p>
  <p><strong>Change axes</strong> by typing in the tag fields above — the dropdown filters as you type. Hit <kbd>Enter</kbd> or click Plot.</p>
  <p><strong>Filter platforms</strong> using the checkboxes on the right.</p>
  <p><strong>Save a view</strong> by giving it a name and clicking Save — axes and active platform filters are restored when you load it from Saved Views.</p>
</div>
<script>
const data = {{.PointsJSON}};
const allTags = {{.TagsJSON}};
const savedPlatforms = {{.PlatformsJSON}};

// ── Platform colors ───────────────────────────────────────────────────────────
const PLAT_COLORS = {
  nes:          '#e05a5a',
  snes:         '#8a5ae0',
  n64:          '#5a8ae0',
  gamegear:     '#5ae0b4',
  mastersystem: '#e0b45a',
  gba:          '#b45ae0',
};
const DEFAULT_COLOR = 'rgba(100,180,255,0.6)';
function platColor(p) { return PLAT_COLORS[p] || DEFAULT_COLOR; }

// ── Platform filter state ────────────────────────────────────────────────────
const platforms = [...new Set(data.map(d => d.platform).filter(Boolean))].sort();
const enabled = Object.fromEntries(platforms.map(p => [p, true]));

// ── Hash sync ─────────────────────────────────────────────────────────────────
function readHash() {
  // Restore from server-injected saved platforms first
  if (savedPlatforms && Array.isArray(savedPlatforms)) {
    const active = new Set(savedPlatforms);
    platforms.forEach(p => { enabled[p] = active.has(p); });
    return;
  }
  const h = window.location.hash.slice(1);
  if (!h) return;
  const active = new Set(h.split(',').map(s => s.trim()).filter(Boolean));
  platforms.forEach(p => { enabled[p] = active.has(p); });
}

function writeHash() {
  const active = platforms.filter(p => enabled[p]);
  // If all enabled, clear hash (default state); otherwise list the enabled ones
  window.location.hash = active.length === platforms.length ? '' : active.join(',');
}

const checkboxes = {};

function buildPlatFilter() {
  const list = document.getElementById('platList');
  readHash();
  platforms.forEach(p => {
    const row = document.createElement('label');
    row.className = 'plat-row';
    const cb = document.createElement('input');
    cb.type = 'checkbox'; cb.checked = enabled[p];
    cb.addEventListener('change', () => { enabled[p] = cb.checked; writeHash(); draw(); });
    checkboxes[p] = cb;
    const dot = document.createElement('div');
    dot.className = 'plat-dot';
    dot.style.background = platColor(p);
    const lbl = document.createElement('span');
    lbl.className = 'plat-label'; lbl.textContent = p;
    row.append(cb, dot, lbl);
    list.appendChild(row);
  });
}
buildPlatFilter();

// Keep checkboxes in sync if someone edits the hash manually / uses back/forward
window.addEventListener('hashchange', () => {
  readHash();
  platforms.forEach(p => { if (checkboxes[p]) checkboxes[p].checked = enabled[p]; });
  draw();
});

function visibleData() { return data.filter(d => !d.platform || enabled[d.platform]); }
const canvas = document.getElementById('c');
const ctx = canvas.getContext('2d');
const tooltip = document.getElementById('tooltip');
let W, H, PAD = 60;

// ── Combobox ──────────────────────────────────────────────────────────────────
function makeCombo(inputId, dropId) {
  const input = document.getElementById(inputId);
  const drop  = document.getElementById(dropId);
  let activeIdx = -1;

  function render(q) {
    const lq = q.toLowerCase();
    const matches = allTags.filter(t => t.includes(lq)).slice(0, 40);
    drop.innerHTML = '';
    activeIdx = -1;
    if (!matches.length || !q) { drop.classList.remove('open'); return; }
    matches.forEach((t, i) => {
      const d = document.createElement('div');
      d.textContent = t;
      d.addEventListener('mousedown', e => { e.preventDefault(); input.value = t; drop.classList.remove('open'); });
      drop.appendChild(d);
    });
    drop.classList.add('open');
  }

  function setActive(idx) {
    const items = drop.querySelectorAll('div');
    items.forEach(el => el.classList.remove('active'));
    activeIdx = Math.max(-1, Math.min(idx, items.length - 1));
    if (activeIdx >= 0) items[activeIdx].classList.add('active');
  }

  input.addEventListener('input', () => render(input.value));
  input.addEventListener('focus', () => { if (input.value) render(input.value); });
  input.addEventListener('blur',  () => setTimeout(() => drop.classList.remove('open'), 150));
  input.addEventListener('keydown', e => {
    if (!drop.classList.contains('open')) { if (e.key === 'Enter') replot(); return; }
    if (e.key === 'ArrowDown')  { e.preventDefault(); setActive(activeIdx + 1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setActive(activeIdx - 1); }
    else if (e.key === 'Enter') {
      e.preventDefault();
      const items = drop.querySelectorAll('div');
      if (activeIdx >= 0 && items[activeIdx]) { input.value = items[activeIdx].textContent; }
      drop.classList.remove('open');
      replot();
    } else if (e.key === 'Escape') { drop.classList.remove('open'); }
  });
}

makeCombo('xTag', 'dropX');
makeCombo('yTag', 'dropY');

// ── Help card ─────────────────────────────────────────────────────────────────
const helpCard = document.getElementById('helpCard');

function dismissHelp() {
  helpCard.classList.remove('open');
  localStorage.setItem('helpSeen', '1');
}

if (!localStorage.getItem('helpSeen')) helpCard.classList.add('open');

document.getElementById('helpBtn').addEventListener('click', () => {
  if (helpCard.classList.contains('open')) { dismissHelp(); } else { helpCard.classList.add('open'); }
});
document.addEventListener('click', e => {
  if (!e.target.closest('#helpBtn') && !e.target.closest('#helpCard')) dismissHelp();
});

// ── Canvas ────────────────────────────────────────────────────────────────────
function resize() {
  W = canvas.width  = window.innerWidth;
  H = canvas.height = window.innerHeight;
  draw();
}

function draw() {
  const vis = visibleData();
  if (!data.length) { ctx.fillStyle='#555'; ctx.font='14px monospace'; ctx.fillText('No data — run sync_game_embeddings first, or check tag names.', PAD, H/2); return; }
  ctx.clearRect(0, 0, W, H);

  const xs = data.map(d=>d.x), ys = data.map(d=>d.y);
  const minX=Math.min(...xs), maxX=Math.max(...xs);
  const minY=Math.min(...ys), maxY=Math.max(...ys);
  const scX = x => PAD + (x-minX)/(maxX-minX||1) * (W-PAD*2);
  const scY = y => H-PAD - (y-minY)/(maxY-minY||1) * (H-PAD*2);

  ctx.strokeStyle='#333'; ctx.lineWidth=1;
  ctx.beginPath(); ctx.moveTo(PAD,PAD); ctx.lineTo(PAD,H-PAD); ctx.lineTo(W-PAD,H-PAD); ctx.stroke();

  ctx.fillStyle='#666'; ctx.font='11px monospace'; ctx.textAlign='center';
  ctx.fillText('← low   ' + data[0]?.xLabel + '   high →', W/2, H-10);
  ctx.save(); ctx.translate(14, H/2); ctx.rotate(-Math.PI/2);
  ctx.fillText('← low   ' + data[0]?.yLabel + '   high →', 0, 0);
  ctx.restore();

  vis.forEach(d => {
    const px = scX(d.x), py = scY(d.y);
    ctx.beginPath();
    ctx.arc(px, py, 4, 0, Math.PI*2);
    ctx.fillStyle = platColor(d.platform);
    ctx.globalAlpha = 0.75;
    ctx.fill();
    ctx.globalAlpha = 1;
  });

  document.getElementById('status').textContent = vis.length + ' games plotted';
}

canvas.addEventListener('mousemove', e => {
  const vis = visibleData();
  if (!vis.length) return;
  const xs = data.map(d=>d.x), ys = data.map(d=>d.y);
  const minX=Math.min(...xs), maxX=Math.max(...xs);
  const minY=Math.min(...ys), maxY=Math.max(...ys);
  const scX = x => PAD + (x-minX)/(maxX-minX||1) * (W-PAD*2);
  const scY = y => H-PAD - (y-minY)/(maxY-minY||1) * (H-PAD*2);

  let nearest = null, bestDist = 20;
  vis.forEach(d => {
    const dx = scX(d.x)-e.clientX, dy = scY(d.y)-e.clientY;
    const dist = Math.sqrt(dx*dx+dy*dy);
    if (dist < bestDist) { bestDist=dist; nearest=d; }
  });
  if (nearest) {
    tooltip.style.display='block';
    tooltip.style.left=(e.clientX+12)+'px';
    tooltip.style.top=(e.clientY-10)+'px';
    tooltip.querySelector('.name').textContent = nearest.name;
    tooltip.querySelector('.tags').textContent = (nearest.platform ? '['+nearest.platform+'] ' : '') + nearest.tags.join(', ');
  } else {
    tooltip.style.display='none';
  }
});

canvas.addEventListener('click', e => {
  const vis = visibleData();
  if (!vis.length) return;
  const xs = data.map(d=>d.x), ys = data.map(d=>d.y);
  const minX=Math.min(...xs), maxX=Math.max(...xs);
  const minY=Math.min(...ys), maxY=Math.max(...ys);
  const scX = x => PAD + (x-minX)/(maxX-minX||1) * (W-PAD*2);
  const scY = y => H-PAD - (y-minY)/(maxY-minY||1) * (H-PAD*2);
  vis.forEach(d => {
    const dx = scX(d.x)-e.clientX, dy = scY(d.y)-e.clientY;
    if (Math.sqrt(dx*dx+dy*dy) < 8) window.open('/game/'+encodeURIComponent(d.name), '_blank');
  });
});

function lucky() {
  const pick = () => allTags[Math.floor(Math.random() * allTags.length)];
  let x = pick(), y = pick();
  while (y === x) y = pick();
  window.location.href = '/viz?x='+encodeURIComponent(x)+'&y='+encodeURIComponent(y);
}

function replot() {
  const x = document.getElementById('xTag').value.trim();
  const y = document.getElementById('yTag').value.trim();
  if (x && y) window.location.href = '/viz?x='+encodeURIComponent(x)+'&y='+encodeURIComponent(y);
}

async function saveView() {
  const name = document.getElementById('viewName').value.trim();
  const x = document.getElementById('xTag').value.trim();
  const y = document.getElementById('yTag').value.trim();
  if (!name || !x || !y) return;
  const activePlatforms = platforms.filter(p => enabled[p]);
  const resp = await fetch('/viz/save', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({name, x_tag: x, y_tag: y, platforms: activePlatforms})
  });
  if (resp.ok) {
    const msg = document.getElementById('saveMsg');
    msg.style.display = 'block';
    setTimeout(() => { msg.style.display = 'none'; }, 2000);
  }
}

window.addEventListener('resize', resize);
resize();
</script>
</body>
</html>`

const viewsTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>saved views — rom-tagger</title>
<style>
` + sharedCSS + `
  .page { max-width: 900px; margin: 0 auto; padding: 4rem 1rem 2rem; }
  h1 { color: #fff; font-size: 1.1rem; margin-bottom: 1.2rem; }
  .view-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 1rem; }
  .view-card {
    background: #1a1a2e; border: 1px solid #2a2a40; border-radius: 4px;
    padding: .8rem 1rem; text-decoration: none; display: block;
    transition: border-color .15s;
  }
  .view-card:hover { border-color: #7af; }
  .view-card .vname { color: #adf; font-size: .95rem; margin-bottom: .4rem; }
  .view-card .vaxes { color: #778; font-size: .8rem; }
  .view-card .vaxes span { color: #99b; }
  .view-card .vdate { color: #445; font-size: .72rem; margin-top: .5rem; }
  .empty { color: #555; font-style: italic; margin-top: 2rem; }
  footer { margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #333; font-size: .8rem; }
  footer a { color: #555; }
</style>
</head>
<body>
<header id="site-header">
  <a class="logo" href="/">rom-tagger</a>
  <nav>
    <a href="/">games</a>
    <a href="/tags">tags</a>
    <a href="/viz">viz</a>
    <a href="/views" class="active">saved views</a>
  </nav>
</header>
<div class="page">
<h1>saved views</h1>
{{if .}}
<div class="view-grid">
  {{range .}}
  <a class="view-card" href="/viz?x={{.XTag | urlquery}}&y={{.YTag | urlquery}}{{if .PlatformsJSON}}&platforms={{.PlatformsJSON | urlquery}}{{end}}">
    <div class="vname">{{.Name}}</div>
    <div class="vaxes">x: <span>{{.XTag}}</span> &nbsp; y: <span>{{.YTag}}</span></div>
    <div class="vdate">{{.CreatedAt}}</div>
  </a>
  {{end}}
</div>
{{else}}
<p class="empty">No saved views yet — head to the <a href="/viz" style="color:#7af">viz</a> and save one.</p>
{{end}}
<footer><a href="/about">about this library</a></footer>
</div>
</body>
</html>`

const reviewTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>review queue — rom-tagger</title>
<style>
` + sharedCSS + `
  .page { max-width: 960px; margin: 0 auto; padding: 4rem 1rem 2rem; }
  h1 { color: #fff; font-size: 1.1rem; margin-bottom: .4rem; }
  .subtitle { color: #666; font-size: .82rem; margin-bottom: 1.5rem; }
  table { width: 100%; border-collapse: collapse; }
  th { text-align: left; border-bottom: 1px solid #444; padding: .3rem .5rem; color: #aaa; font-size: .8rem; }
  td { padding: .4rem .5rem; border-bottom: 1px solid #1e1e1e; vertical-align: middle; font-size: .85rem; }
  td.plat { color: #778; font-size: .78rem; }
  td.notes { color: #f88; font-size: .75rem; font-style: italic; }
  .rawg-input { background: #1a1a1a; border: 1px solid #444; color: #ddd; padding: .2rem .4rem; border-radius: 3px; font-family: monospace; width: 90px; }
  .rawg-input:focus { outline: none; border-color: #7af; }
  .btn { background: #2a2a4a; border: 1px solid #445; color: #adf; padding: .2rem .5rem; border-radius: 3px; cursor: pointer; font-family: monospace; font-size: .8rem; }
  .btn:hover { background: #3a3a6a; }
  .btn-skip { background: #2a1a1a; border-color: #543; color: #a88; }
  .btn-skip:hover { background: #3a2a2a; }
  .empty { color: #555; font-style: italic; margin-top: 2rem; }
  footer { margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #333; font-size: .8rem; }
  footer a { color: #555; }
</style>
</head>
<body>
<header id="site-header">
  <a class="logo" href="/">rom-tagger</a>
  <nav>
    <a href="/">games</a>
    <a href="/tags">tags</a>
    <a href="/viz">viz</a>
    <a href="/views">saved views</a>
    <a href="/review" class="active">review</a>
  </nav>
</header>
<div class="page">
<h1>manual review queue</h1>
<p class="subtitle">Games hidden from the library due to missing or rejected RAWG metadata. Supply the correct RAWG ID to fetch real metadata, or skip to permanently hide.</p>
{{if .}}
<table>
  <tr><th>Game</th><th>Platform</th><th>Notes</th><th>RAWG ID</th><th></th></tr>
  {{range .}}
  <tr>
    <td>{{.Name}}</td>
    <td class="plat">{{.Platform}}</td>
    <td class="notes">{{.Notes}}</td>
    <td>
      <form method="post" action="/review/correct" style="display:inline-flex;gap:.3rem;align-items:center">
        <input type="hidden" name="game" value="{{.Name}}">
        <input class="rawg-input" type="number" name="rawg_id" placeholder="12345" min="1">
        <button class="btn" type="submit">Fix</button>
      </form>
    </td>
    <td>
      <form method="post" action="/review/skip" style="display:inline">
        <input type="hidden" name="game" value="{{.Name}}">
        <button class="btn btn-skip" type="submit">Skip</button>
      </form>
    </td>
  </tr>
  {{end}}
</table>
{{else}}
<p class="empty">Queue is empty — all games are identified or skipped.</p>
{{end}}
<footer><a href="/about">about this library</a></footer>
</div>
</body>
</html>`

var (
	pageTpl    = template.Must(template.New("page").Parse(pageTmpl))
	tagsTpl    = template.Must(template.New("tags").Parse(tagsTmpl))
	gameTpl    = template.Must(template.New("game").Parse(gameTmpl))
	vizTpl    = template.Must(template.New("viz").Parse(vizTmpl))
	aboutTpl  = template.Must(template.New("about").Parse(aboutTmpl))
	viewsTpl  = template.Must(template.New("views").Parse(viewsTmpl))
	reviewTpl = template.Must(template.New("review").Parse(reviewTmpl))
)

type pageData struct {
	Games     []GameEntry
	TagFilter string
	Query     string
}

type tagCount struct {
	Tag   string
	Count int
}

func Serve(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleGames)
	mux.HandleFunc("/tags", handleTags)
	mux.HandleFunc("/game/", handleGame)
	mux.HandleFunc("/api/health", handleAPIHealth)
	mux.HandleFunc("/api/games", handleAPIGames)
	mux.HandleFunc("/api/games/", handleAPIGame)
	mux.HandleFunc("/api/tags", handleAPITags)
	mux.HandleFunc("/api/playlists", handleAPIPlaylists)
	mux.HandleFunc("/api/playlists/", handleAPIPlaylist)
	mux.HandleFunc("/api/views", handleAPIViews)
	mux.HandleFunc("/api/review", handleAPIReview)
	mux.HandleFunc("/viz", handleViz)
	mux.HandleFunc("/viz/save", handleVizSave)
	mux.HandleFunc("/views", handleViews)
	mux.HandleFunc("/review", handleReview)
	mux.HandleFunc("/review/correct", handleReviewCorrect)
	mux.HandleFunc("/review/skip", handleReviewSkip)
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		aboutTpl.Execute(w, nil)
	})
	return http.ListenAndServe(addr, mux)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"status":  status,
			"message": msg,
		},
	})
}

func filterBrowsableGames(q, tagFilter, platform string, verifiedOnly bool) []GameEntry {
	q = strings.ToLower(q)
	games, _ := dbListGames(platform)

	if verifiedOnly {
		validKeys, _ := dbListCachedKeys()
		validSet := make(map[string]bool, len(validKeys))
		for _, k := range validKeys {
			validSet[k] = true
		}
		filtered := games[:0]
		for _, g := range games {
			if isSystemFile(g.Name) {
				continue
			}
			if !validSet[cacheKey(g.Name)] {
				continue
			}
			filtered = append(filtered, g)
		}
		games = filtered
	}

	sort.Slice(games, func(i, j int) bool { return games[i].Name < games[j].Name })

	if tagFilter != "" {
		filtered := games[:0]
		for _, g := range games {
			for _, t := range g.Tags {
				if t == tagFilter {
					filtered = append(filtered, g)
					break
				}
			}
		}
		games = filtered
	}

	if q != "" {
		filtered := games[:0]
		for _, g := range games {
			if strings.Contains(strings.ToLower(g.Name), q) {
				filtered = append(filtered, g)
				continue
			}
			for _, t := range g.Tags {
				if strings.Contains(strings.ToLower(t), q) {
					filtered = append(filtered, g)
					break
				}
			}
		}
		games = filtered
	}

	return games
}

// isSystemFile returns true for ROM collection metadata files that sneak into
// the games table (e.g. "sega-game-gear-romset-ultra-us_meta.sqlite").
func isSystemFile(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".sqlite", ".db", ".xml", ".dat", ".txt"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func handleGames(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("q"))
	tagFilter := r.URL.Query().Get("tag")
	games := filterBrowsableGames(q, tagFilter, "", true)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	pageTpl.Execute(w, pageData{Games: games, TagFilter: tagFilter, Query: r.URL.Query().Get("q")})
}

func handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"service": "rom-tagger",
	})
}

func handleAPIGames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query().Get("q")
	tag := r.URL.Query().Get("tag")
	platform := r.URL.Query().Get("platform")
	verifiedOnly := r.URL.Query().Get("verified") != "0"
	games := filterBrowsableGames(q, tag, platform, verifiedOnly)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"games":    games,
		"count":    len(games),
		"query":    q,
		"tag":      tag,
		"platform": platform,
		"verified": verifiedOnly,
	})
}

func handleAPIGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/games/")
	if n, err := url.QueryUnescape(name); err == nil {
		name = n
	}
	if name == "" {
		apiError(w, http.StatusBadRequest, "missing game name")
		return
	}
	entry, err := dbGetGame(name)
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entry == nil {
		apiError(w, http.StatusNotFound, "game not found")
		return
	}
	meta, _ := dbGetCachedMeta(cacheKey(entry.Name))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"game": entry,
		"meta": meta,
	})
}

func handleAPITags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rows, err := dbTagCounts()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tags":   rows,
		"count":  len(rows),
	})
}

func handleAPIPlaylists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	playlists, err := dbListPlaylists()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"playlists": playlists,
		"count":     len(playlists),
	})
}

func handleAPIPlaylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/playlists/")
	if n, err := url.QueryUnescape(name); err == nil {
		name = n
	}
	if name == "" {
		apiError(w, http.StatusBadRequest, "missing playlist name")
		return
	}
	entry, games, err := dbGetPlaylist(name)
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entry == nil {
		apiError(w, http.StatusNotFound, "playlist not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"playlist": entry,
		"games":    games,
	})
}

func handleAPIViews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	views, err := dbListViews()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"views": views,
		"count": len(views),
	})
}

func handleAPIReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !isTailscale(r) {
		apiError(w, http.StatusNotFound, "not found")
		return
	}
	queue, err := dbListReviewQueue()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"review_queue": queue,
		"count":        len(queue),
	})
}

type gamePageData struct {
	Game    GameEntry
	Meta    *RAWGMeta
	ShowArt bool
}

func handleGame(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/game/")
	name = strings.ReplaceAll(name, "+", " ")
	if n, err := url.QueryUnescape(name); err == nil {
		name = n
	}
	if name == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	entry, err := dbGetGame(name)
	if err != nil || entry == nil {
		http.NotFound(w, r)
		return
	}
	meta, _ := dbGetCachedMeta(cacheKey(entry.Name))
	showArt := strings.HasSuffix(r.Host, ".onion")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	gameTpl.Execute(w, gamePageData{Game: *entry, Meta: meta, ShowArt: showArt})
}

func handleTags(w http.ResponseWriter, r *http.Request) {
	rows, _ := dbTagCounts()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tagsTpl.Execute(w, rows)
}

type vizPoint struct {
	Name     string   `json:"name"`
	Platform string   `json:"platform"`
	X        float32  `json:"x"`
	Y        float32  `json:"y"`
	Tags     []string `json:"tags"`
	XLabel   string   `json:"xLabel"`
	YLabel   string   `json:"yLabel"`
}

type vizData struct {
	XTag          string
	YTag          string
	PointsJSON    template.JS
	TagsJSON      template.JS
	PlatformsJSON template.JS
}

func handleViz(w http.ResponseWriter, r *http.Request) {
	xTag := r.URL.Query().Get("x")
	yTag := r.URL.Query().Get("y")
	if xTag == "" { xTag = "action-forward" }
	if yTag == "" { yTag = "chill" }

	xVec, xOK := dbGetTagEmbedding(xTag)
	yVec, yOK := dbGetTagEmbedding(yTag)

	// Build valid game set (same filter as /games).
	validKeys, _ := dbListCachedKeys()
	validSet := make(map[string]bool, len(validKeys))
	for _, k := range validKeys {
		validSet[k] = true
	}

	var points []vizPoint
	if xOK && yOK {
		gameEmbeds, err := dbGetAllGameEmbeddingsForViz()
		if err == nil {
			games, _ := dbListGames("")
			tagMap := make(map[string][]string, len(games))
			platMap := make(map[string]string, len(games))
			for _, g := range games {
				if isSystemFile(g.Name) || !validSet[cacheKey(g.Name)] {
					continue
				}
				tagMap[g.Name] = g.Tags
				platMap[g.Name] = g.Platform
			}
			for name, vec := range gameEmbeds {
				if _, ok := tagMap[name]; !ok {
					continue // system file or unverified
				}
				points = append(points, vizPoint{
					Name:     name,
					Platform: platMap[name],
					X:        cosineSim(vec, xVec),
					Y:        cosineSim(vec, yVec),
					Tags:     tagMap[name],
					XLabel:   xTag,
					YLabel:   yTag,
				})
			}
		}
	}

	var js []byte
	if len(points) > 0 {
		js, _ = json.Marshal(points)
	} else {
		js = []byte("[]")
	}

	// Build tag name list for autocomplete — only tags from verified games.
	tagSet := make(map[string]struct{})
	for i := range points {
		for _, t := range points[i].Tags {
			tagSet[t] = struct{}{}
		}
	}
	tagNames := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tagNames = append(tagNames, t)
	}
	sort.Strings(tagNames)
	tagsJS, _ := json.Marshal(tagNames)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	platFilter := r.URL.Query().Get("platforms")
	platJS := template.JS("null")
	if platFilter != "" {
		platJS = template.JS(platFilter)
	}
	vizTpl.Execute(w, vizData{XTag: xTag, YTag: yTag, PointsJSON: template.JS(js), TagsJSON: template.JS(tagsJS), PlatformsJSON: platJS})
}

func handleVizSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name      string   `json:"name"`
		XTag      string   `json:"x_tag"`
		YTag      string   `json:"y_tag"`
		Platforms []string `json:"platforms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.XTag == "" || req.YTag == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Re-use the current viz computation to get points JSON.
	xVec, xOK := dbGetTagEmbedding(req.XTag)
	yVec, yOK := dbGetTagEmbedding(req.YTag)
	if !xOK || !yOK {
		http.Error(w, "unknown tag", http.StatusBadRequest)
		return
	}

	validKeys, _ := dbListCachedKeys()
	validSet := make(map[string]bool, len(validKeys))
	for _, k := range validKeys {
		validSet[k] = true
	}

	gameEmbeds, err := dbGetAllGameEmbeddingsForViz()
	if err != nil {
		http.Error(w, "embed error", http.StatusInternalServerError)
		return
	}
	games, _ := dbListGames("")
	tagMap := make(map[string][]string, len(games))
	platMap := make(map[string]string, len(games))
	for _, g := range games {
		if isSystemFile(g.Name) || !validSet[cacheKey(g.Name)] {
			continue
		}
		tagMap[g.Name] = g.Tags
		platMap[g.Name] = g.Platform
	}
	var points []vizPoint
	for name, vec := range gameEmbeds {
		if _, ok := tagMap[name]; !ok {
			continue
		}
		points = append(points, vizPoint{
			Name:     name,
			Platform: platMap[name],
			X:        cosineSim(vec, xVec),
			Y:        cosineSim(vec, yVec),
			Tags:     tagMap[name],
			XLabel:   req.XTag,
			YLabel:   req.YTag,
		})
	}
	js, _ := json.Marshal(points)

	platJS, _ := json.Marshal(req.Platforms)
	if err := dbSaveView(req.Name, req.XTag, req.YTag, string(js), string(platJS)); err != nil {
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleViews(w http.ResponseWriter, r *http.Request) {
	views, _ := dbListViews()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	viewsTpl.Execute(w, views)
}

func isTailscale(r *http.Request) bool {
	return r.Header.Get("X-Tailscale") == "1"
}

func handleReview(w http.ResponseWriter, r *http.Request) {
	if !isTailscale(r) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	queue, _ := dbListReviewQueue()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	reviewTpl.Execute(w, queue)
}

func handleReviewCorrect(w http.ResponseWriter, r *http.Request) {
	if !isTailscale(r) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	gameName := r.FormValue("game")
	rawgIDStr := r.FormValue("rawg_id")
	if gameName == "" || rawgIDStr == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	var rawgID int
	if _, err := fmt.Sscanf(rawgIDStr, "%d", &rawgID); err != nil || rawgID <= 0 {
		http.Error(w, "invalid rawg_id", http.StatusBadRequest)
		return
	}
	if _, err := FetchGameMetadataByID(gameName, rawgID); err != nil {
		http.Error(w, "RAWG fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	dbRecordCorrection(gameName, rawgID, "")
	http.Redirect(w, r, "/review", http.StatusSeeOther)
}

func handleReviewSkip(w http.ResponseWriter, r *http.Request) {
	if !isTailscale(r) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	gameName := r.FormValue("game")
	if gameName == "" {
		http.Error(w, "missing game", http.StatusBadRequest)
		return
	}
	dbMarkSkip(gameName, "manually skipped")
	http.Redirect(w, r, "/review", http.StatusSeeOther)
}
