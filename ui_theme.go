package main

// This file holds the shared visual language for the plugin's management pages.
//
// The look follows the credit-manager plug-in the operator asked to match:
// a soft green accent, white cards on a very light background, 12-14px radii,
// low-contrast borders and a restrained shadow. Rather than repeat the palette
// in each page's <style> block, it lives here and every page embeds it, so a
// change lands everywhere at once.
//
// Two constraints shape the markup:
//
//   - the page is rendered inside CPA's own panel, so it must not assume a
//     fixed viewport or try to take over the whole screen
//   - no <form> elements: CPA serves resource routes with GET only, so all
//     actions go through fetch() with the management key from localStorage
const uiCSS = `
:root {
  color-scheme: light dark;
  --accent: #1eb787;
  --accent-dark: #14926b;
  --accent-soft: #e4f7ef;
  --accent-2: #7968ee;
  --purple-soft: #efedff;
  --warn: #e5a12d;
  --warn-soft: #fdf3e2;
  --ok: #1eb787;
  --ok-soft: #e4f7ef;
  --danger: #d95c51;
  --danger-soft: #fdf0ee;
  --bg: #fafbf8;
  --card-bg: #ffffff;
  --panel-2: #f7f9f6;
  --line: #e7ebe5;
  --text: #1a1f1c;
  --muted: #5f6a61;
  --shadow: 0 8px 28px rgba(41, 57, 46, .07);
  --shadow-sm: 0 4px 15px rgba(41, 57, 46, .04);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans SC", system-ui, sans-serif;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #14171a;
    --card-bg: #1b1f23;
    --panel-2: #202428;
    --line: #2c3237;
    --text: #e6eae6;
    --muted: #9aa39c;
    --accent-soft: #14322a;
    --purple-soft: #232034;
    --warn-soft: #33291a;
    --danger-soft: #33211f;
    --shadow: 0 8px 28px rgba(0, 0, 0, .35);
    --shadow-sm: 0 4px 15px rgba(0, 0, 0, .25);
  }
}
* { box-sizing: border-box; }
body {
  margin: 0;
  padding: 0;
  background: var(--bg);
  color: var(--text);
  font-size: 14px;
  line-height: 1.6;
}
.root {
  max-width: 1180px;
  margin: 0 auto;
  padding: 22px 20px 44px;
  background:
    radial-gradient(680px 260px at 40% -10%, rgba(90, 219, 176, .14), transparent 72%),
    radial-gradient(560px 260px at 100% 6%, rgba(121, 104, 238, .06), transparent 76%);
}
/* ---------- header ---------- */
.hero { display: flex; flex-wrap: wrap; gap: 14px; justify-content: space-between; align-items: flex-end; margin: 4px 0 18px; }
.hero h1 { margin: 0; font-size: 1.55rem; line-height: 1.15; letter-spacing: -.035em; font-weight: 750; }
.hero h1::before { content: ""; display: inline-block; width: 8px; height: 8px; margin: 0 9px 4px 0; background: var(--accent); border-radius: 50%; }
.hero .sub { color: var(--muted); margin-top: 6px; font-size: .84rem; }
.hero .badge {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 5px 11px; border-radius: 999px;
  background: var(--accent-soft); color: var(--accent-dark);
  font-size: .74rem; font-weight: 700;
}
/* ---------- tabs ---------- */
.tabs { display: flex; flex-wrap: wrap; gap: 6px; margin: 0 0 18px; border-bottom: 1px solid var(--line); padding-bottom: 0; }
.tabs button {
  border: 0; background: transparent; color: var(--muted);
  padding: 9px 14px; border-radius: 9px 9px 0 0;
  font: inherit; font-size: .86rem; font-weight: 650; cursor: pointer;
  border-bottom: 2px solid transparent; margin-bottom: -1px;
}
.tabs button:hover { color: var(--text); background: var(--panel-2); }
.tabs button.active { color: var(--accent-dark); border-bottom-color: var(--accent); background: var(--accent-soft); }
.panel { display: none; }
.panel.active { display: block; }
/* ---------- cards ---------- */
.card {
  background: var(--card-bg);
  border: 1px solid var(--line);
  border-radius: 14px;
  padding: 16px 18px;
  box-shadow: var(--shadow-sm);
  margin-bottom: 14px;
}
.card > h2 {
  margin: 0 0 12px; font-size: .95rem; font-weight: 720;
  display: flex; align-items: center; gap: 8px;
}
.card > h2 .hint { color: var(--muted); font-weight: 500; font-size: .78rem; }
.grid { display: grid; gap: 12px; }
.grid.stats { grid-template-columns: repeat(auto-fit, minmax(148px, 1fr)); }
.stat {
  position: relative; overflow: hidden;
  background: linear-gradient(135deg, var(--card-bg), var(--panel-2));
  border: 1px solid var(--line); border-radius: 12px;
  padding: 13px 15px; box-shadow: var(--shadow-sm);
}
.stat::before { content: ""; position: absolute; top: 12px; left: 15px; width: 16px; height: 2px; border-radius: 2px; background: var(--accent); }
.stat:nth-child(2n)::before { background: var(--accent-2); }
.stat:nth-child(3n)::before { background: var(--warn); }
.stat .k { color: var(--muted); font-size: .75rem; margin-top: 8px; }
.stat .v { font-size: 1.32rem; font-weight: 730; letter-spacing: -.025em; margin-top: 3px; font-variant-numeric: tabular-nums; word-break: break-all; }
/* ---------- controls ---------- */
.row { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin: 8px 0; }
.row.tight { margin: 5px 0; }
label.field { display: inline-flex; align-items: center; gap: 7px; color: var(--muted); font-size: .82rem; }
input[type=password], input[type=text], input[type=number] {
  border: 1px solid var(--line); border-radius: 9px;
  background: var(--panel-2); color: var(--text);
  padding: 8px 11px; font: inherit; font-size: .86rem; outline: 0;
}
input[type=password], input[type=text] { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
input:focus { border-color: #76d7b8; box-shadow: 0 0 0 3px rgba(30, 183, 135, .13); background: var(--card-bg); }
input[type=number] { width: 5em; }
input[type=checkbox], input[type=radio] { accent-color: var(--accent); width: 15px; height: 15px; }
button {
  border: 0; border-radius: 9px; padding: 9px 15px;
  background: var(--accent); color: #fff;
  font: inherit; font-size: .85rem; font-weight: 700; cursor: pointer;
}
button:hover { background: var(--accent-dark); }
button:disabled { cursor: wait; opacity: .55; }
button.ghost { background: var(--panel-2); color: var(--text); border: 1px solid var(--line); }
button.ghost:hover { background: var(--accent-soft); color: var(--accent-dark); }
.opt { display: flex; align-items: flex-start; gap: 9px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; margin: 7px 0; cursor: pointer; background: var(--panel-2); }
.opt:hover { border-color: var(--accent); background: var(--accent-soft); }
.opt input { margin-top: 3px; }
.opt .name { font-weight: 680; }
.opt .desc { color: var(--muted); font-size: .79rem; }
/* Small labelled buttons for table rows.
   Short text ("启用"/"签到"/"积分" are two characters each) keeps the action column
   narrow enough for a phone without resorting to icons. */
button.mini {
  padding: 4px 9px; font-size: .76rem;
  border-radius: 7px;
  white-space: nowrap;
}
button.mini + button.mini { margin-left: 4px; }
td.actions { white-space: nowrap; padding-right: 12px; }
/* ---------- filter bar ---------- */
/* A single row that fills its card: the search input takes the flexible width and
   the select sizes to its content, so there is no gap left on the right. */
.filter-bar {
  display: flex; align-items: center; gap: 8px;
  margin: .1rem 0 .7rem; flex-wrap: wrap;
}
.filter-search {
  position: relative; display: flex; align-items: center;
  flex: 1 1 200px; min-width: 0;
}
.filter-search .filter-icon {
  position: absolute; left: 10px; color: var(--muted);
  pointer-events: none;
}
.filter-search input[type=search] {
  width: 100%; box-sizing: border-box;
  /* Left padding clears the icon; right padding clears the clear button. */
  padding-left: 31px; padding-right: 30px;
  border-radius: 10px;
}
/* The platform's own decorations duplicate the clear button and look foreign. */
.filter-search input[type=search]::-webkit-search-decoration,
.filter-search input[type=search]::-webkit-search-cancel-button { -webkit-appearance: none; appearance: none; }
.filter-clear {
  position: absolute; right: 5px;
  width: 22px !important; height: 22px; min-height: 0 !important;
  padding: 0 !important;
  display: inline-flex; align-items: center; justify-content: center;
  border: none; background: transparent; color: var(--muted);
  border-radius: 50%; cursor: pointer;
}
.filter-clear:hover { background: var(--line); color: var(--text); }
.filter-select {
  flex: 0 0 auto;
  border: 1px solid var(--line); border-radius: 10px;
  background: var(--panel-2) url("data:image/svg+xml;charset=utf-8,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 12 12'%3E%3Cpath d='M3 4.8l3 3 3-3' fill='none' stroke='%235f6a61' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") no-repeat right 9px center / 12px 12px;
  color: var(--text); font-size: .82rem;
  padding: 8px 28px 8px 11px;
  -webkit-appearance: none; appearance: none;
  cursor: pointer;
}
.filter-select:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
.filter-count { flex: 0 0 auto; margin-left: auto; }
@media (max-width: 720px) {
  /* The count moves under the controls rather than squeezing the input. */
  .filter-count { margin-left: 0; flex-basis: 100%; text-align: right; }
}
/* ---------- table ---------- */
/* Tables get their own horizontal scroll container.
   A six-column account table (plus three action buttons) cannot fit a 360px
   viewport no matter how it is typeset, and letting the page itself scroll
   sideways moves the header and the tab bar out of view along with it. Scoping the
   overflow to the table keeps the page chrome in place and lets the operator swipe
   just the rows. */
.table-wrap {
  width: 100%;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
  /* A hairline shadow hints that there is more to the right without stealing
     space from the content. */
  background:
    linear-gradient(to right, var(--card-bg) 30%, rgba(0, 0, 0, 0)) left / 24px 100% no-repeat,
    linear-gradient(to left, var(--card-bg) 30%, rgba(0, 0, 0, 0)) right / 24px 100% no-repeat;
  background-attachment: local, local;
}
table { width: 100%; border-collapse: collapse; font-size: .84rem; }
/* Cells keep their content on one line: wrapping a uid across three lines makes
   the row taller than the fold and harder to scan than a swipe. */
th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--line); vertical-align: middle; white-space: nowrap; }
th { color: var(--muted); font-weight: 640; font-size: .77rem; }
tbody tr:hover { background: var(--panel-2); }
td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: .79rem; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: .78rem; }
/* ---------- status ---------- */
.pill { display: inline-flex; align-items: center; gap: 5px; padding: 2px 9px; border-radius: 999px; font-size: .75rem; font-weight: 660; }
.pill.ok { background: var(--accent-soft); color: var(--accent-dark); }
.pill.warn { background: var(--warn-soft); color: #a9721a; }
.pill.bad { background: var(--danger-soft); color: var(--danger); }
.pill.idle { background: var(--panel-2); color: var(--muted); }
.dot { width: 7px; height: 7px; border-radius: 50%; display: inline-block; background: currentColor; }
.muted { color: var(--muted); }
.small { font-size: .78rem; }
.ok { color: var(--accent-dark); }
.warn { color: #b8801f; }
.bad { color: var(--danger); }
.note { margin: 8px 0 0; color: var(--muted); font-size: .78rem; line-height: 1.55; }
.empty { padding: 18px 4px; color: var(--muted); font-size: .84rem; }
/* ---------- run log ---------- */
pre.log {
  margin: 8px 0 0; padding: 11px 13px; max-height: 340px; overflow: auto;
  background: var(--panel-2); border: 1px solid var(--line); border-radius: 9px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: .76rem; line-height: 1.6; white-space: pre-wrap; word-break: break-word;
}
details > summary { cursor: pointer; margin-top: 8px; }
/* ---------- growth run result ---------- */
/* Marks are drawn in CSS rather than as font glyphs or images: the panel ships no
   assets, and a text glyph renders differently on every platform. */
i.mark {
  display: inline-block; width: 12px; height: 12px; flex: 0 0 12px;
  border-radius: 50%; vertical-align: -1px;
  border: 1.5px solid currentColor; box-sizing: border-box;
}
i.mark.ok { border-color: var(--ok); background: var(--ok); }
i.mark.skip { border-color: var(--muted); background: transparent; }
i.mark.err { border-color: var(--danger); background: var(--danger); }
i.mark.info { border-color: var(--accent); background: transparent; }
.legend { display: flex; flex-wrap: wrap; gap: 14px; margin: 8px 0 4px; color: var(--muted); font-size: .78rem; }
.legend span { display: inline-flex; align-items: center; gap: 6px; }
details.log-group {
  border: 1px solid var(--line); border-radius: 9px;
  margin-top: 8px; background: var(--panel-2); overflow: hidden;
}
details.log-group > summary {
  display: flex; align-items: center; gap: 8px;
  padding: 9px 12px; cursor: pointer; font-size: .84rem;
  list-style: none;
}
details.log-group > summary::-webkit-details-marker { display: none; }
details.log-group > summary::after {
  content: '▾'; margin-left: auto; opacity: .55; transition: transform .15s ease;
}
details.log-group[open] > summary::after { transform: rotate(180deg); }
details.log-group > summary:hover { background: var(--accent-soft); }
details.log-group .count { margin-left: auto; color: var(--muted); font-size: .76rem; }
details.log-group .count + * { margin-left: 0; }
.log-body { padding: 2px 12px 10px; }
.log-entry { display: flex; gap: 8px; align-items: flex-start; padding: 5px 0; font-size: .8rem; line-height: 1.55; }
.log-entry i.mark { margin-top: 4px; }
.log-text { word-break: break-word; }
/* Bounded height with its own scrollbar: a run where one missing prerequisite
   blocks every task produces one entry per task, and unbounded they push the
   summary out of view. */
.log-scroll { max-height: 320px; overflow-y: auto; }
.log-scroll::-webkit-scrollbar { width: 8px; }
.log-scroll::-webkit-scrollbar-thumb { background: var(--line); border-radius: 4px; }
/* A value that just changed gets a brief highlight. Without it an updated number
   that happens to look the same gives no feedback that the action did anything. */
@keyframes value-flash {
  from { background: var(--accent-soft); }
  to { background: transparent; }
}
.flash { animation: value-flash .9s ease-out; }
@media (prefers-reduced-motion: reduce) {
  .flash { animation: none; background: var(--accent-soft); }
}
/* ---------- usage trend ---------- */
.trend { padding: 4px 0 0; color: var(--text); }
.trend-svg { display: block; width: 100%; height: auto; max-height: 200px; }
.trend-ok { fill: var(--ok); }
.trend-bad { fill: var(--danger); }
.trend-legend { display: flex; gap: 14px; align-items: center; margin-top: 8px; }
.trend-legend i.sw {
  display: inline-block; width: 9px; height: 9px; border-radius: 2px;
  margin-right: 5px; vertical-align: -1px;
}
.trend-legend .sw-ok { background: var(--ok); }
.trend-legend .sw-bad { background: var(--danger); }
.trend-note { margin-left: auto; opacity: .75; }
/* ---------- toasts ---------- */
/* Fixed to the corner so a notice never reflows the page or moves the control the
   operator is about to click again. */
#toasts {
  position: fixed; right: 16px; bottom: 16px; z-index: 60;
  display: flex; flex-direction: column; gap: 8px; align-items: flex-end;
  pointer-events: none; max-width: min(420px, calc(100vw - 32px));
}
.toast {
  pointer-events: auto;
  background: var(--panel); color: var(--text);
  border: 1px solid var(--line); border-left: 3px solid var(--accent);
  border-radius: 9px; padding: 9px 13px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, .18);
  font-size: .82rem; line-height: 1.5;
  animation: toast-in .18s ease-out;
  transition: opacity .25s ease, transform .25s ease;
}
.toast.ok { border-left-color: var(--ok); }
.toast.warn { border-left-color: var(--warn); }
.toast.bad { border-left-color: var(--danger); }
.toast.leaving { opacity: 0; transform: translateY(6px); }
@keyframes toast-in { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: none; } }
@media (prefers-reduced-motion: reduce) {
  .toast { animation: none; transition: none; }
}
/* ---------- panels/radio groups ---------- */
.seg { display: inline-flex; border: 1px solid var(--line); border-radius: 9px; overflow: hidden; background: var(--panel-2); }
.seg button { border-radius: 0; background: transparent; color: var(--text); font-weight: 620; padding: 7px 13px; }
.seg button + button { border-left: 1px solid var(--line); }
.seg button.active { background: var(--accent); color: #fff; }

/* ---------- narrow viewports (phones) ---------- */
/* One breakpoint at 720px covers the whole phone range in both orientations: a
   landscape phone is ~740 CSS px, so pushing it past this would leave the widest
   common case unstyled. */
@media (max-width: 720px) {
  .root { padding: 14px 12px 32px; }

  /* Touch targets.
     A finger needs roughly 44px; the desktop sizes here were tuned for a mouse and
     land around 30px. Raising the floor rather than every rule keeps the visual
     hierarchy intact. */
  button, .seg button, input[type=button], select {
    min-height: 42px;
    padding-top: 9px;
    padding-bottom: 9px;
  }
  input[type=password], input[type=text], input[type=number] { min-height: 42px; font-size: 16px; }
  /* 16px avoids the automatic zoom Safari applies to smaller inputs, which would
     leave the page zoomed in after the operator taps a field. */
  input[type=checkbox], input[type=radio] { width: 18px; height: 18px; }

  /* Rows of controls stack instead of being squeezed.
     justify-content: space-between would leave a lone button floating right. */
  .row { gap: 8px; }
  .row > button, .row > .seg { flex: 1 1 auto; }
  .row .seg { display: flex; }
  .row .seg button { flex: 1 1 0; }

  /* Cards lose their generous padding: on a 360px screen the 20px inside each
     edge is over 10% of the width spent on margins. */
  .card { padding: 13px 12px; border-radius: 11px; }
  .card > h2 { font-size: .95rem; }

  /* Stats pair up two-per-row rather than one, so the summary stays above the
     fold. */
  .grid.stats { grid-template-columns: repeat(auto-fit, minmax(132px, 1fr)); gap: 9px; }
  .stat { padding: 10px 12px; }

  /* Tabs scroll sideways rather than wrapping into two rows. */
  .tabs { overflow-x: auto; -webkit-overflow-scrolling: touch; flex-wrap: nowrap; }
  .tabs button { white-space: nowrap; flex: 0 0 auto; }

  /* The chart keeps a readable minimum and scrolls if the container is narrower:
     squeezing seven bars into 320px makes each one a sliver. */
  .trend-svg { min-width: 420px; }
  .trend { overflow-x: auto; -webkit-overflow-scrolling: touch; }
  .trend-legend { flex-wrap: wrap; gap: 8px 12px; }
  .trend-note { margin-left: 0; flex-basis: 100%; }

  /* Toasts span the width instead of hanging off the right edge, where a long
     message would be clipped. */
  #toasts { left: 12px; right: 12px; bottom: 12px; align-items: stretch; max-width: none; }
  .toast { padding: 10px 13px; }

  /* The growth log's bounded height is a desktop convenience; on a phone the
     viewport is already short, so let the page scroll instead of nesting one. */
  .log-scroll { max-height: none; overflow: visible; }
  .log-entry { font-size: .82rem; }
}

/* The account table's action cell.
   Three buttons (启用/签到/积分, ~250px) cannot fit the ~90px left to them in the
   last column of a 360px screen; letting them be clipped means only the first is
   reachable and the other two are invisible until the operator swipes the table
   sideways. Below the breakpoint the cell becomes a full-width row of equally
   sized buttons, so all three are tappable without any horizontal scrolling.

   display:block on the <td> is what lets it span: a table cell normally sizes to
   its column, and width:100% means "100% of the column", not of the table. Taking
   it out of the table layout entirely is the reliable way. The trade-off is that
   the row no longer aligns to the columns above it — acceptable, because on a
   phone the buttons are the only interactive thing in the row and alignment
   matters less than reachability. */
@media (max-width: 720px) {
  /* Below the breakpoint the table stops shrinking gracefully.
     A six-column table does not fit a phone; the page must not scroll sideways
     because that moves the tab bar and header out of view, so the overflow is
     scoped to the table itself. The rows stay rows — a per-field stacked layout
     makes the list taller than it is informative, and an operator scanning for one
     account wants the whole row on one line. */
  .table-wrap { margin: 0 -12px; padding: 0 12px; }
  table { font-size: .8rem; }
  th, td { padding: 7px 8px; }

  /* Compact labelled buttons keep the action column narrow enough to fit. */
  button.mini {
    min-width: 0; min-height: 32px;
    padding: 5px 8px !important;
    font-size: .74rem;
  }
  button.mini + button.mini { margin-left: 5px; }
  td.actions { padding-right: 12px; white-space: nowrap; }
}

/* Very narrow (iPhone SE and similar). Below this the two-column stat grid stops
   being useful and the header needs to stack. */
@media (max-width: 400px) {
  .grid.stats { grid-template-columns: 1fr 1fr; }
  .hero { gap: 10px; }
  .hero h1 { font-size: 1.15rem; }
  .seg { flex-wrap: wrap; }
  .seg button { border-radius: 8px !important; border: 1px solid var(--line); }
  .seg button + button { border-left: 1px solid var(--line); }
}
`

// uiTabsScript wires the tab bar. It is plain top-level script source (no
// <script> wrapper) so the page can concatenate it with the rest of its
// JavaScript into a single block — two separate blocks would still work, but a
// single one keeps function order obvious and avoids a redundant tag.
const uiTabsScript = `
function showTab(id, btn) {
  var root = document;
  root.querySelectorAll('.panel').forEach(function (p) { p.classList.remove('active'); });
  root.querySelectorAll('.tabs button').forEach(function (b) { b.classList.remove('active'); });
  var panel = document.getElementById(id);
  if (panel) panel.classList.add('active');
  if (btn) btn.classList.add('active');
  try { localStorage.setItem('workbuddy-panel-tab', id); } catch (e) {}
  // The usage tab's chart is only fetched while it is visible, so switching to it
  // has to ask for the data rather than waiting for the next tick.
  //
  // The hook is looked up as a property of window, not as a bare identifier:
  // showTab is top-level but the fetch lives inside the page's IIFE, so a bare
  // reference would resolve to undefined and the chart would stay on its loading
  // placeholder forever.
  if (id === 'tab-usage' && typeof window.refreshUsageTrend === 'function') {
    window.refreshUsageTrend();
  }
}
function restoreTab() {
  var saved = '';
  try { saved = localStorage.getItem('workbuddy-panel-tab') || ''; } catch (e) {}

  // 签到不再是独立标签页，它并入了任务页。存过这个 id 的浏览器要落到任务页，
  // 而不是被当成失效值丢回第一个标签——那会让「上次看的是签到」变成「回了账号页」。
  if (saved === 'tab-checkin') saved = 'tab-tasks';

  if (saved) {
    var panel = document.getElementById(saved);
    var btn = document.querySelector('.tabs button[data-tab="' + saved + '"]');
    if (panel && btn) { showTab(saved, btn); return; }
  }
  var first = document.querySelector('.tabs button');
  if (first) first.click();
}
`
