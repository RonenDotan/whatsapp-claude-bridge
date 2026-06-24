'use strict';

// ── Config ────────────────────────────────────────────────────────────────────
const STATUS_REFRESH = 30_000;
const LOG_LIMIT      = 60;

// ── State ─────────────────────────────────────────────────────────────────────
const S = {
  page:          'dashboard',
  status:        null,
  chats:         [],
  health:        null,
  server:        null,
  accordionOpen: {},
  permissions:   {},  // {chatID: {level, custom_allow}}
  // chat detail
  selectedChat:  null,
  chatLog:       [],
  chatSending:   false,
};

let refreshTimer = null;

// ── API ───────────────────────────────────────────────────────────────────────
async function api(path, opts = {}) {
  const res = await fetch(path, { headers: { 'Content-Type': 'application/json' }, ...opts });
  if (!res.ok) { const b = await res.text(); throw new Error(b || `HTTP ${res.status}`); }
  return res.json();
}
const postCmd  = (id, cmd)  => api('/api/cmd',  { method: 'POST', body: JSON.stringify({ chat_id: id, command: cmd }) });
const postSend = (id, text) => api('/api/send', { method: 'POST', body: JSON.stringify({ chat_id: id, text }) });

// ── Loaders ───────────────────────────────────────────────────────────────────
async function loadStatus()  { S.status = await api('/api/status'); updateSidebarVersion(); }
async function loadChats()   { S.chats  = await api('/api/chats'); }
async function loadHealth()  { S.health = await api('/api/health'); }
async function loadServer()  { S.server = await api('/api/server'); }
async function loadChatLog(chatID) {
  const entries = await api(`/api/log?chat=${encodeURIComponent(chatID)}&limit=${LOG_LIMIT}`) || [];
  S.chatLog = entries.slice().reverse(); // API returns newest-first; we want oldest-first
}
async function loadPermissions() {
  S.permissions = await api('/api/permissions') || {};
}

function updateSidebarVersion() {
  const el = document.getElementById('sidebar-version');
  if (el && S.status) el.textContent = `v${S.status.version || '?'}`;
}

// ── Router ────────────────────────────────────────────────────────────────────
const pageLoaders = {
  health: () => !S.health && loadHealth(),
  server: () => !S.server && loadServer(),
};

async function navTo(page) {
  clearTimeout(refreshTimer);
  S.selectedChat = null; // reset detail view on page change
  const loader = pageLoaders[page];
  if (loader) { try { await loader(); } catch(e) { console.error(e); } }
  navigate(page);
}

function navigate(page) {
  if (!pages[page]) page = 'dashboard';
  S.page = page;
  history.replaceState(null, '', '#' + page);
  updateNavActive();
  renderPage();
}

function updateNavActive() {
  document.querySelectorAll('.nav-link, .bn-link').forEach(a =>
    a.classList.toggle('active', a.dataset.page === S.page)
  );
}

// ── Render ────────────────────────────────────────────────────────────────────
const pages = { dashboard, chats, health, server };

function renderPage() {
  clearTimeout(refreshTimer);
  document.getElementById('content').innerHTML = pages[S.page]();
  bindPageEvents();
  scheduleRefresh();
}

function scheduleRefresh() {
  refreshTimer = setTimeout(async () => {
    try {
      if (S.selectedChat)              { await loadChatLog(S.selectedChat.chat_id); }
      else if (S.page === 'dashboard') { await loadStatus(); }
      else if (S.page === 'chats')     { await loadChats(); }
      else if (S.page === 'health')    { await loadHealth(); }
      renderPage();
    } catch (_) {}
  }, STATUS_REFRESH);
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function fmtTime(ts) {
  const d = new Date(ts), now = new Date();
  if (d.toDateString() === now.toDateString())
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' +
         d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}
function fmtCost(usd) {
  if (!usd) return null;
  return usd < 0.01 ? `$${(usd * 100).toFixed(3)}¢` : `$${usd.toFixed(4)}`;
}
function dot(ok) {
  return `<span class="dot ${ok ? 'green pulse' : 'red'}"></span>`;
}
function badge(type) {
  return `<span class="badge ${type === 'codex' ? 'badge-codex' : 'badge-claude'}">${type || 'claude'}</span>`;
}
const PERM_LABELS = { standard: 'Std', developer: 'Dev', god: 'God', custom: 'Custom' };
function permBadge(chatID) {
  const p = S.permissions[chatID];
  const lvl = p ? p.level : 'standard';
  const cls = lvl === 'god' ? 'badge-perm-god' : lvl === 'developer' ? 'badge-perm-dev' : lvl === 'custom' ? 'badge-perm-custom' : 'badge-perm-std';
  return `<span class="badge ${cls}">${PERM_LABELS[lvl] || lvl}</span>`;
}
function chanIcon(ch) { return ch === 'signal' ? '📡' : '💬'; }

function escHtml(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}
function escAttr(s) { return String(s).replace(/"/g,'&quot;').replace(/'/g,'&#39;'); }
function cssID(s)   { return s.replace(/[^a-zA-Z0-9_-]/g,'_'); }

// Toast
function toast(msg, type = 'success') {
  const el = document.createElement('div');
  el.className = `toast ${type}`;
  el.textContent = msg;
  document.getElementById('toast-container').appendChild(el);
  requestAnimationFrame(() => {
    el.classList.add('show');
    setTimeout(() => { el.classList.remove('show'); setTimeout(() => el.remove(), 250); }, 3000);
  });
}

// Modal
function showModal(title, text) {
  document.getElementById('modal-title').textContent = title;
  document.getElementById('modal-body').textContent  = text;
  document.getElementById('modal').classList.remove('hidden');
}
function closeModal() { document.getElementById('modal').classList.add('hidden'); }

// ── Dashboard ─────────────────────────────────────────────────────────────────
function dashboard() {
  if (!S.status) return spinner();
  const st = S.status, ch = st.channels || {};
  const wa = ch.whatsapp || {}, sig = ch.signal || {};
  return `
<div class="page-title">
  <svg viewBox="0 0 24 24" fill="currentColor"><path d="M3 13h8V3H3zm0 8h8v-6H3zm10 0h8V11h-8zm0-18v6h8V3z"/></svg>
  Dashboard
  <button class="btn btn-ghost btn-sm" id="btn-refresh" style="margin-left:auto">↻</button>
</div>
<div class="section grid-3">
  <div class="stat-card"><div class="stat-label">Version</div><div class="stat-value" style="font-size:18px">${escHtml(st.version||'—')}</div></div>
  <div class="stat-card"><div class="stat-label">Uptime</div><div class="stat-value" style="font-size:18px">${escHtml(st.uptime||'—')}</div></div>
  <div class="stat-card"><div class="stat-label">Active Chats</div><div class="stat-value">${S.chats.length}</div></div>
</div>
<div class="section card">
  <div class="card-header"><span class="card-title">Channels</span></div>
  ${channelRow('WhatsApp', wa)}
  ${channelRow('Signal', sig)}
</div>`;
}

function channelRow(label, state) {
  return `
<div class="channel-row">
  <div>
    <div class="channel-name">${dot(state.Connected)} ${escHtml(label)}</div>
    <div class="channel-account">${state.AccountID ? escHtml(state.AccountID) : 'Not connected'}</div>
  </div>
  <span class="badge ${state.Connected ? 'badge-ok' : 'badge-error'}">${state.Connected ? 'Online' : 'Offline'}</span>
</div>`;
}

// ── Chats (list + detail) ─────────────────────────────────────────────────────
function chats() {
  return S.selectedChat ? chatDetail(S.selectedChat) : chatList();
}

function chatList() {
  if (!S.chats.length) return `
<div class="page-title"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M20 2H4C2.9 2 2 2.9 2 4v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2z"/></svg>Chats</div>
<div class="empty"><div class="empty-icon">💬</div>No allowed chats yet.</div>`;

  const groups = {};
  for (const c of S.chats) {
    const ch = c.channel || (c.chat_id.includes('@') ? 'whatsapp' : 'signal');
    if (!groups[ch]) groups[ch] = [];
    groups[ch].push(c);
  }

  const channelOrder = ['whatsapp', 'signal'];
  const sections = channelOrder.filter(k => groups[k]).map(ch => {
    // sort by last message desc, unseen chats last
    const sorted = groups[ch].slice().sort((a, b) => (b.last_ts || 0) - (a.last_ts || 0));
    const isOpen = S.accordionOpen[ch] !== false; // default open
    const label  = ch === 'whatsapp' ? '💬 WhatsApp' : '📡 Signal';
    const rows   = sorted.map(c => chatRow(c)).join('');
    return `
<div class="chan-group">
  <div class="chan-group-header" data-channel="${ch}">
    <span>${label} <span class="chan-count">${sorted.length}</span></span>
    <svg class="accordion-arrow ${isOpen ? 'open' : ''}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="6 9 12 15 18 9"/></svg>
  </div>
  <div class="chan-group-body ${isOpen ? 'open' : ''}">
    <div class="chat-list">${rows}</div>
  </div>
</div>`;
  }).join('');

  return `
<div class="page-title">
  <svg viewBox="0 0 24 24" fill="currentColor"><path d="M20 2H4C2.9 2 2 2.9 2 4v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2z"/></svg>
  Chats (${S.chats.length})
  <button class="btn btn-ghost btn-sm" id="btn-refresh-chats" style="margin-left:auto">↻</button>
</div>
${sections}`;
}

function chatRow(c) {
  const icon    = c.icon || (c.chat_id.endsWith('@g.us') ? '👥' : '👤');
  const st      = c.stats || c.codex_stats;
  const cost    = st ? fmtCost(st.total_cost_usd || st.TotalCostUSD) : null;
  const lastMsg = c.last_ts ? fmtTime(c.last_ts) : null;
  const countStr = c.msg_count ? `${c.msg_count} msg${c.msg_count !== 1 ? 's' : ''}` : null;
  const sub      = [c.personality, cost, countStr].filter(Boolean).join(' · ');
  return `
<div class="chat-row" data-chatid="${escAttr(c.chat_id)}">
  <div class="chat-row-icon">${icon}</div>
  <div class="chat-row-body">
    <div class="chat-row-name">${escHtml(c.name || c.chat_id)}</div>
    ${sub ? `<div class="chat-row-sub">${escHtml(sub)}</div>` : ''}
  </div>
  <div class="chat-row-right">
    ${lastMsg ? `<div class="chat-row-ts">${lastMsg}</div>` : ''}
    <div style="display:flex;gap:4px">${badge(c.llm)}${permBadge(c.chat_id)}</div>
    <svg class="chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg>
  </div>
</div>`;
}

// ── Permissions panel (inside chat detail) ────────────────────────────────────
const CLAUDE_EXTRA_TOOLS = ['Edit', 'Write', 'Bash'];

function permPanel(c) {
  const p      = S.permissions[c.chat_id] || { level: 'god', custom_allow: [] };
  const lvl    = p.level;
  const custom = p.custom_allow || [];
  const presets = ['standard', 'developer', 'god', 'custom'];

  const presetBtns = presets.map(l => {
    const active = lvl === l;
    return `<button class="btn btn-sm ${active ? 'btn-primary' : 'btn-ghost'} perm-preset" data-level="${l}" data-chatid="${escAttr(c.chat_id)}">${l.charAt(0).toUpperCase()+l.slice(1)}</button>`;
  }).join('');

  const customTools = lvl === 'custom' && c.llm !== 'codex' ? `
<div class="perm-custom-tools" id="perm-custom-tools">
  ${CLAUDE_EXTRA_TOOLS.map(t => `
  <label class="perm-tool-label">
    <input type="checkbox" class="perm-tool-cb" value="${t}" ${custom.includes(t) ? 'checked' : ''}> ${t}
  </label>`).join('')}
  <button class="btn btn-primary btn-sm" id="btn-save-custom" data-chatid="${escAttr(c.chat_id)}">Save</button>
</div>` : '';

  return `
<div class="perm-panel card">
  <div class="card-header">
    <span class="card-title">Permissions</span>
    ${permBadge(c.chat_id)}
  </div>
  <div class="perm-presets">${presetBtns}</div>
  ${customTools}
</div>`;
}

// ── Chat Detail ───────────────────────────────────────────────────────────────
function chatDetail(c) {
  const icon = c.icon || (c.chat_id.endsWith('@g.us') ? '👥' : '👤');

  const bubbles = S.chatLog.length
    ? S.chatLog.map(e => bubble(e)).join('')
    : `<div class="empty" style="padding:24px"><div class="empty-icon">💬</div>No messages yet.</div>`;

  const typing = S.chatSending
    ? `<div class="bubble out"><div class="typing-dots"><span></span><span></span><span></span></div></div>`
    : '';

  return `
<div class="detail-header">
  <button class="btn btn-ghost btn-sm" id="btn-back">← Back</button>
  <div class="detail-title">
    <span class="detail-icon">${icon}</span>
    <div>
      <div class="detail-name">${escHtml(c.name || c.chat_id)}</div>
      <div class="detail-sub">${escHtml(c.chat_id)}</div>
    </div>
    ${badge(c.llm)}
  </div>
  <div class="detail-actions">
    <button class="btn btn-ghost btn-sm" data-stats="${escAttr(c.chat_id)}">Stats</button>
    <button class="btn btn-ghost btn-sm" data-clear="${escAttr(c.chat_id)}">Clear</button>
    <button class="btn btn-danger btn-sm" data-disconnect="${escAttr(c.chat_id)}" data-llm="${escAttr(c.llm)}">Off</button>
  </div>
</div>
${permPanel(c)}
<div class="chat-messages" id="chat-messages">
  ${bubbles}
  ${typing}
</div>
<div class="chat-input-row">
  <textarea id="chat-input" class="chat-input" placeholder="Type a message or !command…" rows="1"></textarea>
  <button id="btn-send-msg" class="btn btn-primary" ${S.chatSending ? 'disabled' : ''}>Send</button>
</div>`;
}

function bubble(e) {
  const dir     = e.direction === 'in' ? 'in' : 'out';
  const text    = e.text || (e.has_attachment ? '📎 [attachment]' : '');
  const clipped = text.length > 400;
  return `
<div class="bubble ${dir}" id="b-${e.id}">
  <div class="bubble-text${clipped ? ' clipped' : ''}">${escHtml(text)}</div>
  ${clipped ? `<button class="btn btn-ghost btn-sm" style="margin-top:4px;font-size:10px" onclick="expandBubble(${e.id})">Show more</button>` : ''}
  <div class="bubble-meta">
    <span>${fmtTime(e.ts)}</span>
    ${e.tokens_used ? `<span>${e.tokens_used} tok</span>` : ''}
  </div>
</div>`;
}

window.expandBubble = id => {
  const el = document.querySelector(`#b-${id} .bubble-text`);
  if (el) el.classList.remove('clipped');
};

// ── Health ────────────────────────────────────────────────────────────────────
const HEALTH_CHANNELS = ['whatsapp', 'signal'];
const HEALTH_LLMS     = ['claude_cli', 'codex_cli'];

function healthItem(name, val) {
  const ok     = val && (val.ok === true);
  const detail = val ? (val.version || val.error || val.account || '') : '';
  const label  = name.replace(/_/g,' ').replace(/\b\w/g, c => c.toUpperCase());
  return `
<div class="health-item">
  <div>
    <div class="health-name">${escHtml(label)}</div>
    ${detail ? `<div class="health-detail">${escHtml(String(detail))}</div>` : ''}
  </div>
  <span class="badge ${ok ? 'badge-ok' : 'badge-error'}">${ok ? '✓ OK' : '✗ Error'}</span>
</div>`;
}

function health() {
  if (!S.health) return spinner();
  const h = S.health;
  const chItems  = HEALTH_CHANNELS.map(k => healthItem(k, h[k])).join('');
  const llmItems = HEALTH_LLMS.map(k => healthItem(k, h[k])).join('');
  return `
<div class="page-title">
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>
  Health
  <button class="btn btn-ghost btn-sm" id="btn-refresh-health" style="margin-left:auto">↻</button>
</div>
<div class="section card">
  <div class="card-header"><span class="card-title">Channels</span></div>
  ${chItems}
</div>
<div class="card">
  <div class="card-header"><span class="card-title">LLMs</span></div>
  ${llmItems}
</div>`;
}

// ── Server ────────────────────────────────────────────────────────────────────
function server() {
  if (!S.server) return spinner();
  const sv = S.server;
  return `
<div class="page-title">
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="5" rx="1"/><rect x="2" y="10" width="20" height="5" rx="1"/><rect x="2" y="17" width="20" height="5" rx="1"/></svg>
  Server
  <button class="btn btn-ghost btn-sm" id="btn-refresh-server" style="margin-left:auto">↻</button>
</div>
<div class="card section">
  <div class="card-header"><span class="card-title">Bridge</span></div>
  <table class="kv-table">
    <tr><td>Version</td><td>${escHtml(sv.version||'—')}</td></tr>
    <tr><td>Data dir</td><td>${escHtml(sv.data_dir||'—')}</td></tr>
    <tr><td>Admin port</td><td>${window.location.port||'8081'}</td></tr>
    ${sv.lan_ip ? `<tr><td>Port-forwarding IP</td><td><strong style="color:var(--primary)">${escHtml(sv.lan_ip)}</strong></td></tr>` : ''}
    <tr><td>All IPs</td><td>${escHtml((sv.ips||[]).join(', ')||'—')}</td></tr>
    ${sv.ngrok_url ? `<tr><td>Public URL</td><td><a href="${escAttr(sv.ngrok_url)}" target="_blank" style="color:var(--primary)">${escHtml(sv.ngrok_url)}</a></td></tr>` : ''}
  </table>
</div>
<div class="card">
  <div class="card-header"><span class="card-title">CLIs</span></div>
  <table class="kv-table">
    <tr><td>Claude CLI</td><td>${escHtml(sv.claude_version||'—')}</td></tr>
    <tr><td>Codex CLI</td><td>${escHtml(sv.codex_version||'—')}</td></tr>
  </table>
</div>`;
}

function spinner() {
  return `<div class="loading-screen"><div class="spinner"></div><p>Loading…</p></div>`;
}

// ── Event binding ─────────────────────────────────────────────────────────────
function bindPageEvents() {
  // Dashboard
  on('btn-refresh',        () => loadStatus().then(renderPage));
  // Chats list
  on('btn-refresh-chats',  () => loadChats().then(renderPage));
  // Health
  on('btn-refresh-health', () => loadHealth().then(renderPage));
  // Server
  on('btn-refresh-server', () => loadServer().then(renderPage));

  // Chat rows → open detail
  document.querySelectorAll('.chat-row').forEach(row => {
    row.onclick = async () => {
      const id = row.dataset.chatid;
      const c  = S.chats.find(x => x.chat_id === id);
      if (!c) return;
      S.selectedChat = c;
      S.chatLog      = [];
      S.chatSending  = false;
      renderPage(); // render immediately with empty log + spinner feel
      try { await Promise.all([loadChatLog(id), loadPermissions()]); } catch(e) { console.error(e); }
      renderPage();
      scrollChatBottom();
    };
  });

  // Chat detail — back
  on('btn-back', () => { S.selectedChat = null; renderPage(); });

  // Chat detail — action buttons
  document.querySelectorAll('[data-stats]').forEach(b => {
    b.onclick = async () => {
      try { const r = await postCmd(b.dataset.stats, '!stats'); showModal('Stats', r.reply||'(no reply)'); }
      catch(e) { toast(e.message, 'error'); }
    };
  });
  document.querySelectorAll('[data-clear]').forEach(b => {
    b.onclick = async () => {
      if (!confirm('Clear session?')) return;
      try { await postCmd(b.dataset.clear, '!clear-session'); toast('Session cleared'); }
      catch(e) { toast(e.message, 'error'); }
    };
  });
  document.querySelectorAll('[data-disconnect]').forEach(b => {
    b.onclick = async () => {
      const id = b.dataset.disconnect, llm = b.dataset.llm || 'claude';
      if (!confirm(`Remove from ${llm}?`)) return;
      try {
        await postCmd(id, llm === 'codex' ? '!remove-codex' : '!remove-claude');
        toast('Chat removed');
        S.selectedChat = null;
        await loadChats();
        renderPage();
      } catch(e) { toast(e.message, 'error'); }
    };
  });

  // Chat detail — send
  on('btn-send-msg', sendChatMessage);
  const inp = document.getElementById('chat-input');
  if (inp) {
    inp.oninput = () => { inp.style.height = 'auto'; inp.style.height = Math.min(inp.scrollHeight, 120) + 'px'; };
    inp.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendChatMessage(); } };
  }

  // Permission preset buttons
  document.querySelectorAll('.perm-preset').forEach(btn => {
    btn.onclick = async () => {
      const chatID = btn.dataset.chatid;
      const level  = btn.dataset.level;
      if (level === 'custom') {
        // just switch to custom UI, save later via btn-save-custom
        if (!S.permissions[chatID]) S.permissions[chatID] = { level: 'god', custom_allow: [] };
        S.permissions[chatID].level = 'custom';
        renderPage();
        return;
      }
      try {
        await api('/api/permissions', { method: 'POST', body: JSON.stringify({ chat_id: chatID, level }) });
        S.permissions[chatID] = { level, custom_allow: [] };
        toast(`Permission set to ${level} — session cleared`);
        renderPage();
      } catch(e) { toast(e.message, 'error'); }
    };
  });

  // Custom tool save
  on('btn-save-custom', async () => {
    const btn    = document.getElementById('btn-save-custom');
    if (!btn) return;
    const chatID = btn.dataset.chatid;
    const checks = document.querySelectorAll('.perm-tool-cb');
    const extra  = Array.from(checks).filter(cb => cb.checked).map(cb => cb.value);
    try {
      await api('/api/permissions', { method: 'POST', body: JSON.stringify({ chat_id: chatID, level: 'custom', custom_allow: extra }) });
      S.permissions[chatID] = { level: 'custom', custom_allow: extra };
      toast('Custom permissions saved — session cleared');
      renderPage();
    } catch(e) { toast(e.message, 'error'); }
  });

  // Accordion toggles
  document.querySelectorAll('.chan-group-header').forEach(h => {
    h.onclick = () => {
      const ch = h.dataset.channel;
      S.accordionOpen[ch] = S.accordionOpen[ch] === false ? true : false;
      renderPage();
    };
  });

  // Modal
  on('modal-backdrop', closeModal);
  on('modal-close',    closeModal);
}

function on(id, fn) {
  const el = document.getElementById(id);
  if (el) el.onclick = fn;
}

// ── Send from chat detail ─────────────────────────────────────────────────────
async function sendChatMessage() {
  if (S.chatSending || !S.selectedChat) return;
  const inp  = document.getElementById('chat-input');
  const text = inp ? inp.value.trim() : '';
  if (!text) return;
  if (inp) { inp.value = ''; inp.style.height = 'auto'; }

  const chatID = S.selectedChat.chat_id;
  const isCmd  = text.startsWith('!');

  // Optimistically add the IN bubble
  const fakeIn = { id: Date.now(), ts: new Date().toISOString(), chat_id: chatID, direction: 'in', text, has_attachment: false, tokens_used: 0 };
  S.chatLog.push(fakeIn);
  S.chatSending = true;
  renderPage();
  scrollChatBottom();

  try {
    let reply;
    if (isCmd) {
      const r = await postCmd(chatID, text);
      reply = r.reply;
    } else {
      const r = await postSend(chatID, text);
      reply = r.reply;
    }
    if (reply) {
      S.chatLog.push({ id: Date.now()+1, ts: new Date().toISOString(), chat_id: chatID, direction: 'out', text: reply, has_attachment: false, tokens_used: 0 });
    }
  } catch(e) {
    S.chatLog.push({ id: Date.now()+1, ts: new Date().toISOString(), chat_id: chatID, direction: 'out', text: `⚠️ Error: ${e.message}`, has_attachment: false, tokens_used: 0 });
  }

  S.chatSending = false;
  renderPage();
  scrollChatBottom();
}

function scrollChatBottom() {
  requestAnimationFrame(() => {
    const el = document.getElementById('chat-messages');
    if (el) el.scrollTop = el.scrollHeight;
  });
}

// ── Init ──────────────────────────────────────────────────────────────────────
async function init() {
  // Nav clicks load data then navigate
  document.querySelectorAll('[data-page]').forEach(a => {
    a.addEventListener('click', e => { e.preventDefault(); navTo(a.dataset.page); });
  });

  const initPage = location.hash.slice(1) || 'dashboard';
  updateNavActive();

  try {
    const loaders = [loadStatus(), loadChats()];
    if (initPage === 'health') loaders.push(loadHealth());
    if (initPage === 'server') loaders.push(loadServer());
    await Promise.all(loaders);
  } catch(e) {
    document.getElementById('content').innerHTML =
      `<div class="loading-screen"><p style="color:var(--red)">Cannot reach bridge API.<br>${escHtml(e.message)}</p></div>`;
    return;
  }

  navigate(initPage);

  window.addEventListener('hashchange', () => {
    navTo(location.hash.slice(1) || 'dashboard');
  });
}

window.addEventListener('DOMContentLoaded', init);
