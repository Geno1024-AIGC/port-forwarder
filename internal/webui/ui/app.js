const $ = (id) => document.getElementById(id);
const form = $('add-form');

async function api(path, options = {}) {
  const res = await fetch(path, options);
  if (!res.ok) {
    const detail = await res.text();
    throw new Error(`${res.status} ${detail}`);
  }
  if (res.status === 204) return null;
  return res.json();
}

function setStatus(ok, text) {
  const badge = $('conn-status');
  badge.className = 'badge ' + (ok ? 'ok' : 'err');
  badge.textContent = text;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function truncate(s, n) {
  s = String(s);
  return s.length > n ? s.slice(0, n - 1) + '…' : s;
}

// ——— forwarding path diagram —————————————————————

// Renders an SVG showing how a rule forwards.
//  "local":  client -> listen -> pf -> target
//  "remote": public -> server listen -> tunnel -> pf -> target
function renderDiagram(opts) {
  const typ = opts.typ || 'local';
  const listen = truncate(opts.listen || '—', 16);
  const target = truncate(opts.target || '—', 16);
  const status = opts.status;
  const stroke = status === 'error' ? 'var(--err)' : 'var(--muted)';

  const span = typ === 'remote'
    ? [
        { label: '接入', sub: 'public', w: 66 },
        { label: '公网监听', sub: listen, w: 66 },
        { label: '隧道', sub: 'tunnel', w: 66, accent: true },
        { label: '转发器', sub: 'pf', w: 66 },
        { label: '目标', sub: target, w: 84 },
      ]
    : [
        { label: '接入', sub: 'client', w: 64 },
        { label: '监听', sub: listen, w: 64 },
        { label: '转发器', sub: 'pf', w: 64, accent: true },
        { label: '目标', sub: target, w: 84 },
      ];

  // All nodes share one vertical center so the connectors line up.
  const LANE = 54;
  const GAP = 18;
  const H = LANE + 24; // extends below the lane for the arrow heads
  const W = span.reduce((sum, n) => sum + n.w, 0) + GAP * span.length + 8;

  let x = 4;
  const nodes = span.map((n) => {
    const o = { ...n, x, y: LANE - 24, h: 40 };
    x += n.w + GAP;
    return o;
  });

  const nodeSvg = (n) => `
    <g>
      <rect x="${n.x}" y="${n.y}" width="${n.w}" height="${n.h}" rx="9"
            fill="${n.accent ? 'var(--accent)' : 'var(--panel)'}" stroke="${stroke}" stroke-width="1.6"/>
      <text x="${n.x + n.w / 2}" y="${LANE - 7}" text-anchor="middle" class="pb-node">${escapeHtml(n.label)}</text>
      <text x="${n.x + n.w / 2}" y="${LANE + 9}" text-anchor="middle" class="pb-sub">${escapeHtml(n.sub)}</text>
    </g>`;

  const segments = [];
  for (let i = 0; i < nodes.length - 1; i++) {
    const a = nodes[i], b = nodes[i + 1];
    const x1 = a.x + a.w, x2 = b.x;
    segments.push(`
      <line x1="${x1}" y1="${LANE}" x2="${x2}" y2="${LANE}" stroke="${stroke}" stroke-width="1.8"
            marker-end="url(#pf-head)"/>
      <g>${[0, 0.8].map((off, k) => `
        <circle r="2.6" fill="var(--accent)">
          <animateMotion dur="${Math.max(0.8, (x2 - x1) / 300).toFixed(1)}s" begin="${(i * 0.5 + k * 0.35).toFixed(2)}s" repeatCount="indefinite"
            path="M ${x1} ${LANE} H ${x2}"/>
        </circle>`).join('')}</g>`);
  }

  return `
  <svg class="path-diagram" viewBox="0 0 ${W} ${H}" xmlns="http://www.w3.org/2000/svg" role="img">
    <defs>
      <marker id="pf-head" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
        <path d="M 0 1 L 9 5 L 0 9 z" fill="${stroke}"/>
      </marker>
    </defs>
    ${nodes.map(nodeSvg).join('')}
    ${segments.join('')}
  </svg>`;
}

// ——— rule list —————————————————

async function refresh() {
  render(await api('/api/rules'));
}

function render(rules) {
  const container = $('rules');
  container.innerHTML = '';

  if (!rules.length) {
    const empty = document.createElement('div');
    empty.className = 'empty';
    empty.textContent = '暂无规则，请按上面的路径图填写。';
    container.appendChild(empty);
    return;
  }

  const list = document.createElement('div');
  list.className = 'rule-list';

  for (const rule of rules) {
    const listStr = rule.listen || '—';
    const targetStr = rule.target || '—';

    const card = document.createElement('article');
    card.className = 'rule-card';
    card.innerHTML = `
      <div class="rule-head">
        <span class="rule-name">${escapeHtml(rule.name || rule.id)}</span>
        <span class="rule-type">${escapeHtml(rule.type || 'local')}</span>
        <span class="status ${rule.status}">${escapeHtml(rule.status)}</span>
      </div>`;
    card.insertAdjacentHTML('beforeend', renderDiagram({
      typ: rule.type,
      listen: listStr,
      target: targetStr,
      status: rule.status,
    }));
    const foot = document.createElement('div');
    foot.className = 'rule-foot';
    foot.innerHTML = `
      <span class="mono">${escapeHtml(listStr)} → ${escapeHtml(targetStr)}</span>
      <span class="rule-actions">
        <button class="edit" data-edit-rule="${escapeHtml(rule.id)}">编辑</button>
        <button class="danger" data-del="${escapeHtml(rule.id)}">删除</button>
      </span>`;
    card.appendChild(foot);

    list.appendChild(card);
  }

  container.appendChild(list);

  for (const btn of container.querySelectorAll('[data-del]')) {
    btn.addEventListener('click', async () => {
      await api('/api/rules/' + btn.dataset.del, { method: 'DELETE' });
      await refresh();
    });
  }
  for (const btn of container.querySelectorAll('[data-edit-rule]')) {
    btn.addEventListener('click', () => enterRuleEdit(btn.dataset.editRule));
  }
}

// ——— rule editing ————————————

let editingRuleID = null;

function enterRuleEdit(id) {
  const rules = [];
  $('rules').querySelectorAll('.rule-card');
  api('/api/rules').then((list) => {
    const rule = list.find((r) => r.id === id);
    if (!rule) return;
    editingRuleID = rule.id;
    $('goal-type').value = rule.type || 'local';
    $('name').value = rule.name || '';
    $('listen').value = rule.listen || '';
    $('target').value = rule.target || '';
    syncTypeLabels();
    syncCredRow();
    updatePreviewFill();
    const credSelect = $('credential');
    if (rule.type === 'remote' && rule.credential && credSelect) {
      credSelect.value = rule.credential;
    }
    $('form-submit-label').textContent = '更新规则';
    $('form-cancel').hidden = false;
    $('view-rules').scrollIntoView({ behavior: 'smooth', block: 'start' });
    $('name').focus();
  });
}

function exitRuleEdit() {
  editingRuleID = null;
  const form = $('add-form');
  form.reset();
  $('goal-type').value = 'local';
  syncTypeLabels();
  syncCredRow();
  updatePreviewFill();
  if ($('form-submit-label')) $('form-submit-label').textContent = '添加';
  $('form-cancel').hidden = true;
}

// ——— form —————————————————

function updatePreviewFill() {
  const listen = ($('listen') || {}).value || '';
  const target = ($('target') || {}).value || '';
  if ($('listen')) $('listen').classList.toggle('filled', !!listen.trim());
  if ($('target')) $('target').classList.toggle('filled', !!target.trim());
}

// Reflect the chosen forwarding kind on the in-form path diagram.
function syncTypeLabels() {
  const typ = ($('goal-type') || {}).value || 'local';
  if (typ === 'remote') {
    $('path-src').textContent = 'internet';
    $('path-listen-kicker').textContent = '公网监听';
    $('path-relay-kicker').textContent = '隧道';
    $('listen').placeholder = ':8080 (公网)';
    $('target').placeholder = '192.168.1.2:7777 (内网)';
  } else {
    $('path-src').textContent = '客户端';
    $('path-listen-kicker').textContent = '监听地址';
    $('path-relay-kicker').textContent = '转发器';
    $('listen').placeholder = ':8080';
    $('target').placeholder = '127.0.0.1:3000';
  }
}

function syncCredRow() {
  const typ = ($('goal-type') || {}).value || 'local';
  $('cred-row').hidden = typ !== 'remote';
}

for (const id of ['listen', 'target']) {
  $(id).addEventListener('input', updatePreviewFill);
}
$('goal-type').addEventListener('change', () => { syncTypeLabels(); syncCredRow(); });

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  const errBox = $('form-error');
  errBox.hidden = true;
  try {
    const typeEl = $('goal-type');
    const payload = {
      type: typeEl ? typeEl.value : 'local',
      name: $('name').value.trim(),
      listen: $('listen').value.trim(),
      target: $('target').value.trim(),
    };
    if (payload.type === 'remote') {
      const cred = $('credential').value;
      if (!cred) {
        errBox.textContent = '请到「认证信息」页面先添加并选择认证信息。';
        errBox.hidden = false;
        return;
      }
      payload.credential = cred;
    }
    const opts = {
      method: editingRuleID ? 'PUT' : 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    };
    const url = editingRuleID ? '/api/rules/' + editingRuleID : '/api/rules';
    const rule = await api(url, opts);
    exitRuleEdit();
    if (typeEl) typeEl.value = rule.type || 'local';
    updatePreviewFill();
    await refresh();
  } catch (err) {
    errBox.textContent = '保存失败: ' + (err.message || 'unknown error');
    errBox.hidden = false;
    setStatus(false, 'error');
  }
});

$('form-cancel').addEventListener('click', () => {
  exitRuleEdit();
  $('form-error').hidden = true;
});

// ——— credentials ————————————

async function refreshCreds() {
  const creds = await api('/api/credentials');
  populateCredSelect(creds);
  renderCreds(creds);
}

function populateCredSelect(creds) {
  const sel = $('credential');
  sel.innerHTML = '';
  if (!creds.length) {
    const o = document.createElement('option');
    o.value = '';
    o.textContent = '（暂无认证，请先到下方添加）';
    sel.appendChild(o);
  } else {
    for (const c of creds) {
      const o = document.createElement('option');
      o.value = c.id;
      o.textContent = `${c.name} (${c.host})`;
      sel.appendChild(o);
    }
  }
}

function renderCreds(creds) {
  const container = $('creds');
  container.innerHTML = '';
  if (!creds.length) {
    const empty = document.createElement('div');
    empty.className = 'empty';
    empty.textContent = '暂无认证信息。';
    container.appendChild(empty);
    return;
  }
  const list = document.createElement('div');
  list.className = 'cred-list';
  for (const c of creds) {
    const row = document.createElement('div');
    row.className = 'cred-item';
    row.innerHTML = `
      <span class="cred-name">${escapeHtml(c.name || c.id)}</span>
      <span class="mono">${escapeHtml(c.user)}@${escapeHtml(c.host)}</span>
      <span class="badge">${escapeHtml(c.authType || 'password')}</span>
      <button class="probe" data-probe="${escapeHtml(c.id)}">测试</button>
      <button class="edit" data-edit-cred="${escapeHtml(c.id)}">编辑</button>
      <button class="danger" data-del-cred="${escapeHtml(c.id)}">删除</button>
      <span class="probe-result" data-probe-out></span>`;
    list.appendChild(row);
  }
  container.appendChild(list);

  for (const btn of container.querySelectorAll('[data-probe]')) {
    btn.addEventListener('click', async () => {
      const out = rowOf(btn);
      out.textContent = '测试中…';
      try {
        await api('/api/credentials/' + btn.dataset.probe + '/probe', { method: 'POST' });
        out.textContent = '成功';
        out.classList.add('ok');
      } catch (err) {
        out.textContent = '失败: ' + (err.message || 'unknown');
        out.classList.add('err');
      }
    });
  }
  for (const btn of container.querySelectorAll('[data-del-cred]')) {
    btn.addEventListener('click', async () => {
      try {
        await api('/api/credentials/' + btn.dataset.delCred, { method: 'DELETE' });
        await refreshCreds();
      } catch (err) {
        alert('删除失败: ' + (err.message || err));
      }
    });
  }
  for (const btn of container.querySelectorAll('[data-edit-cred]')) {
    btn.addEventListener('click', () => editCredential(btn.dataset.editCred));
  }
}

function rowOf(btn) {
  return btn.closest('.cred-item').querySelector('[data-probe-out]');
}

function currentAuthType() {
  const el = document.querySelector('input[name="c-auth"]:checked');
  return el ? el.value : 'password';
}

function syncAuthRows() {
  const key = currentAuthType() === 'key';
  $('c-key-row').hidden = !key;
  $('c-pass-row').hidden = key;
}

for (const el of document.querySelectorAll('input[name="c-auth"]')) {
  el.addEventListener('change', () => {
    syncAuthRows();
    resetKeyPick();
  });
}

// read picked key content (primary input is the typed path; a picked file
// uploads content instead since browsers hide the real absolute path).
let pickedKey = null;

let editingCredID = null;

function resetKeyPick() {
  $('c-keyfile').value = '';
  pickedKey = null;
  $('c-key-hint').hidden = true;
}

function enterEditMode(cred) {
  editingCredID = cred.id;
  $('c-name').value = cred.name || '';
  $('c-host').value = cred.host || '';
  $('c-user').value = cred.user || '';
  const radio = document.querySelector(`input[name="c-auth"][value="${cred.authType || 'password'}"]`);
  if (radio) radio.checked = true;
  syncAuthRows();
  $('c-keypath').value = cred.authType === 'key' ? (cred.keyPath || '') : '';
  resetKeyPick();
  $('cred-submit').textContent = '更新认证';
  $('cred-cancel').hidden = false;
  $('view-creds').scrollIntoView({ behavior: 'smooth', block: 'start' });
  $('c-name').focus();
}

function exitEditMode() {
  editingCredID = null;
  $('cred-form').reset();
  syncAuthRows();
  resetKeyPick();
  $('cred-submit').textContent = '保存认证';
  $('cred-cancel').hidden = true;
}

async function editCredential(id) {
  try {
    const list = await api('/api/credentials');
    const cred = list.find((c) => c.id === id);
    if (!cred) throw new Error('未找到该认证');
    enterEditMode(cred);
  } catch (err) {
    alert('读取失败: ' + (err.message || err));
  }
}

$('cred-cancel').addEventListener('click', () => {
  exitEditMode();
  $('cred-error').hidden = true;
});

// The path box is the primary input. Picking a file is a shortcut that
// uploads the key content instead (browsers cannot hand over the real path).
$('c-keyfile').addEventListener('change', async () => {
  const file = ($('c-keyfile').files || [])[0];
  if (!file) {
    resetKeyPick();
    return;
  }
  pickedKey = new Uint8Array(await file.arrayBuffer());
  const hint = $('c-key-hint');
  hint.textContent = '将上传所选文件，保存到本机 pf 目录。';
  hint.hidden = false;
});

$('c-keypath').addEventListener('input', () => {
  if ($('c-keyfile').files.length) {
    $('c-keyfile').value = '';
    pickedKey = null;
  }
  $('c-key-hint').hidden = true;
});

$('cred-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const errBox = $('cred-error');
  errBox.hidden = true;
  const authType = currentAuthType();
  const payload = {
    name: $('c-name').value.trim(),
    host: $('c-host').value.trim(),
    user: $('c-user').value.trim(),
    authType: authType,
  };
  if (authType === 'key') {
    if (pickedKey) {
      payload.keyContent = Array.from(pickedKey);
    } else {
      payload.keyPath = $('c-keypath').value.trim();
    }
  } else {
    payload.password = $('c-pass').value;
  }
  try {
    const opts = {
      method: editingCredID ? 'PUT' : 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    };
    const url = editingCredID ? '/api/credentials/' + editingCredID : '/api/credentials';
    await api(url, opts);
    exitEditMode();
    await refreshCreds();
  } catch (err) {
    errBox.textContent = '保存失败: ' + (err.message || 'unknown error');
    errBox.hidden = false;
  }
});

// ——— sidebar view switching ————————————

function switchView(view) {
  const showRules = view === 'rules';
  $('view-rules').hidden = !showRules;
  $('view-creds').hidden = showRules;
  for (const btn of document.querySelectorAll('.nav-btn')) {
    btn.classList.toggle('active', btn.dataset.view === view);
  }
}

for (const btn of document.querySelectorAll('.nav-btn')) {
  btn.addEventListener('click', () => switchView(btn.dataset.view));
}

// ——— boot —————————————————

async function boot() {
  try {
    await refresh();
    setStatus(true, 'connected');
  } catch (err) {
    setStatus(false, 'offline');
  }
  setInterval(() => {
    api('/api/rules').then(render).catch(() => setStatus(false, 'offline'));
  }, 3000);
}

syncTypeLabels();
syncCredRow();
syncAuthRows();
updatePreviewFill();
refreshCreds();
boot();