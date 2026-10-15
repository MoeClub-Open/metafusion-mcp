const locales = await fetch('/mcp/assets/locales.json').then(r => r.json());
const language = document.getElementById('language');
const normalize = (value) => {
  if (!value) return '';
  if (Object.hasOwn(locales, value)) return value;
  const base = value.toLowerCase();
  if (base.startsWith('zh-tw') || base.startsWith('zh-hk') || base.startsWith('zh-hant')) return 'zh-TW';
  if (base.startsWith('zh')) return 'zh-CN';
  if (base.startsWith('ja')) return 'ja-JP';
  if (base.startsWith('en')) return 'en-US';
  return '';
};
// Language priority — do not use browser navigator locales (they overwrote server zh-CN with en-US):
// 1) explicit ?ui_locales=  2) site NEXT_LOCALE cookie  3) <html lang> from server  4) zh-CN
const SITE_LOCALE_COOKIE = 'NEXT_LOCALE';
const readSiteLocale = () => {
  const m = document.cookie.match(/(?:^|;\s*)NEXT_LOCALE=([^;]+)/);
  return m ? normalize(decodeURIComponent(m[1])) : '';
};
const writeSiteLocale = (value) => {
  document.cookie = `${SITE_LOCALE_COOKIE}=${encodeURIComponent(value)}; Path=/; Max-Age=${60 * 60 * 24 * 365}; SameSite=Lax`;
};
const requestedLanguages = (new URLSearchParams(location.search).get('ui_locales') || '').split(/\s+/).map(normalize).filter(Boolean);
const siteLocale = readSiteLocale();
const htmlLocale = normalize(document.documentElement.lang);
language.value = [...requestedLanguages, siteLocale, htmlLocale, 'zh-CN'].find(value => value && Object.hasOwn(locales, value)) || 'zh-CN';
let words = locales[language.value];

const MODE_KEY = 'metafusion_theme_mode';
const ACCENT_KEY = 'metafusion_theme_accent';
const TONE_KEY = 'metafusion_theme_tone';
const ACCENTS = [
  ['blue', '#3b82f6'], ['sky', '#0ea5e9'], ['cyan', '#06b6d4'], ['teal', '#14b8a6'],
  ['emerald', '#10b981'], ['lime', '#65a30d'], ['amber', '#f59e0b'], ['orange', '#f97316'],
  ['red', '#ef4444'], ['rose', '#f43f5e'], ['fuchsia', '#d946ef'], ['purple', '#a855f7'],
  ['violet', '#8b5cf6'], ['indigo', '#6366f1'], ['slate', '#64748b'], ['monochrome', '#71717a'],
];
const TONES = new Set(['neutral', 'cool', 'warm', 'deep']);
const themeModeBtn = document.getElementById('themeMode');
const themeAccent = document.getElementById('themeAccent');

function applyTheme(mode, accent, tone) {
  const effective = mode === 'system'
    ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
    : mode;
  const root = document.documentElement;
  root.setAttribute('data-theme-mode', effective);
  root.setAttribute('data-theme-accent', accent);
  root.setAttribute('data-theme-tone', tone);
  root.classList.toggle('dark', effective === 'dark');
  root.classList.toggle('light', effective === 'light');
  root.style.colorScheme = effective;
  const meta = document.querySelector('meta[name="theme-color"]');
  const bg = getComputedStyle(root).getPropertyValue('--bg-color').trim();
  if (meta && bg) meta.setAttribute('content', bg);
  themeModeBtn.dataset.mode = mode;
  themeModeBtn.title = words.themeMode?.[mode] || mode;
  themeModeBtn.setAttribute('aria-label', themeModeBtn.title);
  themeModeBtn.textContent = mode === 'light' ? '☀' : mode === 'system' ? '◐' : '☾';
}

function readTheme() {
  const modeRaw = localStorage.getItem(MODE_KEY);
  const mode = modeRaw === 'light' || modeRaw === 'dark' || modeRaw === 'system' ? modeRaw : 'dark';
  const accentRaw = localStorage.getItem(ACCENT_KEY);
  const accent = ACCENTS.some(([id]) => id === accentRaw) ? accentRaw : 'blue';
  const toneRaw = localStorage.getItem(TONE_KEY);
  const tone = TONES.has(toneRaw) ? toneRaw : 'neutral';
  return { mode, accent, tone };
}

function initTheme() {
  themeAccent.replaceChildren();
  for (const [id, swatch] of ACCENTS) {
    const option = document.createElement('option');
    option.value = id;
    option.textContent = words.accents?.[id] || id;
    option.style.color = swatch;
    themeAccent.append(option);
  }
  const current = readTheme();
  themeAccent.value = current.accent;
  applyTheme(current.mode, current.accent, current.tone);
  themeModeBtn.onclick = () => {
    const cycle = { dark: 'light', light: 'system', system: 'dark' };
    const next = cycle[themeModeBtn.dataset.mode || 'dark'] || 'dark';
    localStorage.setItem(MODE_KEY, next);
    const t = readTheme();
    applyTheme(next, t.accent, t.tone);
  };
  themeAccent.onchange = () => {
    localStorage.setItem(ACCENT_KEY, themeAccent.value);
    const t = readTheme();
    applyTheme(t.mode, themeAccent.value, t.tone);
  };
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    const t = readTheme();
    if (t.mode === 'system') applyTheme(t.mode, t.accent, t.tone);
  });
}

const SCOPE_GROUPS = [
  { id: 'catalogRead', scopes: ['mcp.read'] },
  { id: 'web', scopes: ['mcp.web.write'] },
  { id: 'catalogEdit', scopes: ['catalog.entity.edit', 'catalog.relation.edit'] },
  { id: 'community', scopes: ['community.post.create', 'community.post.moderate', 'community.topic.pin', 'community.report.review'] },
  { id: 'storage', scopes: ['storage.asset.upload', 'storage.asset.moderate'] },
  { id: 'audit', scopes: ['auth.audit.read'] },
];

const translate = () => {
  words = locales[language.value];
  document.documentElement.lang = language.value;
  document.title = `${words.manageTitle} · MetaFusion`;
  document.querySelectorAll('[data-i18n]').forEach(el => { el.textContent = words[el.dataset.i18n] || ''; });
  for (const option of themeAccent.options) {
    option.textContent = words.accents?.[option.value] || option.value;
  }
  const t = readTheme();
  applyTheme(t.mode, t.accent, t.tone);
};
initTheme();
translate();

const notice = document.getElementById('notice');
const sessionBanner = document.getElementById('sessionBanner');
const sessionMessage = document.getElementById('sessionMessage');
const errorDetails = document.getElementById('errorDetails');
const errorCode = document.getElementById('errorCode');
const login = document.getElementById('login');
const manageShell = document.getElementById('manageShell');
const workspace = document.getElementById('workspace');
const consent = document.getElementById('consent');
const selectAllScopes = document.getElementById('selectAllScopes');

manageShell.hidden = false;

function showSessionRequired(code) {
  workspace.hidden = true;
  sessionBanner.hidden = false;
  sessionMessage.textContent = words.sessionRequired;
  login.href = `/login?redirect=${encodeURIComponent(location.pathname + location.search)}`;
  login.hidden = false;
  if (code) {
    errorDetails.hidden = false;
    errorCode.textContent = code;
  } else {
    errorDetails.hidden = true;
    errorCode.textContent = '';
  }
  notice.textContent = '';
}

function showError(error) {
  const code = error && (error.code || error.message);
  if (error && error.status === 401 || code === 'session_required') {
    showSessionRequired(code || 'session_required');
    return;
  }
  const human = (words.errors && words.errors[code]) || words.error;
  notice.className = '';
  notice.textContent = human;
  if (code && code !== human) {
    errorDetails.hidden = false;
    errorCode.textContent = code;
    sessionBanner.hidden = false;
    sessionMessage.textContent = human;
    login.hidden = true;
  }
}

async function api(path, options = {}) {
  const response = await fetch(`/mcp/manage/api/${path}`, {
    credentials: 'same-origin',
    ...options,
    headers: { 'Content-Type': 'application/json', ...options.headers },
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    const err = new Error(data.error || String(response.status));
    err.status = response.status;
    err.code = data.error || String(response.status);
    throw err;
  }
  return data;
}

function element(tag, text, className) {
  const el = document.createElement(tag);
  if (text !== undefined) el.textContent = text;
  if (className) el.className = className;
  return el;
}

function scopeMeta(scope) {
  const entry = words.scopes && words.scopes[scope];
  return entry || { name: scope, purpose: '' };
}

function selected(container) {
  return [...container.querySelectorAll('input[type=checkbox]:checked')].map(el => el.value);
}

function setGroupChecks(groupEl, checked) {
  groupEl.querySelectorAll('input[type=checkbox]').forEach(input => { input.checked = checked; });
}

function syncSelectAll() {
  const boxes = [...document.querySelectorAll('#scopes input[type=checkbox]')];
  if (!boxes.length) {
    selectAllScopes.checked = false;
    selectAllScopes.indeterminate = false;
    return;
  }
  const checked = boxes.filter(input => input.checked).length;
  selectAllScopes.checked = checked === boxes.length;
  selectAllScopes.indeterminate = checked > 0 && checked < boxes.length;
}

function scopePicker(container, available, checked) {
  container.replaceChildren();
  const availableSet = new Set(available);
  const checkedSet = new Set(checked);
  for (const group of SCOPE_GROUPS) {
    const scopes = group.scopes.filter(scope => availableSet.has(scope));
    if (!scopes.length) continue;
    const details = element('details', undefined, 'scope-group');
    details.open = false;
    const summary = element('summary');
    summary.append(element('span', words.groups[group.id].title));
    summary.append(element('span', words.groups[group.id].description, 'scope-group-desc'));
    const actions = element('div', undefined, 'group-actions');
    const selectAll = element('button', words.selectGroup);
    selectAll.type = 'button';
    const clearAll = element('button', words.clearGroup);
    clearAll.type = 'button';
    selectAll.onclick = (event) => {
      event.preventDefault();
      setGroupChecks(details, true);
      syncSelectAll();
    };
    clearAll.onclick = (event) => {
      event.preventDefault();
      setGroupChecks(details, false);
      syncSelectAll();
    };
    actions.append(selectAll, clearAll);
    summary.append(actions);
    details.append(summary);
    const list = element('div', undefined, 'scope-list');
    for (const scope of scopes) {
      const meta = scopeMeta(scope);
      const label = element('label', undefined, 'scope');
      const input = document.createElement('input');
      input.type = 'checkbox';
      input.value = scope;
      input.checked = checkedSet.has(scope);
      input.addEventListener('change', syncSelectAll);
      const name = element('span', undefined, 'scope-name');
      name.append(document.createTextNode(meta.name));
      name.append(element('span', scope, 'scope-code'));
      label.append(input, name, element('span', meta.purpose, 'scope-purpose'));
      list.append(label);
    }
    details.append(list);
    container.append(details);
  }
  if (container.id === 'scopes') syncSelectAll();
}

selectAllScopes.addEventListener('change', () => {
  document.querySelectorAll('#scopes input[type=checkbox]').forEach(input => {
    input.checked = selectAllScopes.checked;
  });
  syncSelectAll();
});

function busy(button, fn) {
  button.disabled = true;
  notice.textContent = '';
  notice.className = '';
  return fn().catch(showError).finally(() => { button.disabled = false; });
}

const request = location.search.slice(1);

async function refresh() {
  sessionBanner.hidden = true;
  login.hidden = true;
  errorDetails.hidden = true;
  const { user, endpoint, scopes } = await api('me');
  workspace.hidden = false;
  document.getElementById('identity').textContent = `${words.signedIn}: ${user.username}`;
  document.getElementById('endpoint').textContent = endpoint;

  const authorization = await api(`authorization?${request}`);
  document.getElementById('client').textContent = `${authorization.client.client_name} (${authorization.client.client_id})`;
  document.getElementById('callback').textContent = `${words.callback}: ${authorization.redirect_uri}`;
  const allowed = authorization.scopes.filter(scope => scopes.includes(scope) || user.permissions.includes('*'));
  // Writes, including third-party HTTP writes, require an explicit checkbox selection.
  scopePicker(document.getElementById('scopes'), allowed, allowed.filter(scope => scope === 'mcp.read'));
  consent.hidden = false;
}


for (const [id, allow] of [['approve', true], ['deny', false]]) {
  const button = document.getElementById(id);
  button.onclick = () => busy(button, async () => {
    const response = await api('approve', {
      method: 'POST',
      body: JSON.stringify({ request, allow, scopes: selected(document.getElementById('scopes')) }),
    });
    location.assign(response.redirect);
  });
}

language.addEventListener('change', () => {
  writeSiteLocale(language.value);
  translate();
  void refresh().catch(showError);
});

await refresh().catch(showError);
