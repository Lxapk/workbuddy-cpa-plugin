package main

// uiCSS is the panel's stylesheet.
//
// The palette, spacing and component shapes are taken from CPA's management UI
// (CPAMC) so the panel looks like the surface it lives on rather than a separate app
// embedded in it. The tokens below are the host's own: an "极简暖灰" scale, where the
// action colour is a neutral grey rather than a brand colour, success is green and
// both warning and error are the same brick red.
//
// Layout follows the host's tab pattern: a single row of tabs above the content, each
// tab an underline indicator at rest and a darker label when active. Every card
// carries its title and its own actions in its header, with a flexible spacer pushing
// the buttons right — actions were previously collected in a toolbar at the top of a
// page, several blocks away from what they acted on.
//
// Structure:
//
//	tokens      colours, spacing, fonts (dark and light, from the host)
//	shell       page frame and tab bar
//	box         the card, its header, and the header's action slot
//	stats       the summary strip
//	table       data tables
//	controls    buttons, inputs, chips
//	feedback    toasts, empty states, log groups
//	narrow      phone adjustments
const uiCSS = `
/* ======================= tokens ======================= */
/* Values mirror CPAMC's themes.scss. The theme follows the host: it writes
   data-theme="dark" or "white" on its own root, and removes the attribute when the
   user chose "follow system" — in which case the media query decides. */
:root,
:root[data-theme="dark"] {
  color-scheme: dark;
  --bg-secondary: #151412;   /* page background */
  --bg-primary: #1d1b18;     /* card surface */
  --bg-tertiary: #262320;    /* hover / secondary */
  --bg-hover: #2e2a26;
  --bg-quinary: #191714;

  --text-primary: #f6f4f1;
  --text-secondary: #c9c3bb;
  --text-tertiary: #9c958d;
  --text-quaternary: #6f6962;

  --border-color: #3a3530;
  --border-primary: #4a453f;
  --border-hover: #5a544d;

  /* Action colour: a neutral grey, not a brand hue. Buttons in this host are quiet;
     the colour that carries meaning is the status one. */
  --primary-color: #8b8680;
  --primary-hover: #9a948e;
  --primary-active: #a6a099;
  --primary-contrast: #ffffff;

  --success-color: #10b981;
  --warning-color: #c65746;
  --error-color: #c65746;
  --quota-medium-color: #ffd862;

  --shadow: 0 1px 3px 0 rgb(0 0 0 / 0.3);
  --shadow-lg: 0 14px 30px rgba(0, 0, 0, 0.4);

  --radius-md: 8px;
  --radius-full: 9999px;
  --header-height: 56px;

  --mono: ui-monospace, "SF Mono", "Cascadia Mono", "JetBrains Mono", Menlo, Consolas, monospace;
  --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "PingFang SC",
          "Microsoft YaHei", "Helvetica Neue", sans-serif;
}
:root[data-theme="white"],
:root[data-theme="light"] {
  color-scheme: light;
  --bg-secondary: #faf9f5;
  --bg-primary: #f0eee8;
  --bg-tertiary: #e9e6df;
  --bg-hover: #e2dfd7;
  --bg-quinary: #f6f4ee;

  --text-primary: #2d2a26;
  --text-secondary: #6d6760;
  --text-tertiary: #a29c95;
  --text-quaternary: #c0bab3;

  --border-color: #e3e1db;
  --border-primary: #d5d2cb;
  --border-hover: #cecac4;

  --primary-color: #8b8680;
  --primary-hover: #7f7a74;
  --primary-active: #726d67;
  --primary-contrast: #ffffff;

  --success-color: #10b981;
  --warning-color: #c65746;
  --error-color: #c65746;
  --quota-medium-color: #e0aa14;

  --shadow: 0 1px 2px 0 rgb(0 0 0 / 0.08);
  --shadow-lg: 0 10px 18px -3px rgb(0 0 0 / 0.1);

  --radius-md: 8px;
  --radius-full: 9999px;
  --header-height: 56px;

  --mono: ui-monospace, "SF Mono", "Cascadia Mono", "JetBrains Mono", Menlo, Consolas, monospace;
  --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "PingFang SC",
          "Microsoft YaHei", "Helvetica Neue", sans-serif;
}
/* The host's "follow system" case carries no attribute, so the media query decides. */
@media (prefers-color-scheme: light) {
  :root:not([data-theme]) {
    color-scheme: light;
    --bg-secondary: #faf9f5; --bg-primary: #f0eee8; --bg-tertiary: #e9e6df;
    --bg-hover: #e2dfd7; --bg-quinary: #f6f4ee;
    --text-primary: #2d2a26; --text-secondary: #6d6760;
    --text-tertiary: #a29c95; --text-quaternary: #c0bab3;
    --border-color: #e3e1db; --border-primary: #d5d2cb; --border-hover: #cecac4;
    --primary-hover: #7f7a74; --primary-active: #726d67;
    --quota-medium-color: #e0aa14;
    --shadow: 0 1px 2px 0 rgb(0 0 0 / 0.08);
    --shadow-lg: 0 10px 18px -3px rgb(0 0 0 / 0.1);
  }
}

* { box-sizing: border-box; margin: 0; padding: 0; }
/* [hidden] must win over any display rule: the panel switches pages with this
   attribute, and a stray display:flex elsewhere would keep a page visible. */
[hidden] { display: none !important; }
html, body { background: var(--bg-secondary); }
body {
  color: var(--text-primary);
  font: 14px/1.55 var(--sans);
  -webkit-font-smoothing: antialiased;
  min-height: 100vh;
}
a { color: var(--primary-active); text-decoration: none; }
code, .mono { font-family: var(--mono); font-size: .93em; }
::selection { background: color-mix(in srgb, var(--primary-color) 30%, transparent); }
:focus-visible { outline: 2px solid var(--primary-color); outline-offset: -2px; border-radius: 4px; }
@media (prefers-reduced-motion: reduce) { * { transition: none !important; animation: none !important; } }

/* ======================= shell ======================= */
/* One surface throughout: the tab strip sits on the page background rather than on a
   panel of its own colour, so the page reads as a single sheet with a row of tabs. */
.shell { display: block; min-height: 100vh; background: var(--bg-secondary); }

.tabbar {
  position: sticky; top: 0; z-index: 20;
  display: flex; align-items: stretch; gap: 2px;
  padding: 0 18px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
  overflow-x: auto; scrollbar-width: none;
}
.tabbar::-webkit-scrollbar { display: none; }
.tabbar .brand {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 0 16px 0 0; margin-right: 6px;
  border-right: 1px solid var(--border-color);
  white-space: nowrap;
}
.tabbar .brand .name { font-weight: 650; font-size: 14px; letter-spacing: -.01em; }
.tabbar .brand .ver { color: var(--text-tertiary); font-size: 11.5px; font-family: var(--mono); }
/* The tab: an underline indicator, quiet at rest. Taken from the host's own tab
   component — colour and weight change, plus a 2px bar under the active label. */
.tab {
  position: relative; display: inline-flex; align-items: center; gap: 7px; flex-shrink: 0;
  border: 0; background: none; cursor: pointer;
  padding: 14px 12px 13px; border-radius: 8px 8px 0 0;
  font: 550 13px/1 var(--sans); color: var(--text-secondary);
  white-space: nowrap;
  transition: color 200ms ease, background-color 200ms ease;
}
.tab::after {
  content: ''; position: absolute; left: 10px; right: 10px; bottom: -1px;
  height: 2px; border-radius: var(--radius-full); background: transparent;
}
.tab:hover { color: var(--text-primary); background: color-mix(in srgb, var(--bg-tertiary) 55%, transparent); }
.tab.on { color: var(--text-primary); font-weight: 650; }
.tab.on::after { background: var(--text-primary); }

.main { padding: 20px 22px 44px; max-width: 1280px; margin: 0 auto; min-width: 0; }
.page-head { display: flex; align-items: flex-start; gap: 14px; margin-bottom: 18px; }
.page-head h1 { font-size: 19px; font-weight: 650; letter-spacing: -.02em; }
.page-head .sub { color: var(--text-tertiary); font-size: 12.5px; margin-top: 4px; }
.page-head .grow { flex: 1; }

/* ======================= box (card) ======================= */
.box {
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow);
  margin-bottom: 16px; overflow: hidden;
}
/* The header is the card's control strip: title left, actions right, .grow between. */
.box > header {
  display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
  padding: 12px 16px; border-bottom: 1px solid var(--border-color);
}
.box > header h3 { font-size: 13.5px; font-weight: 600; display: flex; align-items: baseline; gap: 7px; flex-wrap: wrap; }
.box > header h3 .hint { font-size: 11.5px; font-weight: 400; color: var(--text-tertiary); }
.box > header .grow { flex: 1; min-width: 0; }
.box > header .note { color: var(--text-tertiary); font-size: 12px; }
.box .pad { padding: 16px; }
.box .foot {
  padding: 10px 16px; border-top: 1px solid var(--border-color);
  color: var(--text-tertiary); font-size: 12px;
}

/* ======================= stats ======================= */
.stats {
  display: grid; grid-template-columns: repeat(auto-fit, minmax(132px, 1fr));
  background: var(--bg-primary); border: 1px solid var(--border-color);
  border-radius: var(--radius-md); box-shadow: var(--shadow);
  overflow: hidden; margin-bottom: 16px;
}
.stat { padding: 13px 15px; border-right: 1px solid var(--border-color); border-bottom: 1px solid var(--border-color); }
.stat .v {
  font: 600 23px/1.15 var(--mono); font-variant-numeric: tabular-nums;
  letter-spacing: -.02em; white-space: nowrap;
}
.stat .k { color: var(--text-tertiary); font-size: 11.5px; margin-top: 4px; }
.stat.good .v { color: var(--success-color); }
.stat.warn .v { color: var(--quota-medium-color); }
.stat.bad .v { color: var(--error-color); }

/* ======================= table ======================= */
/* Tables scroll inside their own wrapper rather than moving the page: the tab bar and
   the frame must stay put while a wide table is scrolled. */
.tbl-wrap { overflow-x: auto; -webkit-overflow-scrolling: touch; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th {
  text-align: left; font-weight: 500; font-size: 11.5px; color: var(--text-tertiary);
  padding: 9px 12px; border-bottom: 1px solid var(--border-color); white-space: nowrap;
}
td { padding: 9px 12px; border-bottom: 1px solid var(--border-color); vertical-align: middle; }
tbody tr:last-child td { border-bottom: none; }
tbody tr:hover { background: color-mix(in srgb, var(--bg-tertiary) 60%, transparent); }
th.num, td.num { text-align: right; font-variant-numeric: tabular-nums; }
td.actions { white-space: nowrap; text-align: right; }

/* ======================= badges ======================= */
.pill {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 2px 9px; border-radius: var(--radius-full);
  font-size: 11.5px; font-weight: 550; border: 1px solid transparent; white-space: nowrap;
}
.pill.ok { background: color-mix(in srgb, var(--success-color) 14%, transparent); color: var(--success-color); }
.pill.warn { background: color-mix(in srgb, var(--quota-medium-color) 16%, transparent); color: var(--quota-medium-color); }
.pill.bad { background: color-mix(in srgb, var(--error-color) 18%, transparent); color: var(--error-color); }
.pill.idle { background: var(--bg-tertiary); color: var(--text-tertiary); }
/* A health strip on the first cell: colour reads faster than text when scanning. */
td.bar { position: relative; padding-left: 16px; }
td.bar::before {
  content: ""; position: absolute; left: 0; top: 8px; bottom: 8px;
  width: 3px; border-radius: 0 2px 2px 0; background: var(--border-color);
}
td.bar.ok::before { background: var(--success-color); }
td.bar.warn::before { background: var(--quota-medium-color); }
td.bar.bad::before { background: var(--error-color); }

/* ======================= controls ======================= */
button, .btn {
  font: 500 13px/1 var(--sans);
  color: var(--text-primary); background: var(--bg-primary);
  border: 1px solid var(--border-primary); border-radius: var(--radius-md);
  padding: 7px 13px; cursor: pointer; white-space: nowrap;
  transition: background-color 200ms ease, border-color 200ms ease, color 200ms ease;
}
button:hover, .btn:hover { background: var(--bg-hover); border-color: var(--border-hover); }
button:active { transform: translateY(.5px); }
button:disabled { opacity: .55; cursor: not-allowed; }
/* The host's primary is the same neutral grey; weight of border does the work. */
button.primary, .btn.primary {
  background: var(--primary-color); border-color: var(--primary-color); color: var(--primary-contrast);
}
button.primary:hover { background: var(--primary-hover); border-color: var(--primary-hover); }
button.danger { color: var(--error-color); border-color: color-mix(in srgb, var(--error-color) 40%, transparent); }
button.danger:hover { background: color-mix(in srgb, var(--error-color) 12%, transparent); border-color: var(--error-color); }
button.ghost { background: transparent; border-color: transparent; }
button.ghost:hover { background: var(--bg-tertiary); border-color: transparent; }
button.xs { padding: 5px 10px; font-size: 12.5px; border-radius: 6px; }
button.ok-btn { background: color-mix(in srgb, var(--success-color) 14%, transparent); color: var(--success-color); border-color: transparent; }
button.ok-btn:hover { border-color: var(--success-color); }
button.idle-btn { background: var(--bg-tertiary); color: var(--text-tertiary); }
button.idle-btn:hover { border-color: var(--border-hover); color: var(--text-primary); }

input, select, textarea {
  font: 13px/1.4 var(--sans); color: var(--text-primary); background: var(--bg-secondary);
  border: 1px solid var(--border-primary); border-radius: var(--radius-md); padding: 7px 11px;
}
input:focus, select:focus, textarea:focus { border-color: var(--primary-color); outline: none; }
input[type=checkbox], input[type=radio] { width: 15px; height: 15px; accent-color: var(--primary-color); }
label.field { display: inline-flex; align-items: center; gap: 7px; color: var(--text-secondary); font-size: 13px; }
label.field input[type=number] { width: 74px; }

.row { display: flex; gap: 9px; align-items: center; flex-wrap: wrap; }
.row.tight { margin-bottom: 8px; }
.grow { flex: 1; min-width: 0; }

/* Radio option cards, used for the routing strategy. */
.opt {
  display: flex; gap: 10px; align-items: flex-start;
  padding: 11px 13px; margin-bottom: 8px;
  border: 1px solid var(--border-color); border-radius: var(--radius-md);
  background: var(--bg-secondary); cursor: pointer;
  transition: border-color 200ms ease, background-color 200ms ease;
}
.opt:hover { border-color: var(--border-hover); background: var(--bg-tertiary); }
.opt input { margin-top: 3px; }
.opt .name { font-weight: 550; font-size: 13px; }
.opt .desc { color: var(--text-tertiary); font-size: 12px; }

/* Segmented control. */
.seg {
  display: inline-flex; border: 1px solid var(--border-primary);
  border-radius: var(--radius-md); overflow: hidden; background: var(--bg-secondary);
}
.seg button { border: none; border-radius: 0; background: transparent; }
.seg button.on { background: var(--primary-color); color: var(--primary-contrast); }
.seg button + button { border-left: 1px solid var(--border-primary); }

/* ======================= filter bar ======================= */
.filter-bar {
  display: flex; align-items: center; gap: 8px; padding: 11px 16px; flex-wrap: wrap;
  border-bottom: 1px solid var(--border-color);
}
.filter-search { position: relative; display: flex; align-items: center; flex: 1 1 200px; min-width: 0; }
.filter-search .filter-icon { position: absolute; left: 11px; color: var(--text-tertiary); pointer-events: none; }
.filter-search input[type=search] { width: 100%; padding-left: 32px; padding-right: 30px; }
.filter-search input[type=search]::-webkit-search-decoration,
.filter-search input[type=search]::-webkit-search-cancel-button { -webkit-appearance: none; appearance: none; }
.filter-clear {
  position: absolute; right: 5px; width: 22px; height: 22px; padding: 0; border: none;
  display: inline-flex; align-items: center; justify-content: center;
  background: transparent; color: var(--text-tertiary); border-radius: 50%;
}
.filter-clear:hover { background: var(--bg-tertiary); color: var(--text-primary); }
.filter-count { color: var(--text-tertiary); font-size: 12px; }

/* ======================= chart ======================= */
.trend { padding: 4px 16px 0; color: var(--text-secondary); }
.trend-svg { display: block; width: 100%; height: auto; max-height: 190px; }
.trend-ok { fill: var(--success-color); }
.trend-bad { fill: var(--error-color); }
.trend-legend { display: flex; gap: 14px; align-items: center; padding: 8px 16px 14px; color: var(--text-tertiary); font-size: 12px; flex-wrap: wrap; }
.trend-legend i.sw { display: inline-block; width: 9px; height: 9px; border-radius: 2px; margin-right: 5px; }
.trend-legend .sw-ok { background: var(--success-color); }
.trend-legend .sw-bad { background: var(--error-color); }

/* ======================= log groups ======================= */
i.mark {
  display: inline-block; width: 10px; height: 10px; flex: 0 0 10px;
  border-radius: 50%; border: 1.5px solid currentColor; box-sizing: border-box;
}
i.mark.ok { border-color: var(--success-color); background: var(--success-color); }
i.mark.skip { border-color: var(--text-tertiary); }
i.mark.err { border-color: var(--error-color); background: var(--error-color); }
i.mark.info { border-color: var(--primary-color); }
details.log-group {
  border: 1px solid var(--border-color); border-radius: var(--radius-md);
  margin-bottom: 8px; background: var(--bg-secondary); overflow: hidden;
}
details.log-group > summary {
  display: flex; align-items: center; gap: 8px; padding: 9px 12px;
  cursor: pointer; font-size: 13px; list-style: none;
}
details.log-group > summary::-webkit-details-marker { display: none; }
details.log-group > summary::after { content: '▾'; margin-left: auto; opacity: .5; }
details.log-group[open] > summary::after { transform: rotate(180deg); }
details.log-group .count { color: var(--text-tertiary); font-size: 12px; }
.log-body { padding: 2px 12px 10px; }
.log-entry { display: flex; gap: 8px; align-items: flex-start; padding: 5px 0; font-size: 12.5px; line-height: 1.55; }
.log-entry i.mark { margin-top: 4px; }
.log-text { word-break: break-word; }
.log-scroll { max-height: 320px; overflow-y: auto; }

/* ======================= misc ======================= */
.empty { padding: 24px 16px; color: var(--text-tertiary); font-size: 13px; text-align: center; }
.note { color: var(--text-tertiary); font-size: 12px; line-height: 1.6; }
.legend { display: flex; flex-wrap: wrap; gap: 14px; color: var(--text-tertiary); font-size: 12px; }
.legend span { display: inline-flex; align-items: center; gap: 6px; }
details > summary { cursor: pointer; }

#toasts {
  position: fixed; right: 16px; bottom: 16px; z-index: 60;
  display: flex; flex-direction: column; gap: 8px; align-items: flex-end;
  pointer-events: none; max-width: min(420px, calc(100vw - 32px));
}
.toast {
  pointer-events: auto; background: var(--bg-primary); color: var(--text-primary);
  border: 1px solid var(--border-color); border-left: 3px solid var(--primary-color);
  border-radius: var(--radius-md); padding: 10px 13px; font-size: 12.5px; line-height: 1.5;
  box-shadow: var(--shadow-lg); animation: toast-in .18s ease-out;
  transition: opacity .25s ease, transform .25s ease;
}
.toast.ok { border-left-color: var(--success-color); }
.toast.warn { border-left-color: var(--quota-medium-color); }
.toast.bad { border-left-color: var(--error-color); }
.toast.leaving { opacity: 0; transform: translateY(6px); }
@keyframes toast-in { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: none; } }

@keyframes value-flash { from { background: color-mix(in srgb, var(--success-color) 18%, transparent); } to { background: transparent; } }
.flash { animation: value-flash .9s ease-out; }

/* ======================= narrow ======================= */
@media (max-width: 768px) {
  /* The host's floating controls sit over the frame's top-right corner; on a phone
     they are wider relative to the viewport, so the tab strip gets extra clearance. */
  .tabbar { padding: 8px 12px 0; }
  .tabbar .brand { display: none; }
  .tab { padding: 12px 11px 11px; font-size: 13px; }

  .main { padding: 14px 12px 36px; }
  .page-head { margin-bottom: 14px; }
  .page-head h1 { font-size: 17px; }
  .page-head .sub { font-size: 12px; }

  /* Touch targets: 44px is the accepted minimum, desktop sizes land near 30px. */
  button, .btn, select,
  input[type=text], input[type=password], input[type=number], input[type=search] { min-height: 40px; }
  button.xs { min-height: 34px; padding: 7px 11px; }
  /* 16px stops iOS from zooming the page when a field is focused, which would leave
     the viewport scaled up afterwards. */
  input[type=search], input[type=text], input[type=password], input[type=number] { font-size: 16px; }
  input[type=checkbox], input[type=radio] { width: 18px; height: 18px; }

  .box { margin-bottom: 14px; }
  .box > header { padding: 11px 12px; gap: 8px; }
  .box .pad { padding: 12px; }
  .box .foot { padding: 9px 12px; }

  .stats { grid-template-columns: 1fr 1fr; margin-bottom: 14px; }
  .stat { padding: 11px 12px; }
  .stat .v { font-size: 20px; }
  .stat .k { font-size: 11px; }

  /* Stacked cells keep every value readable without horizontal scrolling. */
  table.stack thead { display: none; }
  table.stack tr { display: block; padding: 11px 0; border-bottom: 1px solid var(--border-color); }
  table.stack tbody tr:last-child { border-bottom: none; }
  table.stack td { display: flex; align-items: baseline; gap: 10px; padding: 3px 12px; border: none; white-space: normal; }
  table.stack td[data-label]::before {
    content: attr(data-label); flex: 0 0 4.5em; color: var(--text-tertiary); font-size: 11.5px;
  }
  /* The account cell heads the stacked block, so it drops the label. */
  table.stack td[data-label="账号"] { padding-bottom: 6px; }
  table.stack td[data-label="账号"]::before { display: none; }
  table.stack td[data-label="账号"] strong { font-size: 14.5px; }
  table.stack td.actions { margin-top: 6px; }

  .filter-bar { padding: 10px 12px; }
  .filter-search { flex: 1 1 100%; }
  .filter-count { flex-basis: 100%; text-align: right; }

  .trend { padding: 4px 0 0; overflow-x: auto; }
  .trend-svg { min-width: 420px; }
  .trend-legend { padding: 8px 12px 12px; }

  .opt { padding: 11px 12px; }
  .seg { flex-wrap: wrap; }
  .seg button { border-radius: 6px !important; border: 1px solid var(--border-primary); }
  .seg button + button { border-left: 1px solid var(--border-primary); }

  /* Toasts span the width instead of hanging off the right edge, where a long message
     would be clipped. */
  #toasts { left: 12px; right: 12px; bottom: 12px; align-items: stretch; max-width: none; }

  /* The bounded log height is a desktop convenience; a phone viewport is already
     short, so let the page scroll rather than nesting a second scroller. */
  .log-scroll { max-height: none; overflow: visible; }
}
`

// uiTabsScript wires the tab strip.
//
// Plain top-level script source (no wrapper) so that showTab is visible to the
// delegated click handler in the page script, which lives inside an IIFE.
//
// Pages are switched by toggling the `hidden` attribute rather than by class names. A
// class-based approach needs a matching `display: none` rule with enough specificity,
// and any other rule that sets `display` silently wins — which is how every page once
// ended up visible at the same time. The attribute is unambiguous.
const uiTabsScript = `
// adoptHostTheme mirrors the host's theme onto this document.
//
// The panel is rendered in an iframe by CPA's management UI, which marks its own
// <html> with data-theme="dark" / "white", or removes it when the user chose "follow
// system". Reading that attribute keeps an explicit choice in sync; when it is absent
// (or the frame is cross-origin, where the read throws) the stylesheet's
// prefers-color-scheme rules take over, which is exactly what "follow system" means.
//
// Re-checked on a timer as well as at load: the host swaps the attribute without
// reloading the iframe, and the panel would otherwise keep the theme it started with.
function adoptHostTheme() {
  var hostTheme = '';
  try {
    if (window.parent && window.parent !== window && window.parent.document) {
      var hostRoot = window.parent.document.documentElement;
      hostTheme = hostRoot.getAttribute('data-theme') || '';
    }
  } catch (e) {
    // Cross-origin: the attribute is unreachable by design. Media queries cover it.
    hostTheme = '';
  }

  var root = document.documentElement;
  if (hostTheme === 'dark' || hostTheme === 'white') {
    root.setAttribute('data-theme', hostTheme);
  } else {
    root.removeAttribute('data-theme');
  }
}

adoptHostTheme();
setInterval(adoptHostTheme, 3000);

function showTab(id, tab) {
  // Accept either the full page id ("view-tasks") or the bare name ("tasks"). The two
  // callers disagree — the tab strip passes what its data attribute holds, and
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

  var tabs = document.querySelectorAll('.tabbar .tab[data-view]');
  for (var j = 0; j < tabs.length; j++) {
    if (tabs[j].getAttribute('data-view') === id) {
      tabs[j].classList.add('on');
    } else {
      tabs[j].classList.remove('on');
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
  // The keys cover every name this panel has ever used, including the tab-era ones.
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
