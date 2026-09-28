package main

import "strings"

// mainPageScript returns the browser-side JavaScript for the combined page.
//
// The key handling is deliberate: CPA's management endpoints authenticate from a
// request header and the resource route is GET-only, so the key lives in
// localStorage and is attached by fetch(). It never reaches the plugin.
func mainPageScript() string {
	const script = `<script>
{{UI_TABS}}
(function () {
  var KEY_NAME = '{{KEY_NAME}}';
  var MGMT = '{{MGMT}}';
  var BASE = '{{BASE}}';

  function key() {
    try { return localStorage.getItem(KEY_NAME) || ''; } catch (e) { return ''; }
  }

  function setKeyState(msg, cls) {
    var el = document.getElementById('keyState');
    if (!el) return;
    el.textContent = msg || '';
    el.className = 'muted small ' + (cls || '');
  }

  function refreshKeyState() {
    var k = key();
    if (k) setKeyState('已保存密钥（' + k.length + ' 字符），操作可直接使用。', 'ok');
    else setKeyState('尚未保存密钥，涉及数据的操作会提示缺少管理密钥。', 'warn');
  }

  window.saveKey = function () {
    var input = document.getElementById('mgmtKey');
    var v = (input && input.value || '').trim();
    if (!v) { setKeyState('请输入密钥', 'bad'); return; }
    try { localStorage.setItem(KEY_NAME, v); } catch (e) {
      setKeyState('浏览器拒绝保存（可能禁用了 localStorage）', 'bad'); return;
    }
    if (input) input.value = '';
    refreshKeyState();
  };

  window.clearKey = function () {
    try { localStorage.removeItem(KEY_NAME); } catch (e) {}
    refreshKeyState();
  };

  function call(path, options) {
    var k = key();
    if (!k) { return Promise.reject(new Error('请先在「设置」里保存管理密钥')); }
    var opts = options || {};
    opts.headers = Object.assign({
      'Authorization': 'Bearer ' + k,
      'X-Management-Key': k
    }, opts.headers || {});
    return fetch(path, opts).then(function (resp) {
      return resp.text().then(function (body) {
        var data = null;
        try { data = JSON.parse(body); } catch (e) {}
        if (!resp.ok) {
          var msg = (data && (data.error || data.message)) || ('HTTP ' + resp.status);
          if (String(msg).indexOf('management key') >= 0) {
            msg = '管理密钥无效或未配置（' + msg + '）';
          }
          throw new Error(msg);
        }
        return data;
      });
    });
  }

  // esc escapes a value for interpolation into HTML.
  //
  // All five characters matter: escaping only <, > and & leaves a value free to
  // close the attribute it sits in (a quote) or to start a new one. Values here
  // come from the upstream, so "it is only a uid" is not a guarantee.
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  // escapeHTML used to be a second implementation of the same thing, and the two
  // drifted: some callbacks used one, some the other, so a fix to one silently
  // left half the page unescaped. It now delegates, so there is a single place to
  // get this right.
  function escapeHTML(v) {
    return esc(v);
  }

  function msgSet(id, text, cls) {
    var el = document.getElementById(id);
    if (!el) return;
    el.textContent = text || '';
    el.className = 'small ' + (cls || 'muted');
  }

  function renderCheckin(run) {
    if (!run) return '';
    var rs = run.results || [];
    var out = '<table><thead><tr><th>账号</th><th>结果</th><th>说明</th><th class="num">码</th></tr></thead><tbody>';
    if (!rs.length) {
      out += '<tr><td colspan="4" class="muted">无结果</td></tr>';
    }
    rs.forEach(function (r) {
      var pill = 'ok', text = '成功';
      if (r.error) { pill = 'bad'; text = '错误'; }
      else if (!r.success) { pill = 'bad'; text = '失败'; }
      else if (r.already_checked_in) { pill = 'warn'; text = '已签到'; }
      out += '<tr><td>' + esc(r.label || r.auth_id) + '</td>' +
        '<td><span class="pill ' + pill + '">' + text + '</span></td>' +
        '<td>' + esc(r.error || r.message) + '</td>' +
        '<td class="num">' + (r.code == null ? '' : r.code) + '</td></tr>';
    });
    out += '</tbody></table>';
    return out;
  }

  // renderQuota renders a refresh pass as a table.
  //
  // Mirrors the server-side renderQuotaResults markup, including the card and the
  // scroll wrapper: this output replaces that table in place, so a different shape
  // would make the panel jump on refresh.
  function renderQuota(results) {
    if (!results || !results.length) {
      return '<div class="empty">本次没有可查询的账号。</div>';
    }
    var out = '<div class="card"><div class="table-wrap"><table>' +
      '<tr><th>账号</th><th>区域</th><th>剩余额度</th><th>说明</th></tr>';
    results.forEach(function (r) {
      var cls = r.error ? 'bad' : 'ok';
      var label = r.label || r.auth_id || '—';
      var note = r.error || r.message || '';
      out += '<tr><td>' + esc(label) + '</td>' +
        '<td>' + esc(r.region || '') + '</td>' +
        '<td class="' + cls + '">' + (r.credits == null ? 0 : r.credits) + '</td>' +
        '<td>' + esc(note) + '</td></tr>';
    });
    out += '</table></div></div>';
    return out;
  }

  // ---- combined action ------------------------------------------------
  window.runAll = function (button) {
    var box = document.getElementById('taskResult');
    if (box) box.innerHTML = '';
    msgSet('taskMsg', '执行中…', 'muted');

    call(BASE + '/run', { method: 'POST' }).then(function (payload) {
      var c = payload.checkin || {};
      msgSet('runMsg', '完成：签到成功 ' + (c.succeeded || 0) + ' / 失败 ' + (c.failed || 0), 'ok');
      if (box) {
        box.innerHTML = '<h2>本次结果</h2>' +
          (payload.checkin ? renderCheckin(payload.checkin) : '') +
          (payload.quota ? renderQuota(payload.quota) : '');
      }
      // No reload: the result above is already on the page, and reloading a moment
      // later is what made it flash by. Counters catch up on the next poll.
    }).catch(function (e) {
      msgSet('runMsg', '执行失败：' + e.message, 'bad');
    }).then(function () { if (btn) btn.disabled = false; });
  };

  window.refreshAccounts = function () {
    msgSet('runMsg', '刷新中…', 'muted');
    call(BASE + '/accounts').then(function (d) {
      msgSet('runMsg', '账号 ' + (d.total || 0) + ' 个，可用 ' + (d.usable || 0) + ' 个', 'ok');
      setTimeout(function () { location.reload(); }, 700);
    }).catch(function (e) { msgSet('runMsg', '刷新失败：' + e.message, 'bad'); });
  };

  // ---- strategy --------------------------------------------------------
  window.saveStrategy = function () {
    var picked = document.querySelector('input[name="strategy"]:checked');
    if (!picked) { msgSet('strategyMsg', '请选择一种策略', 'bad'); return; }
    msgSet('strategyMsg', '应用中…', 'muted');
    call(BASE + '/routing/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ strategy: picked.value })
    }).then(function () {
      msgSet('strategyMsg', '已应用：' + picked.value, 'ok');
      setTimeout(function () { location.reload(); }, 700);
    }).catch(function (e) { msgSet('strategyMsg', '应用失败：' + e.message, 'bad'); });
  };

  window.resetRotation = function () {
    msgSet('strategyMsg', '重置中…', 'muted');
    call(BASE + '/routing/reset', { method: 'POST' }).then(function () {
      msgSet('strategyMsg', '轮巡位置已重置', 'ok');
    }).catch(function (e) { msgSet('strategyMsg', '重置失败：' + e.message, 'bad'); });
  };

  // ---- check-in --------------------------------------------------------
  window.runCheckin = function () {
    msgSet('runMsg', '签到中…', 'muted');
    call(BASE + '/checkin/run', { method: 'POST' }).then(function (run) {
      msgSet('runMsg', '签到完成：成功 ' + (run.succeeded || 0) + ' / 失败 ' + (run.failed || 0), 'ok');
      var box = document.getElementById('taskResult');
      if (box) box.innerHTML = renderCheckin(run);
      // No reload: the run's detail is rendered above and would otherwise be wiped
      // before it could be read.
    }).catch(function (e) { msgSet('runMsg', '签到失败：' + e.message, 'bad'); });
  };

  window.saveCheckinSettings = function () {
    var msg = document.getElementById('runMsg');
    if (msg) { msg.textContent = '保存中…'; msg.className = 'small muted'; }
    call(BASE + '/checkin/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        enabled: readChecked('ckEnabled', false),
        hour: readNumber('ckHour', 0),
        minute: readNumber('ckMinute', 0),
        on_start: readChecked('ckOnStart', false)
      })
    }).then(function () {
      if (msg) { msg.textContent = '设置已保存'; msg.className = 'small ok'; }
      setTimeout(function () { location.reload(); }, 700);
    }).catch(function (e) {
      if (msg) { msg.textContent = '保存失败：' + e.message; msg.className = 'small bad'; }
    });
  };

  // readChecked / readNumber read a control without assuming it exists.
  //
  // getElementById(...).checked throws when the element is absent, and an exception
  // inside a .then() handler silently kills the rest of that promise chain — the
  // visible symptom is a panel that stops updating, with the real cause several
  // layers away. The elements can legitimately be missing: the panel is rendered by
  // the server, and a control only exists on the tab that owns it.
  function readChecked(id, fallback) {
    var el = document.getElementById(id);
    return el ? !!el.checked : !!fallback;
  }

  function readNumber(id, fallback) {
    var el = document.getElementById(id);
    if (!el) return fallback;
    var parsed = parseInt(el.value, 10);
    return isNaN(parsed) ? fallback : parsed;
  }


  // ---- quota -----------------------------------------------------------

  window.refreshQuota = function () {
    msgSet('quotaMsg', '查询中…', 'muted');

    // Update the visible tables in place rather than reloading the page.
    //
    // Both the credits tab and the account table show credit readings, so both need
    // the new numbers. Reloading would also work, but it throws away the operator's
    // scroll position and can interrupt a query that is still in flight.
    function applyResults(payload) {
      var results = (payload && payload.results) || [];
      var summary = '完成：积分合计 ' + ((payload && payload.total_credits) || 0) +
        '（' + ((payload && payload.accounts_known) || 0) + '/' +
        ((payload && payload.accounts_total) || 0) + ' 账号已查询）';
      msgSet('quotaMsg', summary, 'ok');

      // The credits tab's own table.
      var box = document.getElementById('quotaResults');
      if (box) box.innerHTML = renderQuota(results);

      // The account table's per-row credit cells.
      for (var i = 0; i < results.length; i++) {
        var hit = results[i];
        if (hit && hit.uid) {
          updateCreditCell(hit.uid, hit);
        } else if (hit && hit.auth_id) {
          updateCreditCell(hit.auth_id, hit);
        }
      }
    }

    call(BASE + '/quota/refresh', { method: 'POST' })
      .then(applyResults)
      .catch(function (e) { msgSet('quotaMsg', '刷新失败：' + e.message, 'bad'); });
  };

  // refreshQuotaOneAccount is not a separate entry point: the per-row button calls

  // ---- variant override -----------------------------------------------
  // savePanelChoice posts one panel selection.
  //
  // Only the touched field is sent: the endpoint keeps the other one untouched,
  // so switching authorisation cannot silently reset the call scope.
  function savePanelChoice(field, value, segId, dataAttr, msgId, onOk) {
    var msg = document.getElementById(msgId);
    if (msg) { msg.textContent = '保存中…'; msg.className = 'small muted'; }
    var body = {};
    body[field] = value;
    return call(BASE + '/variant', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }).then(function (payload) {
      // Update the segmented control in place. Reloading immediately used to
      // wipe the confirmation after 700ms, which is why a successful switch
      // looked like nothing had happened.
      var seg = document.getElementById(segId);
      if (seg) {
        var buttons = seg.getElementsByTagName('button');
        for (var i = 0; i < buttons.length; i++) {
          buttons[i].className = buttons[i].getAttribute(dataAttr) === value ? 'active' : '';
        }
      }
      if (msg) { msg.textContent = onOk(payload); msg.className = 'small ok'; }
      return payload;
    });
  }

  window.setVariant = function (v) {
    savePanelChoice('variant', v, 'variantSeg', 'data-variant', 'variantMsg', function () {
      return '调用范围已切换为 ' + (v === 'cn' ? '仅国内供应商' : v === 'ai' ? '仅国际供应商' : '全部供应商') +
        '；账号归属不变，仅决定哪些账号参与调用';
    }).catch(function (e) {
      var msg = document.getElementById('variantMsg');
      if (msg) { msg.textContent = '设置失败：' + e.message; msg.className = 'small bad'; }
    });
  };

  // setAuthSupplier switches which supplier CPA's OAuth entry authorises.
  window.setAuthSupplier = function (v) {
    savePanelChoice('auth_supplier', v, 'authSeg', 'data-auth', 'authMsg', function (payload) {
      var label = v === 'cn' ? '国内授权' : v === 'ai' ? '国际授权' : '跟随调用设置';
      var host = payload.auth_effective === 'ai' ? 'www.workbuddy.ai' : 'copilot.tencent.com';
      return '授权渠道已设为 ' + label + '；下次在 CPA 的 OAuth 入口授权将使用 ' + host;
    }).catch(function (e) {
      var msg = document.getElementById('authMsg');
      if (msg) { msg.textContent = '设置失败：' + e.message; msg.className = 'small bad'; }
    });
  };


  // ---- account toggle --------------------------------------------------
  // toggleAccount enables or disables one account.
  //
  // Feedback goes to #accountMsg, which lives in the accounts tab. The previous
  // version wrote to #runMsg — an element in a different tab — so a successful
  // toggle produced no visible change at all and read as "禁用没生效".
  window.toggleAccount = function (uid, action, authIndex) {
    msgSet('accountMsg', '操作中…', 'muted');
    call(BASE + '/account/toggle', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ uid: uid, auth_index: authIndex || '', action: action || 'toggle' })
    }).then(function () {
      msgSet('accountMsg', action === 'enable' ? '已启用，正在刷新列表…' : '已禁用，正在刷新列表…', 'ok');
      setTimeout(function () { location.reload(); }, 500);
    }).catch(function (e) {
      msgSet('accountMsg', '操作失败：' + e.message, 'bad');
    });
  };

  // ---- task tab --------------------------------------------------------
  //
  // These two handlers are referenced by the task tab markup. They were
  // missing entirely, so every button on that tab threw a ReferenceError and
  // the page showed nothing at all — the "任务 tab 点了没反应" report.
  window.runAllTasks = function () {
    var btn = document.getElementById('btnRunAllTasks');
    var box = document.getElementById('taskResult');
    if (btn) btn.disabled = true;
    if (box) box.innerHTML = '';
    msgSet('taskMsg', '执行中…', 'muted');

    call(BASE + '/run', { method: 'POST' }).then(function (payload) {
      var c = payload.checkin || {};
      var extra = (c.skipped || 0) > 0 ? '，跳过 ' + c.skipped + ' 个' : '';
      msgSet('taskMsg', '完成：签到成功 ' + (c.succeeded || 0) + ' / 失败 ' + (c.failed || 0) + extra, 'ok');
      if (box) {
        box.innerHTML = '<h2>本次结果</h2>' +
          (payload.checkin ? renderCheckin(payload.checkin) : '') +
          (payload.quota ? renderQuota(payload.quota) : '');
      }
      // No reload: same reason as the sign-in path — the detail is already rendered.
    }).catch(function (e) {
      msgSet('taskMsg', '执行失败：' + e.message, 'bad');
    }).then(function () { if (btn) btn.disabled = false; });
  };

  // renderGrowthResult turns the run's log lines into grouped, collapsible cards.
  //
  // The log used to be dumped into one <pre>: successes, skips and failures all
  // interleaved, with failure entries carrying a full upstream JSON blob. A run
  // where sixteen tasks are blocked by one missing prerequisite therefore produced
  // sixteen screens of near-identical text, and the one line that explained
  // *why* was buried inside it.
  //
  // So: count first, detail second. Each level gets its own section with a
  // one-line summary; the entries themselves live behind a <details> and are
  // closed by default. Failures open by themselves only when there are few enough
  // to read at a glance — when there are many, the summary is the useful part.
  function renderGrowthResult(lines, earned, accountCount) {
    var groups = { ok: [], skip: [], error: [], warn: [], info: [] };
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i] || {};
      var level = String(line.level || 'info');
      if (!groups[level]) level = 'info';
      groups[level].push(String(line.message || ''));
    }

    var parts = [];
    parts.push('<div class="card"><h2>成长任务结果 <span class="hint">' +
      esc(accountCount) + ' 个账号 · 累计 +' + esc(earned) + ' 积分</span></h2>');

    // Summary strip: the numbers an operator actually wants.
    parts.push('<div class="grid stats">' + [
      ['完成', groups.ok.length],
      ['跳过', groups.skip.length],
      ['未成功', groups.error.length],
      ['提示', groups.warn.length + groups.info.length]
    ].map(function (pair) {
      return '<div class="stat"><div class="v">' + esc(pair[1]) + '</div><div class="k">' +
        esc(pair[0]) + '</div></div>';
    }).join('') + '</div>');

    // Legend: the marks used below, spelled out. Without it the icons are just
    // decoration and a reader has to guess what a crossed circle meant.
    parts.push('<div class="legend">' +
      '<span><i class="mark ok"></i>完成</span>' +
      '<span><i class="mark skip"></i>跳过（无法代做或不在时段）</span>' +
      '<span><i class="mark err"></i>未成功（可展开看原因）</span>' +
      '<span><i class="mark info"></i>说明</span>' +
      '</div>');

    // Sections, most actionable first.
    //
    // "未成功" opens by default even when long: it is the section that needs a
    // decision, and a collapsed group would hide the very thing the operator came
    // to read. Its body is height-capped and scrolls, so a run with dozens of
    // failures cannot push the rest of the page off screen.
    parts.push(renderGrowthSection('未成功', 'err', groups.error, groups.error.length > 0))
    parts.push(renderGrowthSection('跳过', 'skip', groups.skip, false));
    parts.push(renderGrowthSection('完成', 'ok', groups.ok, false));
    parts.push(renderGrowthSection('说明', 'info', groups.info.concat(groups.warn), false));

    parts.push('</div>');
    return parts.join('');
  }

  // renderGrowthSection builds one collapsible group of log entries.
  //
  // Entries are never dropped: an operator debugging a skipped task needs the
  // reason, and the reason is upstream text that this panel cannot paraphrase
  // without losing detail.
  function renderGrowthSection(title, kind, entries, openByDefault) {
    if (!entries.length) return '';
    var body = entries.map(function (message) {
      return '<div class="log-entry"><i class="mark ' + kind + '"></i>' +
        '<span class="log-text">' + esc(message) + '</span></div>';
    }).join('');
    // Cap the height of the long, open-by-default sections. A run blocked by one
    // missing prerequisite yields one entry per affected task, which is easily
    // dozens of lines; unbounded they would bury the summary above them.
    var scrollable = kind === 'err' && entries.length > 6 ? ' log-scroll' : '';
    return '<details class="log-group"' + (openByDefault ? ' open' : '') + '>' +
      '<summary><i class="mark ' + kind + '"></i>' + esc(title) +
      '<span class="count">' + esc(entries.length) + ' 条</span></summary>' +
      '<div class="log-body' + scrollable + '">' + body + '</div>' +
      '</details>';
  }


  //
  // It forwards to the same /account/toggle endpoint the account tab uses, so
  // the task tab and the account tab can never disagree about an account's
  // state. The caller passes the action to apply, not the current state.
  // toggleAccountTask flips one account's task participation.
  //
  // It forwards to the same /account/toggle endpoint the account tab uses, so
  // the task tab and the account tab can never disagree about an account's
  // state. The caller passes the action to apply, not the current state.
  window.toggleAccountTask = function (uid, action) {
    msgSet('taskMsg', '操作中…', 'muted');
    call(BASE + '/account/toggle', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ uid: uid, action: action === 'enable' ? 'enable' : 'disable' })
    }).then(function () {
      msgSet('taskMsg', action === 'enable' ? '已启用，正在刷新…' : '已禁用，正在刷新…', 'ok');
      setTimeout(function () { location.reload(); }, 500);
    }).catch(function (e) {
      msgSet('taskMsg', '操作失败：' + e.message, 'bad');
    });
  };

  // ---- auto refresh ----------------------------------------------------
  //
  // The account list used to update only when the operator pressed 刷新列表.
  // A login completed in CPA's own Auth page therefore stayed invisible until a
  // manual reload, which read as "账号不同步". Poll the inventory while the
  // accounts tab is visible; the host call is cached for 5s server-side, so a
  // 20s interval is cheap.
  var AUTO_REFRESH_MS = 20000;
  var autoRefreshTimer = null;

  // Visibility is read from the hidden attribute, which is how showTab switches
  // pages. Reading a class name instead would silently stop working the day the
  // class is renamed or another rule starts setting display.
  function pageVisible(id) {
    var panel = document.getElementById(id);
    return !!panel && !panel.hidden;
  }

  function accountsTabVisible() {
    return pageVisible('view-accounts');
  }

  // refreshUsageTrend pulls the per-day totals and draws them.
  //
  // Only fetched while the usage tab is on screen: the trend covers a week and
  // changes slowly, so asking for it while the operator is looking at accounts
  // would be pure noise on the management API.
  function refreshUsageTrend() {
    if (!key() || !usageTabVisible()) return;
    call(BASE + '/status').then(function (d) {
      renderUsageTrend(d && d.usage_daily);
    }).catch(function () { /* transient; the next tick retries */ });
  }
  // Exposed for showTab, which is defined outside this closure (it is injected as
  // plain top-level script so the tab bar works even if this block fails to run).
  // A bare identifier would not resolve across that boundary.
  window.refreshUsageTrend = refreshUsageTrend;

  function usageTabVisible() {
    return pageVisible('view-usage');
  }

  function pollAccounts() {
    if (!key() || !accountsTabVisible()) return;
    call(BASE + '/accounts').then(function (d) {
      var stamp = document.getElementById('accountMsg');
      if (stamp) {
        stamp.textContent = '账号 ' + (d.total || 0) + ' 个，可用 ' + (d.usable || 0) +
          ' 个 · 数据读取于 ' + new Date().toLocaleTimeString();
      }
      // Only reload when the inventory actually changed, so a steady state
      // does not keep yanking the page out from under the operator.
      var current = document.getElementById('accountsSignature');
      var list = d.accounts || [];
      var usableCount = 0;
      var parts = [];
      for (var i = 0; i < list.length; i++) {
        if (list[i].usable) usableCount++;
        parts.push((list[i].uid || list[i].auth_index || '') + (list[i].disabled_by_user ? 'D' : 'E'));
      }
      // Must match accountsSignature() in main_page.go exactly.
      var signature = list.length + ':' + usableCount + ':' + parts.join(',');
      if (current && current.value && current.value !== signature) {
        location.reload();
        return;
      }
      if (current) current.value = signature;
    }).catch(function () { /* transient; the next tick retries */ });
  }

  function startAutoRefresh() {
    if (autoRefreshTimer) return;
    autoRefreshTimer = setInterval(function () {
      pollAccounts();
      refreshUsageTrend();
    }, AUTO_REFRESH_MS);
  }

  // ---- growth tasks ----------------------------------------------------
  //
  // The growth pass is slower than the other tabs' actions (it spaces upstream
  // calls by a second), so the buttons report progress and stay disabled until
  // the response arrives.
  function growthButton(id, busy, label) {
    var btn = document.getElementById(id);
    if (!btn) return;
    btn.disabled = busy;
    if (label) btn.textContent = label;
  }

  window.runGrowthTasks = function () {
    growthButton('btnRunGrowth', true, '执行中…');
    msgSet('taskMsg', '正在接取、点亮并领取成长任务（可能需要一两分钟）…', 'muted');

    call(BASE + '/growth/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ uid: 'all' })
    }).then(function (payload) {
      if (payload.ok === false) {
        msgSet('taskMsg', '执行失败：' + (payload.error || '未知原因'), 'bad');
        return;
      }
      var lines = payload.logs || [];
      var earned = payload.earned_credit || 0;
      msgSet('taskMsg', '完成：' + (payload.accounts_count || 0) + ' 个账号，累计 +' + earned + ' 积分', 'ok');
      var box = document.getElementById('taskResult');
      if (box) {
        box.innerHTML = renderGrowthResult(lines, earned, payload.accounts_count || 0);
      }
      // No reload here. The result is rendered in place, and reloading would wipe it
      // a moment later — which is exactly what "the detail flashes by" meant. The
      // page's counters catch up on the next poll.
    }).catch(function (e) {
      msgSet('taskMsg', '执行失败：' + e.message, 'bad');
    }).then(function () {
      growthButton('btnRunGrowth', false, '完成成长任务');
    });
  };

  window.runTravel = function () {
    growthButton('btnTravel', true, '执行中…');
    msgSet('taskMsg', '正在检查猫猫旅行…', 'muted');

    call(BASE + '/growth/travel', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ uid: 'all' })
    }).then(function (payload) {
      if (payload.ok === false) {
        msgSet('taskMsg', '执行失败：' + (payload.error || '未知原因'), 'bad');
        return;
      }
      var results = payload.results || [];
      var parts = [];
      for (var i = 0; i < results.length; i++) {
        parts.push(results[i].label + ': ' + (results[i].message || results[i].error || ''));
      }
      msgSet('taskMsg', parts.join('；') || '没有可执行的账号', parts.length ? 'ok' : 'muted');
    }).catch(function (e) {
      msgSet('taskMsg', '执行失败：' + e.message, 'bad');
    }).then(function () {
      growthButton('btnTravel', false, '猫猫旅行');
    });
  };

  // loadGrowthTasks renders the per-task detail for the first eligible account.
  window.loadGrowthTasks = function () {
    msgSet('growthMsg', '查询中…', 'muted');
    var box = document.getElementById('growthDetail');
    if (box) box.innerHTML = '';

    call(BASE + '/growth/tasks').then(function (runs) {
      var list = runs.runs || [];
      if (!list.length) {
        msgSet('growthMsg', '还没有运行记录，先执行一次成长任务', 'muted');
        return;
      }
      // The stored results carry the uid to query.
      return loadGrowthDetailFor(list[0].uid, list[0].label);
    }).catch(function (e) {
      msgSet('growthMsg', '查询失败：' + e.message, 'bad');
    });
  };

  function loadGrowthDetailFor(uid, label) {
    return call(BASE + '/growth/tasks?uid=' + encodeURIComponent(uid)).then(function (payload) {
      var box = document.getElementById('growthDetail');
      if (payload.ok === false) {
        msgSet('growthMsg', '查询失败：' + (payload.error || '未知原因') +
          (payload.detail ? ' — ' + payload.detail : ''), 'bad');
        if (box) box.innerHTML = '<div class="note bad">' + escapeHTML(payload.error || '') +
          (payload.detail ? '<br>' + escapeHTML(payload.detail) : '') + '</div>';
        return;
      }
      var tasks = payload.tasks || [];
      var s = payload.summary || {};
      var travel = s.travel || {};
      msgSet('growthMsg', '账号 ' + (payload.label || label) + '：能量 ' + (s.energy || 0) +
        '，连续打卡 ' + (s.streak_days || 0) + ' 天，猫猫 ' + (travel.state || '未知'), 'ok');
      if (box) {
        var html = '<table><thead><tr><th>任务</th><th class="num">进度</th><th class="num">奖励</th><th>状态</th></tr></thead><tbody>';
        for (var i = 0; i < tasks.length; i++) {
          var t = tasks[i];
          var statusText = t.status || '';
          if (t.unforgeable) statusText = '无法代做';
          else if (t.desktop_only) statusText = '需桌面操作';
          else if (statusText === 'claimed') statusText = '已领奖';
          else if (statusText === 'completed') statusText = '已完成';
          else if (statusText === 'not_accepted') statusText = '未接取';
          else if (statusText === 'accepted') statusText = '进行中';

          var note = '';
          if (t.skip_reason) note = ' <span class="muted small">' + escapeHTML(t.skip_reason) + '</span>';
          if (t.jump_url) note += ' <span class="muted small">' + escapeHTML(t.jump_url) + '</span>';

          html += '<tr><td><strong>' + escapeHTML(t.name || t.task_code) + '</strong>' + note + '</td>' +
            '<td class="num">' + (t.current || 0) + '/' + (t.target || 1) + '</td>' +
            '<td class="num">+' + (t.reward_credit || 0) + '</td>' +
            '<td>' + escapeHTML(statusText) + '</td></tr>';
        }
        html += '</tbody></table>';
        box.innerHTML = html;
      }
    });
  }

  // Delegated click handling for the account table.
  //
  // The rows used to carry inline onclick attributes built by string concatenation
  // on the server, which put the uid inside a JavaScript string literal inside an
  // HTML attribute: escaping for HTML was not enough, a quote in the uid would end
  // the literal and the attribute. Reading the value from a data attribute keeps
  // the data out of code entirely.

  // refreshAccountsAndQuota re-reads the account list and fetches every account's
  // credit balance.
  //
  // One button for both because they answer the same question — "what do I have and
  // what is it worth" — and the credit readings are only meaningful against a fresh
  // list. Doing them separately left the operator pressing one and then the other.
  window.refreshAccountsAndQuota = function (button) {
    var original = button ? button.textContent : '';
    if (button) {
      button.disabled = true;
      button.textContent = '刷新中';
    }
    msgSet('accountMsg', '正在重新读取账号并刷新积分…', 'muted');

    call(BASE + '/accounts')
      .then(function () {
        // The list itself is rendered server-side, so re-read it from the plugin
        // before fetching balances: an account added since page load would
        // otherwise be missed.
        return call(BASE + '/quota/refresh', { method: 'POST' });
      })
      .then(function (payload) {
        var results = (payload && payload.results) || [];
        var updated = 0;
        for (var i = 0; i < results.length; i++) {
          var hit = results[i];
          if (!hit) continue;
          var key = hit.uid || hit.auth_id;
          if (key && updateCreditCell(key, hit)) updated++;
        }
        msgSet('accountMsg', '完成：' + updated + ' 个账号的积分已更新。', 'ok');
        // The credit totals in the stat strip are server-rendered, so refresh the
        // page once the data is in — this is the one place a reload is worth it.
        if (updated > 0) {
          setTimeout(function () { location.reload(); }, 800);
        }
      })
      .catch(function (e) { msgSet('accountMsg', '刷新失败：' + e.message, 'bad'); })
      .then(function () {
        if (button) {
          button.disabled = false;
          button.textContent = original;
        }
      });
  };



  // updateCreditCell writes a fresh credit reading into the row's credit cell.
  //
  // The cell carries data-credits-for, so the number can be replaced in place.
  // Reloading the page would show the same value, but it also costs the operator
  // their scroll position and dismisses whatever they were reading — a bad trade
  // for one figure.
  //
  // Returns whether anything was written. Not finding the cell is not an error:
  // the list may have been re-rendered, or the account may be filtered out of
  // view, and the toast still has something useful to say.
  function updateCreditCell(uid, result) {
    if (!uid || !result || result.error || !result.known) return false;
    var cell = document.querySelector('[data-credits-for="' + cssEscape(uid) + '"]');
    if (!cell) return false;
    cell.textContent = String(result.credits);
    // Brief highlight so the change is visible; without it a number that happens
    // to be unchanged looks like nothing happened.
    cell.classList.add('flash');
    setTimeout(function () { cell.classList.remove('flash'); }, 900);
    return true;
  }

  // cssEscape quotes a value for use inside an attribute selector.
  //
  // CSS.escape is not present everywhere, and an upstream-provided value can
  // contain quotes or brackets, which would make the selector invalid and throw.
  function cssEscape(value) {
    if (window.CSS && typeof window.CSS.escape === 'function') {
      return window.CSS.escape(value);
    }
    return String(value).replace(/["\\\]\[]/g, function (c) { return '\\' + c; });
  }

  // toast shows a short, self-dismissing notice in the corner.
  //
  // Actions used to report themselves by writing a line of text and then calling
  // location.reload(). That reload cost the operator their scroll position and the
  // tab they were on, and it happened even when the action failed. A toast reports
  // the outcome without moving anything.
  function toast(text, kind) {
    if (!text) return;
    var host = document.getElementById('toasts');
    if (!host) {
      host = document.createElement('div');
      host.id = 'toasts';
      document.body.appendChild(host);
    }
    var node = document.createElement('div');
    node.className = 'toast ' + (kind === 'bad' ? 'bad' : kind === 'warn' ? 'warn' : 'ok');
    // textContent, not innerHTML: the message can carry upstream wording.
    node.textContent = text;
    host.appendChild(node);
    // Errors linger longer than successes — they carry something to read.
    var life = kind === 'bad' ? 6000 : 3600;
    setTimeout(function () {
      node.classList.add('leaving');
      setTimeout(function () { if (node.parentNode) node.parentNode.removeChild(node); }, 250);
    }, life);
  }

  // renderUsageTrend draws one bar per day: failures stacked on successes.
  //
  // A day with no traffic is drawn as an empty slot rather than filled in with a
  // zero-height bar, so a quiet weekend does not read as a provider outage. The
  // scale always includes zero and is taken from the busiest day, which keeps the
  // bars comparable to each other rather than to an arbitrary ceiling.
  function renderUsageTrend(days) {
    var host = document.getElementById('usageTrend');
    if (!host) return;

    if (!days || !days.length) {
      host.innerHTML = '<div class="empty">还没有调用记录。发起一次请求后这里会显示每日用量。</div>';
      return;
    }

    var maxCalls = 0;
    for (var i = 0; i < days.length; i++) {
      if (days[i].calls > maxCalls) maxCalls = days[i].calls;
    }
    if (maxCalls <= 0) {
      host.innerHTML = '<div class="empty">还没有调用记录。</div>';
      return;
    }

    var width = 640;
    var height = 150;
    var padLeft = 34;
    var padBottom = 22;
    var padTop = 10;
    var plotWidth = width - padLeft;
    var plotHeight = height - padBottom - padTop;
    var slot = plotWidth / days.length;
    var barWidth = Math.max(6, Math.min(38, slot * 0.56));

    var parts = [];
    parts.push('<svg viewBox="0 0 ' + width + ' ' + height + '" class="trend-svg" role="img" ' +
      'aria-label="最近用量趋势">');

    // Horizontal gridlines at 0 / half / full, labelled with the call count.
    for (var g = 0; g <= 2; g++) {
      var value = Math.round(maxCalls * g / 2);
      var y = padTop + plotHeight - (plotHeight * g / 2);
      parts.push('<line x1="' + padLeft + '" y1="' + y + '" x2="' + width + '" y2="' + y +
        '" stroke="currentColor" stroke-opacity="' + (g === 0 ? '.22' : '.10') + '" stroke-width="1"/>');
      parts.push('<text x="' + (padLeft - 6) + '" y="' + (y + 3.5) + '" text-anchor="end" ' +
        'font-size="10" fill="currentColor" fill-opacity=".55">' + value + '</text>');
    }

    for (var d = 0; d < days.length; d++) {
      var day = days[d];
      var total = Number(day.calls) || 0;
      var failed = Math.min(Number(day.failed) || 0, total);
      var okPart = total - failed;

      var x = padLeft + slot * d + (slot - barWidth) / 2;
      var fullHeight = plotHeight * (total / maxCalls);
      var okHeight = total > 0 ? fullHeight * (okPart / total) : 0;
      var failedHeight = fullHeight - okHeight;

      var baseY = padTop + plotHeight;
      var label = day.date + '：' + total + ' 次调用';
      if (failed > 0) label += '，失败 ' + failed + ' 次';
      parts.push('<g><title>' + esc(label) + '</title>');

      // Success block sits on the baseline; failures stack on top of it, so the
      // total height stays proportional and the failure share reads at a glance.
      if (okHeight > 0) {
        parts.push('<rect x="' + x + '" y="' + (baseY - okHeight) + '" width="' + barWidth +
          '" height="' + okHeight + '" rx="2" class="trend-ok"/>');
      }
      if (failedHeight > 0) {
        parts.push('<rect x="' + x + '" y="' + (baseY - fullHeight) + '" width="' + barWidth +
          '" height="' + failedHeight + '" rx="2" class="trend-bad"/>');
      }
      if (total === 0) {
        parts.push('<rect x="' + x + '" y="' + (baseY - 2) + '" width="' + barWidth +
          '" height="2" rx="1" fill="currentColor" fill-opacity=".16"/>');
      }
      parts.push('</g>');

      // Day label: month-day, with the year only when it is not the current one.
      // A full ISO date under every bar is unreadable at phone widths.
      var parts0 = String(day.date).split('-');
      var shortLabel = parts0.length === 3 ? (parts0[1] + '-' + parts0[2]) : day.date;
      parts.push('<text x="' + (padLeft + slot * d + slot / 2) + '" y="' + (height - 6) +
        '" text-anchor="middle" font-size="10" fill="currentColor" fill-opacity=".6">' +
        esc(shortLabel) + '</text>');
    }

    parts.push('</svg>');
    host.innerHTML = parts.join('');
  }

  // dispatchDataCall routes a data-call attribute to the named global function.
  //
  // Every control used to carry an inline onclick. A Content Security Policy that
  // forbids inline script blocks all of them at once and silently — the click does
  // nothing and no error reaches the console. Delegating from a listener in this
  // file keeps the behaviour identical while making it CSP-safe.
  //
  // Arguments come from data-arg0, data-arg1, … so a value is never interpolated
  // into code. "this" is not representable as a data attribute; the element that
  // matched is passed instead, which is what every caller meant by it.
  function dispatchDataCall(node) {
    var name = node.getAttribute && node.getAttribute('data-call');
    if (!name) return false;
    var fn = window[name];
    if (typeof fn !== 'function') return false;

    var args = [];
    for (var index = 0; ; index++) {
      var key = 'data-arg' + index;
      if (!node.hasAttribute(key)) break;
      args.push(node.getAttribute(key));
    }
    // A handler that took "this" needs the element; passing it matches how these
    // were called from the inline form.
    fn.apply(null, args.length ? args : [node]);
    return true;
  }

  // refreshUsage re-reads the usage page's numbers.
  //
  // The stat strip and the table are server-rendered, so the only way to refresh them
  // without a reload is to reload — but the chart is drawn client-side and can be
  // redrawn, which is the part that changes minute to minute. The button therefore
  // redraws the chart and reloads once, which is what the operator expects from a
  // refresh control on this page.
  window.refreshUsage = function (button) {
    var original = button ? button.textContent : '';
    if (button) {
      button.disabled = true;
      button.textContent = '刷新中';
    }
    refreshUsageTrend();
    setTimeout(function () { location.reload(); }, 400);
    if (button) {
      button.textContent = original;
      button.disabled = false;
    }
  };

  // selectAllTaskAccounts / clearAllTaskAccounts flip every account's participation
  // in the task runs.
  //
  // Done one request at a time rather than as a batch endpoint: the pool is small,
  // the calls are cheap, and a partial failure is then visible per account instead of
  // aborting the whole sweep.
  function setAllTaskAccounts(action, button) {
    var rows = document.querySelectorAll('[data-task-toggle]');
    if (!rows.length) return;
    var original = button ? button.textContent : '';
    if (button) {
      button.disabled = true;
      button.textContent = '处理中';
    }
    var pending = 0;
    var failed = 0;
    for (var i = 0; i < rows.length; i++) {
      var node = rows[i];
      var uid = node.getAttribute('data-uid');
      var current = node.getAttribute('data-action');
      // Only send the ones whose state would actually change: the toggle applies the
      // action it is given, so re-sending it for an account already in that state is
      // a wasted request.
      var wanted = action === 'enable' ? 'enable' : 'disable';
      if (current !== wanted) continue;
      pending++;
      call(BASE + '/account/toggle', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ uid: uid, action: wanted })
      }).catch(function () { failed++; });
    }
    if (pending === 0) {
      toast(action === 'enable' ? '所有账号都已启用' : '所有账号都已停用', 'ok');
      if (button) {
        button.disabled = false;
        button.textContent = original;
      }
      return;
    }
    // Give the requests a moment, then reload so the table shows the new state.
    setTimeout(function () {
      if (button) {
        button.disabled = false;
        button.textContent = original;
      }
      toast(failed ? ('完成，' + failed + ' 个失败') : '已更新', failed ? 'bad' : 'ok');
      setTimeout(function () { location.reload(); }, 600);
    }, 400 + pending * 60);
  }

  window.selectAllTaskAccounts = function (button) { setAllTaskAccounts('enable', button); };
  window.clearAllTaskAccounts = function (button) { setAllTaskAccounts('disable', button); };

  // loadTaskDetail fetches the per-task breakdown for the first account that has run.
  //
  // The panel has no account picker here: runs are per account but the interesting
  // detail is always the most recent one, and adding a selector for a list that is
  // usually one entry long would be more chrome than information.
  window.loadTaskDetail = function (button) {
    var original = button ? button.textContent : '';
    if (button) {
      button.disabled = true;
      button.textContent = '查询中';
    }
    msgSet('growthMsg', '查询中…', 'muted');
    var box = document.getElementById('growthDetail');
    if (box) box.innerHTML = '';

    call(BASE + '/growth/tasks').then(function (runs) {
      var list = (runs && runs.runs) || [];
      if (!list.length) {
        msgSet('growthMsg', '还没有运行记录，先执行一次任务', 'muted');
        return;
      }
      var first = list[0];
      return call(BASE + '/growth/tasks?uid=' + encodeURIComponent(first.uid || '')).then(function (payload) {
        if (box) box.innerHTML = renderGrowthDetail(payload);
        msgSet('growthMsg', '账号 ' + (first.label || first.uid || '—') + ' 的明细', 'ok');
      });
    }).catch(function (e) {
      msgSet('growthMsg', '查询失败：' + e.message, 'bad');
    }).then(function () {
      if (button) {
        button.disabled = false;
        button.textContent = original;
      }
    });
  };

  // saveGrowthSchedule stores the scheduled-run settings.
  window.saveGrowthSchedule = function (button) {
    msgSet('growthScheduleMsg', '保存中…', 'muted');
    call(BASE + '/growth/schedule', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        enabled: readChecked('gsEnabled', false),
        hour: readNumber('gsHour', 9),
        minute: readNumber('gsMinute', 0),
        on_start: readChecked('gsOnStart', true)
      })
    }).then(function () {
      msgSet('growthScheduleMsg', '已保存', 'ok');
      // The badge next to the title is server-rendered, so reload to show it.
      setTimeout(function () { location.reload(); }, 600);
    }).catch(function (e) {
      msgSet('growthScheduleMsg', '保存失败：' + e.message, 'bad');
    });
  };

  // renderGrowthDetail draws one account's per-task outcome as grouped rows.
  function renderGrowthDetail(payload) {
    if (!payload) return '';
    if (payload.ok === false) {
      return '<div class="note">' + esc(payload.error || '查询失败') +
        (payload.detail ? ' — ' + esc(payload.detail) : '') + '</div>';
    }
    var tasks = payload.tasks || [];
    if (!tasks.length) {
      return '<div class="empty">这次运行没有任务记录。</div>';
    }
    var out = '<div class="tbl-wrap"><table class="data detail"><thead><tr>' +
      '<th>任务</th><th>状态</th><th>说明</th></tr></thead><tbody>';
    for (var i = 0; i < tasks.length; i++) {
      var t = tasks[i] || {};
      var cls = t.error ? 'bad' : (t.skipped ? 'idle' : 'ok');
      out += '<tr><td data-label="任务">' + esc(t.label || t.code || '—') + '</td>' +
        '<td data-label="状态"><span class="pill ' + cls + '">' +
        esc(t.status || (t.skipped ? '跳过' : (t.error ? '失败' : '完成'))) + '</span></td>' +
        '<td class="note" data-label="说明">' + esc(t.error || t.message || '') + '</td></tr>';
    }
    return out + '</tbody></table></div>';
  }


  // runRowAction performs one of the per-row controls.
  //
  // The three actions share a shape: call an endpoint, replace the row's own figures
  // in place, report through the card's message slot. "任务" is different — it toggles
  // the account in or out of task runs and needs a reload because the task page's
  // table is rendered server-side.
  window.runRowAction = function (action, uid, button) {
    if (!uid) return;
    var original = button ? button.textContent : '';
    if (button) {
      button.disabled = true;
    }

    var done = function (message, ok) {
      if (button) {
        button.disabled = false;
        button.textContent = original;
      }
      toast(message, ok ? 'ok' : 'bad');
    };

    if (action === 'checkin') {
      msgSet('accountMsg', '正在为 ' + uid + ' 签到…', 'muted');
      call(BASE + '/checkin/run?uid=' + encodeURIComponent(uid), { method: 'POST' })
        .then(function (data) {
          var hit = ((data && data.accounts) || [])[0] || {};
          done(hit.error ? ('签到失败：' + hit.error) : '签到完成', !hit.error);
        })
        .catch(function (e) { done('签到失败：' + e.message, false); });
      return;
    }

    if (action === 'quota') {
      msgSet('accountMsg', '正在查询 ' + uid + ' 的余额…', 'muted');
      call(BASE + '/quota/refresh?uid=' + encodeURIComponent(uid), { method: 'POST' })
        .then(function (data) {
          var results = (data && data.results) || [];
          var hit = results[0] || {};
          updateCreditCell(uid, hit);
          done(hit.error ? ('查询失败：' + hit.error)
                         : ('余额 ' + (hit.credits != null ? hit.credits : '未知')), !hit.error);
        })
        .catch(function (e) { done('查询失败：' + e.message, false); });
      return;
    }

    if (action === 'tasks') {
      // A reload rather than an in-place update: the tasks page's table is rendered by
      // the server, and its state is what this button changes.
      call(BASE + '/account/toggle', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ uid: uid, action: 'enable' })
      }).then(function () {
        done('已让该账号参与任务', true);
        setTimeout(function () { location.reload(); }, 500);
      }).catch(function (e) { done('操作失败：' + e.message, false); });
      return;
    }

    if (button) button.disabled = false;
  };

  document.addEventListener('click', function (ev) {
    var node = ev.target;
    while (node && node !== document) {
      // Nav links carry data-view; the page id is derived from it.
      // The link carries the full page id, so no prefix is added here. Adding one
      // produced "view-view-tasks" and every page stayed hidden — the click looked
      // like it did nothing.
      if (node.hasAttribute && node.hasAttribute('data-view')) {
        ev.preventDefault();
        showTab(node.getAttribute('data-view'), node);
        return;
      }
      if (dispatchDataCall(node)) {
        ev.preventDefault();
        return;
      }
      if (node.hasAttribute && node.hasAttribute('data-account-toggle')) {
        ev.preventDefault();
        toggleAccount(node.getAttribute('data-uid'), node.getAttribute('data-action'), node.getAttribute('data-auth-index') || '');
        return;
      }
      if (node.hasAttribute && node.hasAttribute('data-row-action')) {
        ev.preventDefault();
        runRowAction(node.getAttribute('data-row-action'), node.getAttribute('data-uid'), node);
        return;
      }
      if (node.hasAttribute && node.hasAttribute('data-task-toggle')) {
        ev.preventDefault();
        toggleAccountTask(node.getAttribute('data-uid'), node.getAttribute('data-action'));
        return;
      }
      if (node.id === 'accountFilterClear') {
        ev.preventDefault();
        var box = document.getElementById('accountFilter');
        if (box) {
          box.value = '';
          // Keep the caret in the field so the operator can keep typing.
          box.focus();
        }
        applyAccountFilter();
        return;
      }
      node = node.parentNode;
    }
  });

  // applyAccountFilter hides rows that do not match the search box and the status
  // select.
  //
  // Filtering is done by toggling display on the existing rows rather than
  // re-rendering: the page is rebuilt by the server, so re-rendering here would
  // mean duplicating that markup in JavaScript.
  function applyAccountFilter() {
    var box = document.getElementById('accountFilter');
    var statusSel = document.getElementById('accountStatusFilter');
    if (!box && !statusSel) return;

    var needle = box ? box.value.trim().toLowerCase() : '';
    var wantStatus = statusSel ? statusSel.value : '';
    var shown = 0;
    var total = 0;

    // The clear button only earns its space once there is something to clear.
    var clearButton = document.getElementById('accountFilterClear');
    if (clearButton) clearButton.hidden = needle === '';

    var tables = document.querySelectorAll('[data-account-table]');
    for (var t = 0; t < tables.length; t++) {
      var rows = tables[t].querySelectorAll('tbody tr');
      var visibleInTable = 0;
      for (var i = 0; i < rows.length; i++) {
        var row = rows[i];
        // Skip the "no accounts here" placeholder row.
        if (row.getAttribute('data-placeholder') === '1') {
          continue;
        }
        total++;
        var haystack = (row.getAttribute('data-search') || '').toLowerCase();
        var matchesText = !needle || haystack.indexOf(needle) !== -1;
        var matchesStatus = !wantStatus || row.getAttribute('data-status') === wantStatus;
        var visible = matchesText && matchesStatus;
        row.hidden = !visible;
        if (visible) {
          shown++;
          visibleInTable++;
        }
      }
      // Hide the group heading and table when nothing under it matches, so an
      // empty realm does not leave a header hanging.
      var group = tables[t].closest('[data-account-group]');
      if (group) {
        group.hidden = visibleInTable === 0;
      }
    }

    var count = document.getElementById('accountFilterCount');
    if (count) {
      count.textContent = (needle || wantStatus) ? ('显示 ' + shown + ' / ' + total) : '';
    }
  }

  document.addEventListener('input', function (ev) {
    if (ev.target && (ev.target.id === 'accountFilter' || ev.target.id === 'accountStatusFilter')) {
      applyAccountFilter();
    }
  });

  document.addEventListener('change', function (ev) {
    if (ev.target && ev.target.id === 'accountStatusFilter') {
      applyAccountFilter();
    }
  });

  document.addEventListener('DOMContentLoaded', applyAccountFilter);

  document.addEventListener('DOMContentLoaded', function () {
    refreshKeyState();
    restoreTab();
    startAutoRefresh();
    pollAccounts();
  });
  if (document.readyState !== 'loading') {
    refreshKeyState();
    restoreTab();
    startAutoRefresh();
    pollAccounts();
  }
})();
</script>`

	return strings.NewReplacer(
		"{{KEY_NAME}}", checkinKeyStorageName,
		"{{MGMT}}", managementBasePath()+"/"+pluginName,
		"{{BASE}}", managementBasePath()+"/"+pluginName,
		"{{UI_TABS}}", uiTabsScript,
	).Replace(script)
}
