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

  function renderQuota(results) {
    if (!results || !results.length) return '';
    var out = '<table><thead><tr><th>账号</th><th>区域</th><th class="num">剩余积分</th><th>说明</th></tr></thead><tbody>';
    results.forEach(function (r) {
      var cls = r.error ? 'bad' : 'ok';
      out += '<tr><td>' + esc(r.label || r.auth_id) + '</td>' +
        '<td><span class="pill idle">' + esc(r.region) + '</span></td>' +
        '<td class="num ' + cls + '">' + (r.credits == null ? 0 : r.credits) + '</td>' +
        '<td>' + esc(r.error || r.message) + '</td></tr>';
    });
    out += '</tbody></table>';
    return out;
  }

  // ---- combined action ------------------------------------------------
  window.runAll = function () {
    var btn = document.getElementById('btnRun');
    var box = document.getElementById('runResult');
    if (btn) btn.disabled = true;
    if (box) box.innerHTML = '';
    msgSet('runMsg', '执行中…', 'muted');

    call(BASE + '/run', { method: 'POST' }).then(function (payload) {
      var c = payload.checkin || {};
      msgSet('runMsg', '完成：签到成功 ' + (c.succeeded || 0) + ' / 失败 ' + (c.failed || 0), 'ok');
      if (box) {
        box.innerHTML = '<h2>本次结果</h2>' +
          (payload.checkin ? renderCheckin(payload.checkin) : '') +
          (payload.quota ? renderQuota(payload.quota) : '');
      }
      setTimeout(function () { location.reload(); }, 1500);
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
      var box = document.getElementById('runResult');
      if (box) box.innerHTML = '<h2>签到结果</h2>' + renderCheckin(run);
      setTimeout(function () { location.reload(); }, 1500);
    }).catch(function (e) { msgSet('runMsg', '签到失败：' + e.message, 'bad'); });
  };

  window.saveCheckinSettings = function () {
    var msg = document.getElementById('runMsg');
    if (msg) { msg.textContent = '保存中…'; msg.className = 'small muted'; }
    call(BASE + '/checkin/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        enabled: !!document.getElementById('ckEnabled').checked,
        hour: parseInt(document.getElementById('ckHour').value, 10) || 0,
        minute: parseInt(document.getElementById('ckMinute').value, 10) || 0,
        on_start: !!document.getElementById('ckOnStart').checked
      })
    }).then(function () {
      if (msg) { msg.textContent = '设置已保存'; msg.className = 'small ok'; }
      setTimeout(function () { location.reload(); }, 700);
    }).catch(function (e) {
      if (msg) { msg.textContent = '保存失败：' + e.message; msg.className = 'small bad'; }
    });
  };

  // ---- quota -----------------------------------------------------------
  window.refreshQuota = function () {
    msgSet('runMsg', '查询中…', 'muted');
    call(BASE + '/quota/refresh', { method: 'POST' }).then(function (payload) {
      msgSet('runMsg', '完成：积分合计 ' + (payload.total_credits || 0) +
        '（' + (payload.accounts_known || 0) + '/' + (payload.accounts_total || 0) + ' 账号已查询）', 'ok');
      var box = document.getElementById('runResult');
      if (box) box.innerHTML = '<h2>积分结果</h2>' + renderQuota(payload.results);
      setTimeout(function () { location.reload(); }, 1500);
    }).catch(function (e) { msgSet('runMsg', '刷新失败：' + e.message, 'bad'); });
  };

  window.saveQuotaSettings = function () {
    var msg = document.getElementById('runMsg');
    if (msg) { msg.textContent = '保存中…'; msg.className = 'small muted'; }
    call(BASE + '/quota/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        enabled: !!document.getElementById('qEnabled').checked,
        interval_minutes: parseInt(document.getElementById('qInterval').value, 10) || 30,
        refresh_on_start: !!document.getElementById('qOnStart').checked
      })
    }).then(function () {
      if (msg) { msg.textContent = '设置已保存'; msg.className = 'small ok'; }
      setTimeout(function () { location.reload(); }, 700);
    }).catch(function (e) {
      if (msg) { msg.textContent = '保存失败：' + e.message; msg.className = 'small bad'; }
    });
  };

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
      setTimeout(function () { location.reload(); }, 1500);
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

  function accountsTabVisible() {
    var panel = document.getElementById('tab-accounts');
    return !!panel && panel.classList.contains('active');
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

  function usageTabVisible() {
    var panel = document.getElementById('tab-usage');
    return !!(panel && panel.classList.contains('active'));
  }

  function pollAccounts() {
    if (!key() || !accountsTabVisible()) return;
    call(BASE + '/accounts').then(function (d) {
      var stamp = document.getElementById('accountsStamp');
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
      setTimeout(function () { location.reload(); }, 2500);
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
  // runAccountCheckin signs in the single account behind a row button.
  //
  // The per-account button exists so an operator does not have to sign in the
  // whole pool (and wait for its upstream traffic) to test one credential.
  window.runAccountCheckin = function (uid, button) {
    if (!uid) return;
    var original = button ? button.textContent : '';
    if (button) {
      button.disabled = true;
      button.textContent = '签到中';
    }
    call('/checkin/run?uid=' + encodeURIComponent(uid), { method: 'POST' })
      .then(function (data) {
        var accounts = (data && data.accounts) || [];
        var hit = accounts[0] || {};
        var message = hit.error ? ('失败：' + hit.error)
                                : ('完成' + (hit.message ? '（' + hit.message + '）' : ''));
        toast(message, hit.error ? 'bad' : 'ok');
      })
      .catch(function (err) { toast('签到失败：' + err.message, 'bad'); })
      .then(function () {
        if (button) {
          button.disabled = false;
          button.textContent = original;
        }
      });
  };

  // runAccountQuota refreshes one account's credit reading.
  window.runAccountQuota = function (uid, button) {
    if (!uid) return;
    var original = button ? button.textContent : '';
    if (button) {
      button.disabled = true;
      button.textContent = '查询中';
    }
    call('/quota/refresh?uid=' + encodeURIComponent(uid), { method: 'POST' })
      .then(function (data) {
        var results = (data && data.results) || [];
        var hit = results[0] || {};
        var message = hit.error ? ('积分查询失败：' + hit.error)
                                : ('剩余积分 ' + (hit.credits != null ? hit.credits : '未知'));
        toast(message, hit.error ? 'bad' : 'ok');
      })
      .catch(function (err) { toast('积分查询失败：' + err.message, 'bad'); })
      .then(function () {
        if (button) {
          button.disabled = false;
          button.textContent = original;
        }
      });
  };

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

  document.addEventListener('click', function (ev) {
    var node = ev.target;
    while (node && node !== document) {
      if (node.hasAttribute && node.hasAttribute('data-account-toggle')) {
        ev.preventDefault();
        toggleAccount(node.getAttribute('data-uid'), node.getAttribute('data-action'), node.getAttribute('data-auth-index') || '');
        return;
      }
      if (node.hasAttribute && node.hasAttribute('data-task-toggle')) {
        ev.preventDefault();
        toggleAccountTask(node.getAttribute('data-uid'), node.getAttribute('data-action'));
        return;
      }
      if (node.hasAttribute && node.hasAttribute('data-account-checkin')) {
        ev.preventDefault();
        runAccountCheckin(node.getAttribute('data-uid'), node);
        return;
      }
      if (node.hasAttribute && node.hasAttribute('data-account-quota')) {
        ev.preventDefault();
        runAccountQuota(node.getAttribute('data-uid'), node);
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
