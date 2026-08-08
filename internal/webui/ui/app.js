const state = { rules: [], filter: '' };

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

// --- forwarding path diagram ----------------------------------------------

function truncate(s, n) {
  s = String(s);
  return s.length > n ? s.slice(0, n - 1) + '…' : s;
}

// Builds an animated SVG showing the forwarding path:
//  [来源接入] -> [监听端口] -> [转发器] -> [目标服务]
function pathDiagram(opts) {
  const listen = opts.listen;
  const target = opts.target;
  const listenLabel = truncate(opts.listenLabel || listen, 14);
  const targetLabel = truncate(opts.targetLabel || target, 14);

  // node coordinates
  const srcBox  = { x: 8,   y: 78, w: 150, h: 96 };
  const lstBox  = { x: 200, y: 78, w: 130, h: 96 };
  const relBox  = { x: 372, y: 52, w: 96,  h: 148 };
  const dstBox  = { x: 510, y: 78, w: 122, h: 96 };

  const ly = 126; // lane y for arrows

  const node = (n, fill, label, sub) => `
    <g>
      <rect x="${n.x}" y="${n.y}" width="${n.w}" height="${n.h}" rx="14"
            fill="${fill}" stroke="var(--muted)" stroke-width="1.5"/>
      <text x="${n.x + n.w / 2}" y="${n.y + 40}" text-anchor="middle" class="pb-node">${label}</text>
      <text x="${n.x + n.w / 2}" y="${n.y + 66}" text-anchor="middle" class="pb-sub">${sub}</text>
    </g>`;

  const arrow = (x1, x2, y) => `
    <line x1="${x1}" y1="${y}" x2="${x2}" y2="${y}"
          stroke="var(--muted)" stroke-width="2" marker-end="url(#pb-head)"/>`;

  const flow = (x1, x2, y, begin) => `
    <circle r="4.5" fill="var(--accent)">
      <animateMotion dur="1.6s" begin="${begin}s" repeatCount="indefinite"
        path="M ${x1} ${y} H ${x2}" keyPoints="0;1" keyTimes="0;1"/>
    </circle>`;

  return `
  <svg class="path-diagram" viewBox="0 0 640 200" xmlns="http://www.w3.org/2000/svg" role="img">
    <defs>
      <marker id="pb-head" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
        <path d="M 0 1 L 9 5 L 0 9 z" fill="var(--muted)"/>
      </marker>
    </defs>

    ${node(srcBox, 'var(--panel)', '接入', 'client')}
    ${node(lstBox, 'var(--panel)', '监听', truncate(listen, 10))}
    ${node(relBox, 'var(--accent)', '转发器', 'pf')}
    ${node(dstBox, 'var(--panel)', '目标', targetLabel)}

    ${arrow(srcBox.x + srcBox.w, lstBox.x, ly)}
    ${arrow(lstBox.x + lstBox.w, relBox.x + 8, ly)}
    ${arrow(relBox.x + relBox.w, dstBox.x, ly + 56)}

    ${flow(srcBox.x + srcBox.w, lstBox.x, ly, 0)}
    ${flow(srcBox.x + srcBox.w, lstBox.x, ly, 0.8)}
    ${flow(lstBox.x + lstBox.w, relBox.x + 8, ly, 0.3)}
    ${flow(lstBox.x + lstBox.w, relBox.x + 8, ly, 1.1)}
    ${flow(relBox.x + relBox.w, dstBox.x, ly + 56, 0.2)}
    ${flow(relBox.x + relBox.w, dstBox.x, ly + 56, 1.0)}
  </svg>`;
}

// --- list rendering -----------------------------------------------------

async function refresh() {
  const rules = await api('/api/rules');
  render(rules);
}

function render(rules) {
  const container = $('rules');
  container.innerHTML = '';

  if (!rules.length) {
    const empty = document.createElement('div');
    empty.className = 'empty';
    empty.textContent = '暂无规则，请在左侧添加。';
    container.appendChild(empty);
    return;
  }

  const list = document.createElement('div');
  list.className = 'rule-list';

  for (const rule of rules) {
    const card = document.createElement('article');
    card.className = 'rule-card';

    const head = document.createElement('div');
    head.className = 'rule-head';
    head.innerHTML = `
      <span class="rule-name">${escapeHtml(rule.name || rule.id)}</span>
      <span class="status ${rule.status}">${escapeHtml(rule.status)}</span>`;
    card.appendChild(head);

    card.insertAdjacentHTML('beforeend', pathDiagram({
      listen: rule.listen,
      target: rule.target,
    }));

    const foot = document.createElement('div');
    foot.className = 'rule-foot';
    foot.innerHTML = `
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

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

// --- live preview in the add form ------------------------------------------

function updatePreview() {
  $('path-preview').innerHTML = pathDiagram({
    listen: $('listen').value.trim() || '…',
    target: $('target').value.trim() || '…',
  });
}

for (const id of ['listen', 'target']) {
  $(id).addEventListener('input', updatePreview);
}

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  try {
    const payload = {
      name: $('name').value.trim(),
      listen: $('listen').value.trim(),
      target: $('target').value.trim(),
    };
    await api('/api/rules', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    form.reset();
    updatePreview();
    await refresh();
  } catch (err) {
    setStatus(false, 'error');
    alert('添加失败: ' + err.message);
  }
});

// --- boot --------------------------------------------------------------------

async function boot() {
  try {
    await refresh();
  } catch (err) {
    setStatus(false, 'offline');
  }
  setInterval(() => {
    api('/api/rules').then(render).catch(() => setStatus(false, 'offline'));
  }, 3000);
}

updatePreview();
boot();