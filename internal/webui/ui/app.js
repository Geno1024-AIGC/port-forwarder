const state = { rules: [], filter: '' };

const $ = (id) => document.getElementById(id);

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

  const table = document.createElement('table');
  const thead = document.createElement('thead');
  thead.innerHTML = '<tr><th>名称</th><th>监听地址</th><th>目标地址</th><th>状态</th><th></th></tr>';
  table.appendChild(thead);

  const tbody = document.createElement('tbody');
  for (const rule of rules) {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${escapeHtml(rule.name || rule.id)}</td>
      <td class="mono">${escapeHtml(rule.listen)}</td>
      <td class="mono">${escapeHtml(rule.target)}</td>
      <td><span class="status ${rule.status}">${escapeHtml(rule.status)}</span></td>
      <td class="actions">
        <button class="danger" data-del="${escapeHtml(rule.id)}">删除</button>
      </td>`;
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);
  container.appendChild(table);

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
    await refresh();
  } catch (err) {
    setStatus(false, 'error');
    alert('添加失败: ' + err.message);
  }
});

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

boot();