package main

// uiCSS is the panel's stylesheet.
//
// The design follows a "box > header" pattern: every card carries its own title and
// its own actions in the header, with a flexible spacer (.grow) pushing the buttons
// to the right. Actions used to be collected in a toolbar at the top of the page,
// far from the table they acted on, which is what made the layout hard to follow —
// the operator had to remember which button belonged to which block.
//
// Structure of the file:
//
//	tokens      colours, spacing, fonts (dark and light)
//	shell       page frame and navigation
//	box         the card, its header, and the header's action slot
//	stats       the summary strip
//	table       data tables
//	forms       inputs, buttons, chips
//	feedback    toasts, empty states, log groups
//	narrow      phone adjustments
const uiCSS = `
/* ======================= tokens ======================= */
:root {
  color-scheme: dark;
  --bg: #0c0e14; --surface: #14171f; --surface-2: #1a1e28; --raise: #202531;
  --line: #262c3a; --line-soft: #1e232e;
  --ink: #e8ebf2; --ink-2: #a8b0c2; --ink-3: #6b7488;
  --accent: #5b7cfa; --accent-ink: #ffffff; --accent-soft: rgba(91,124,250,.13);
  --ok: #3ddc97; --ok-soft: rgba(61,220,151,.12);
  --warn: #f5b544; --warn-soft: rgba(245,181,68,.12);
  --bad: #f0655f; --bad-soft: rgba(240,101,95,.12);
  --shadow: 0 1px 2px rgba(0,0,0,.4), 0 8px 24px -12px rgba(0,0,0,.5);
  --mono: ui-monospace, "Cascadia Mono", "SF Mono", Consolas, monospace;
  --sans: system-ui, -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
}
[data-theme="light"] {
  color-scheme: light;
  --bg: #f4f5f8; --surface: #ffffff; --surface-2: #f8f9fb; --raise: #ffffff;
  --line: #e2e5ec; --line-soft: #eceef3;
  --ink: #14171f; --ink-2: #545c6e; --ink-3: #8b93a5;
  --accent: #3d5fe0; --accent-ink: #ffffff; --accent-soft: rgba(61,95,224,.09);
  --ok: #0f9d63; --ok-soft: rgba(15,157,99,.1);
  --warn: #b47611; --warn-soft: rgba(180,118,17,.11);
  --bad: #cf3b34; --bad-soft: rgba(207,59,52,.09);
  --shadow: 0 1px 2px rgba(16,20,32,.06), 0 8px 24px -14px rgba(16,20,32,.14);
}
* { box-sizing: border-box; margin: 0; padding: 0; }
/* [hidden] must win over any display rule: the panel switches pages by setting this
   attribute, and a stray display:flex elsewhere would keep a page visible. */
[hidden] { display: none !important; }
body {
  background: var(--bg); color: var(--ink); font: 14px/1.55 var(--sans);
  -webkit-font-smoothing: antialiased;
  min-height: 100vh;
}
::selection { background: var(--accent-soft); }
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 4px; }
a { color: var(--accent); text-decoration: none; }
code, .mono { font-family: var(--mono); font-size: .93em; }
@media (prefers-reduced-motion: reduce) { * { transition: none !important; animation: none !important; } }

/* ======================= shell ======================= */
/* The nav sits above the content as a horizontal bar.
   A side column was tried and rejected: this panel is usually viewed in a narrow
   in-app webview, where 196px of chrome costs more than it gives, and the horizontal
   strip keeps the full width for the tables. */
.shell { display: block; }
.nav {
  background: var(--surface); border-bottom: 1px solid var(--line);
  position: sticky; top: 0; z-index: 20;
  display: flex; align-items: center; gap: 14px;
  padding: 0 18px; overflow-x: auto;
}
.brand { padding: 12px 0; display: flex; align-items: baseline; gap: 8px; white-space: nowrap; }
.brand .name { font-weight: 650; letter-spacing: -.01em; font-size: 15px; }
.brand .sub { color: var(--ink-3); font-size: 11.5px; font-family: var(--mono); }
.nav ul { list-style: none; display: flex; gap: 3px; flex: 1; padding: 0; }
.nav a {
  display: inline-flex; align-items: center; gap: 7px;
  padding: 13px 14px; color: var(--ink-2); font-size: 13.5px; font-weight: 500;
  cursor: pointer; white-space: nowrap;
  /* The active marker is an underline on the bar's bottom edge rather than a pill:
     it reads as "which section am I in" without competing with the buttons inside
     the cards, which are the controls that change things. */
  border-bottom: 2px solid transparent; margin-bottom: -1px;
}
.nav a:hover { color: var(--ink); }
.nav a.on { color: var(--accent); border-bottom-color: var(--accent); font-weight: 600; }
.nav .foot { color: var(--ink-3); font-size: 11.5px; white-space: nowrap; padding: 12px 0; }

.main { padding: 20px 22px 44px; max-width: 1320px; margin: 0 auto; min-width: 0; }
.page-head { display: flex; align-items: flex-start; gap: 14px; margin-bottom: 18px; }
.page-head h1 { font-size: 20px; font-weight: 650; letter-spacing: -.02em; }
.page-head .sub { color: var(--ink-3); font-size: 12.5px; margin-top: 4px; }
.page-head .grow { flex: 1; }

/* ======================= box (card) ======================= */
.box {
  background: var(--surface); border: 1px solid var(--line); border-radius: 11px;
  box-shadow: var(--shadow); margin-bottom: 18px; overflow: hidden;
}
/* The header is the card's control strip: title on the left, actions on the right,
   with .grow as the flexible gap between them. Keeping actions here is what ties a
   button to the block it affects. */
.box > header {
  display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
  padding: 11px 16px; border-bottom: 1px solid var(--line-soft);
}
.box > header h3 { font-size: 13.5px; font-weight: 600; display: flex; align-items: baseline; gap: 7px; flex-wrap: wrap; }
.box > header h3 .hint { font-size: 11.5px; font-weight: 400; color: var(--ink-3); }
.box > header .grow { flex: 1; min-width: 0; }
.box > header .note { color: var(--ink-3); font-size: 12px; }
.box .pad { padding: 16px; }
.box .foot { padding: 10px 16px; border-top: 1px solid var(--line-soft); color: var(--ink-3); font-size: 12px; }

/* ======================= stats ======================= */
.stats {
  display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  border: 1px solid var(--line); border-radius: 11px; background: var(--surface);
  overflow: hidden; margin-bottom: 18px; box-shadow: var(--shadow);
}
.stat { padding: 13px 16px; border-right: 1px solid var(--line-soft); border-bottom: 1px solid var(--line-soft); }
.stat .v {
  font: 600 24px/1.15 var(--mono); font-variant-numeric: tabular-nums;
  letter-spacing: -.02em; white-space: nowrap;
}
.stat .k { color: var(--ink-3); font-size: 11.5px; margin-top: 4px; }
.stat.good .v { color: var(--ok); }
.stat.warn .v { color: var(--warn); }
.stat.bad .v { color: var(--bad); }

/* ======================= table ======================= */
/* Tables scroll inside their own box rather than moving the page: the page frame
   and the nav must stay put while the operator scrolls a wide table. */
.tbl-wrap { overflow-x: auto; -webkit-overflow-scrolling: touch; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th {
  text-align: left; font-weight: 500; font-size: 11.5px; color: var(--ink-3);
  padding: 9px 11px; border-bottom: 1px solid var(--line); white-space: nowrap;
}
td { padding: 9px 11px; border-bottom: 1px solid var(--line-soft); vertical-align: middle; }
tbody tr:last-child td { border-bottom: none; }
tbody tr:hover { background: var(--surface-2); }
th.num, td.num { text-align: right; font-variant-numeric: tabular-nums; }
td.actions { white-space: nowrap; text-align: right; }

/* ======================= badges ======================= */
.pill {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 2px 9px; border-radius: 999px; font-size: 11.5px; font-weight: 550;
  border: 1px solid transparent; white-space: nowrap;
}
.pill.ok { background: var(--ok-soft); color: var(--ok); }
.pill.warn { background: var(--warn-soft); color: var(--warn); }
.pill.bad { background: var(--bad-soft); color: var(--bad); }
.pill.idle { background: var(--surface-2); color: var(--ink-3); border-color: var(--line); }
.pill.info { background: var(--accent-soft); color: var(--accent); }
/* A health strip on the first cell of the account row: colour reads faster than text
   when scanning a list. */
td.bar { position: relative; padding-left: 15px; }
td.bar::before {
  content: ""; position: absolute; left: 0; top: 7px; bottom: 7px;
  width: 3px; border-radius: 0 2px 2px 0; background: var(--line);
}
td.bar.ok::before { background: var(--ok); }
td.bar.warn::before { background: var(--warn); }
td.bar.bad::before { background: var(--bad); }

/* ======================= controls ======================= */
button, .btn {
  font: inherit; font-size: 13px; font-weight: 500;
  color: var(--ink); background: var(--surface-2);
  border: 1px solid var(--line); border-radius: 8px;
  padding: 6px 13px; cursor: pointer; white-space: nowrap;
}
button:hover, .btn:hover { background: var(--raise); border-color: var(--ink-3); }
button:disabled { opacity: .5; cursor: default; }
button.primary { background: var(--accent); border-color: var(--accent); color: var(--accent-ink); }
button.primary:hover { filter: brightness(1.08); }
button.danger { color: var(--bad); }
button.danger:hover { background: var(--bad-soft); border-color: var(--bad); }
button.ghost { background: transparent; }
button.xs { padding: 3.5px 9px; font-size: 12.5px; border-radius: 6px; }
button.icon { padding: 6px 9px; }

input, select, textarea {
  font: inherit; font-size: 13px; color: var(--ink); background: var(--surface-2);
  border: 1px solid var(--line); border-radius: 8px; padding: 6px 10px;
}
input:focus, select:focus { border-color: var(--accent); outline: none; }
input[type=checkbox], input[type=radio] { width: 15px; height: 15px; accent-color: var(--accent); }
label.field { display: inline-flex; align-items: center; gap: 7px; color: var(--ink-2); font-size: 13px; }
label.field input[type=number] { width: 76px; }

.row { display: flex; gap: 9px; align-items: center; flex-wrap: wrap; }
.row.tight { margin-bottom: 8px; }
.grow { flex: 1; min-width: 0; }

/* Radio option cards, used for the routing strategy. */
.opt {
  display: flex; gap: 10px; align-items: flex-start;
  padding: 10px 12px; margin-bottom: 7px;
  border: 1px solid var(--line); border-radius: 9px; background: var(--surface-2); cursor: pointer;
}
.opt:hover { border-color: var(--accent); }
.opt input { margin-top: 3px; }
.opt .name { font-weight: 550; font-size: 13px; }
.opt .desc { color: var(--ink-3); font-size: 12px; }

/* Segmented control. */
.seg { display: inline-flex; border: 1px solid var(--line); border-radius: 9px; overflow: hidden; background: var(--surface-2); }
.seg button { border: none; border-radius: 0; background: transparent; }
.seg button.on { background: var(--accent); color: var(--accent-ink); }
.seg button + button { border-left: 1px solid var(--line); }

/* ======================= filter bar ======================= */
.filter-bar { display: flex; align-items: center; gap: 8px; padding: 11px 16px; flex-wrap: wrap; border-bottom: 1px solid var(--line-soft); }
.filter-search { position: relative; display: flex; align-items: center; flex: 1 1 200px; min-width: 0; }
.filter-search .filter-icon { position: absolute; left: 10px; color: var(--ink-3); pointer-events: none; }
.filter-search input[type=search] {
  width: 100%; padding-left: 31px; padding-right: 30px; min-height: 34px;
}
.filter-search input[type=search]::-webkit-search-decoration,
.filter-search input[type=search]::-webkit-search-cancel-button { -webkit-appearance: none; appearance: none; }
.filter-clear {
  position: absolute; right: 5px; width: 22px; height: 22px; padding: 0;
  display: inline-flex; align-items: center; justify-content: center;
  border: none; background: transparent; color: var(--ink-3); border-radius: 50%;
}
.filter-clear:hover { background: var(--line); color: var(--ink); }
.filter-count { color: var(--ink-3); font-size: 12px; }

/* ======================= chart ======================= */
.trend { padding: 4px 16px 0; }
.trend-svg { display: block; width: 100%; height: auto; max-height: 200px; }
.trend-ok { fill: var(--ok); }
.trend-bad { fill: var(--bad); }
.trend-legend { display: flex; gap: 14px; align-items: center; padding: 8px 16px 14px; color: var(--ink-3); font-size: 12px; flex-wrap: wrap; }
.trend-legend i.sw { display: inline-block; width: 9px; height: 9px; border-radius: 2px; margin-right: 5px; }
.trend-legend .sw-ok { background: var(--ok); }
.trend-legend .sw-bad { background: var(--bad); }

/* ======================= log groups ======================= */
i.mark {
  display: inline-block; width: 10px; height: 10px; flex: 0 0 10px;
  border-radius: 50%; border: 1.5px solid currentColor; box-sizing: border-box;
}
i.mark.ok { border-color: var(--ok); background: var(--ok); }
i.mark.skip { border-color: var(--ink-3); }
i.mark.err { border-color: var(--bad); background: var(--bad); }
i.mark.info { border-color: var(--accent); }
details.log-group { border: 1px solid var(--line); border-radius: 9px; margin-bottom: 8px; background: var(--surface-2); overflow: hidden; }
details.log-group > summary { display: flex; align-items: center; gap: 8px; padding: 9px 12px; cursor: pointer; font-size: 13px; list-style: none; }
details.log-group > summary::-webkit-details-marker { display: none; }
details.log-group > summary::after { content: '▾'; margin-left: auto; opacity: .5; }
details.log-group[open] > summary::after { transform: rotate(180deg); }
details.log-group .count { color: var(--ink-3); font-size: 12px; }
.log-body { padding: 2px 12px 10px; }
.log-entry { display: flex; gap: 8px; align-items: flex-start; padding: 5px 0; font-size: 12.5px; line-height: 1.55; }
.log-entry i.mark { margin-top: 4px; }
.log-text { word-break: break-word; }
.log-scroll { max-height: 320px; overflow-y: auto; }

/* ======================= misc ======================= */
.empty { padding: 22px 16px; color: var(--ink-3); font-size: 13px; text-align: center; }
.note { color: var(--ink-3); font-size: 12px; line-height: 1.6; }
.legend { display: flex; flex-wrap: wrap; gap: 14px; color: var(--ink-3); font-size: 12px; }
.legend span { display: inline-flex; align-items: center; gap: 6px; }
details > summary { cursor: pointer; }

#toasts {
  position: fixed; right: 16px; bottom: 16px; z-index: 60;
  display: flex; flex-direction: column; gap: 8px; align-items: flex-end;
  pointer-events: none; max-width: min(420px, calc(100vw - 32px));
}
.toast {
  pointer-events: auto;
  background: var(--surface); color: var(--ink);
  border: 1px solid var(--line); border-left: 3px solid var(--accent);
  border-radius: 9px; padding: 9px 13px; font-size: 12.5px; line-height: 1.5;
  box-shadow: var(--shadow); animation: toast-in .18s ease-out;
  transition: opacity .25s ease, transform .25s ease;
}
.toast.ok { border-left-color: var(--ok); }
.toast.warn { border-left-color: var(--warn); }
.toast.bad { border-left-color: var(--bad); }
.toast.leaving { opacity: 0; transform: translateY(6px); }
@keyframes toast-in { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: none; } }

@keyframes value-flash { from { background: var(--accent-soft); } to { background: transparent; } }
.flash { animation: value-flash .9s ease-out; }

/* ======================= narrow ======================= */
@media (max-width: 760px) {
  .nav { padding: 0 12px; gap: 10px; }
  .brand .sub { display: none; }
  .nav .foot { display: none; }
  .nav a { padding: 12px 10px; font-size: 13px; }
  .main { padding: 14px 12px 36px; }
  .page-head h1 { font-size: 17px; }

  button, .btn, select, input { min-height: 38px; }
  button.xs { min-height: 30px; }
  /* 16px keeps iOS from zooming the page when a field is focused. */
  input[type=search], input[type=text], input[type=password], input[type=number] { font-size: 16px; }

  .box > header { padding: 10px 12px; }
  .box .pad { padding: 12px; }
  .stats { grid-template-columns: 1fr 1fr; }
  .stat { padding: 11px 12px; }
  .stat .v { font-size: 20px; }

  .filter-bar { padding: 10px 12px; }
  .filter-count { flex-basis: 100%; text-align: right; }

  .trend { padding: 4px 12px 0; }
  .trend-svg { min-width: 400px; }
  .trend { overflow-x: auto; }

  #toasts { left: 12px; right: 12px; bottom: 12px; align-items: stretch; max-width: none; }

  /* Stacked cells keep every value readable without horizontal scrolling. */
  table.stack { min-width: 0; }
  table.stack thead { display: none; }
  table.stack tr { display: block; padding: 10px 0; border-bottom: 1px solid var(--line); }
  table.stack td { display: flex; gap: 10px; border: none; padding: 3px 12px; white-space: normal; }
  table.stack td[data-label]::before {
    content: attr(data-label); flex: 0 0 4.5em; color: var(--ink-3); font-size: 11.5px;
  }
  table.stack td.actions { margin-top: 6px; }
}
`

// uiTabsScript wires the page navigation.
//
// It is plain top-level script source (no wrapper) so that showTab is visible to the
// delegated click handler in the page script, which lives inside an IIFE.
//
// Pages are switched by toggling the `hidden` attribute rather than by class names.
// A class-based approach needs a matching `display: none` rule with enough
// specificity, and any other rule that sets `display` silently wins — which is how
// every page ended up visible at once. The attribute is unambiguous.
const uiTabsScript = `
function showTab(id, link) {
  // Accept either the full page id ("view-tasks") or the bare name ("tasks").
  // The two callers disagree — the nav passes what its data attribute holds, and
  // restoreTab passes what was stored — so normalising here removes the chance of a
  // double prefix turning every lookup into a miss.
  id = String(id || '');
  if (id && id.indexOf('view-') !== 0) id = 'view-' + id;

  var pages = document.querySelectorAll('.view');
  var shown = false;
  for (var i = 0; i < pages.length; i++) {
    var match = pages[i].id === id;
    pages[i].hidden = !match;
    if (match) shown = true;
  }
  // Never leave the page blank: if the id matched nothing (a stale stored value, a
  // typo), fall back to the first page rather than hiding everything.
  if (!shown && pages.length) {
    pages[0].hidden = false;
    id = pages[0].id;
  }

  var links = document.querySelectorAll('.nav a[data-view]');
  for (var j = 0; j < links.length; j++) {
    if (links[j].getAttribute('data-view') === id) {
      links[j].classList.add('on');
    } else {
      links[j].classList.remove('on');
    }
  }
  try { localStorage.setItem('workbuddy-panel-view', id); } catch (e) {}
  if (window.scrollY > 0) window.scrollTo(0, 0);
  // The usage chart is only fetched while its page is visible.
  if (id === 'view-usage' && typeof window.refreshUsageTrend === 'function') {
    window.refreshUsageTrend();
  }
}

function restoreTab() {
  var saved = '';
  try { saved = localStorage.getItem('workbuddy-panel-view') || ''; } catch (e) {}

  // Views that were merged away map to the page that absorbed them, so a browser
  // holding an old name lands somewhere sensible instead of on the first page.
  // The keys cover every name this panel has ever used, including the tag-era ones.
  var moved = {
    'view-accounts': 'view-accounts',
    'view-switch': 'view-accounts',
    'view-credits': 'view-accounts',
    'view-checkin': 'view-tasks',
    'view-tasks': 'view-tasks',
    'view-taskscenter': 'view-tasks',
    'view-usage': 'view-usage',
    'view-settings': 'view-settings'
  };
  if (moved[saved]) saved = moved[saved];

  var page = saved ? document.getElementById(saved) : null;
  if (page) {
    showTab(saved, null);
    return;
  }
  var first = document.querySelector('.view');
  if (first) showTab(first.id, null);
}
`
