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
  const listen = truncate(opts.listen || '—', 14);
  const target = truncate(opts.target || '—', 14);
  const status = opts.status;

  const nodes = typ === 'remote'
    ? [
        { label: '接入', sub: 'public', x: 24, y: 88, w: 110, h: 96 },
        { label: '公网监听', sub: listen, x: 180, y: 88, w: 150, h: 96 },
        { label: '隧道', sub: 'tunnel', x: 376, y: 52, w: 96, h: 148, accent: true },
        { label: '转发器', sub: 'pf', x: 520, y: 88, w: 90, h: 96 },
        { label: '目标', sub: target, x: 648, y: 88, w: 130, h: 96 },
      ]
    : [
        { label: '接入', sub: 'client', x: 8,   y: 88, w: 96,  h: 96 },
        { label: '监听', sub: listen, x: 150, y: 88, w: 160, h: 96 },
        { label: '转发器', sub: 'pf', x: 356, y: 52, w: 96, h: 148, accent: true },
        { label: '目标', sub: target, x: 498, y: 88, w: 130, h: 96 },
      ];

  const W = typ === 'remote' ? 808 : 660;
  const lane = typ === 'remote' ? 198 : 136;
  const stroke = status === 'error' ? 'var(--err)' : 'var(--muted)';

  const nodeSvg = (n) => `
    <g>
      <rect x="${n.x}" y="${n.y}" width="${n.w}" height="${n.h}" rx="14"
            fill="${n.accent ? 'var(--accent)' : 'var(--panel)'}" stroke="${stroke}" stroke-width="1.5"/>
      <text x="${n.x + n.w / 2}" y="${n.y + 42}" text-anchor="middle" class="pb-node">${escapeHtml(n.label)}</text>
      <text x="${n.x + n.w / 2}" y="${n.y + 70}" text-anchor="middle" class="pb-sub">${escapeHtml(n.sub)}</text>
    </g>`;

  const segments = [];
  for (let i = 0; i < nodes.length - 1; i++) {
    const a = nodes[i], b = nodes[i + 1];
    const x1 = a.x + a.w, x2 = b.x;
    segments.push(`
      <line x1="${x1}" y1="${lane}" x2="${x2}" y2="${lane}" stroke="${stroke}" stroke-width="2"
            marker-end="url(#pf-head)"/>
      <g>${[0, 0.8].map((off, k) => `
        <circle r="4" fill="var(--accent)">
          <animateMotion dur="1.6s" begin="${(i * 0.6 + k * 0.4).toFixed(1)}s" repeatCount="indefinite"
            path="M ${x1} ${lane} H ${x2}"/>
        </circle>`).join('')}</g>`);
  }

  return `
  <svg class="path-diagram" viewBox="0 0 ${W} 210" xmlns="http://www.w3.org/2000/svg" role="img">
    <defs>
      <marker id="pf-head" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
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
      <button class="danger" data-del="${escapeHtml(rule.id)}">删除</button>`;
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

for (const id of ['listen', 'target']) {
  $(id).addEventListener('input', updatePreviewFill);
}
$('goal-type').addEventListener('change', syncTypeLabels);

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
    const rule = await api('/api/rules', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    form.reset();
    if (typeEl) typeEl.value = rule.type || 'local';
    updatePreviewFill();
    await refresh();
  } catch (err) {
    errBox.textContent = '添加失败: ' + (err.message || 'unknown error');
    errBox.hidden = false;
    setStatus(false, 'error');
  }
});

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
updatePreviewFill();
boot();