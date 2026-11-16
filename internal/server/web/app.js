const locales = await fetch('/mcp/assets/locales.json').then(r => r.json());

const LOCALE_OPTIONS = [
  ['zh-CN', '简体中文'],
  ['en-US', 'English'],
  ['zh-TW', '繁體中文'],
  ['ja-JP', '日本語'],
];

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
let currentLocale = [...requestedLanguages, siteLocale, htmlLocale, 'zh-CN'].find(value => value && Object.hasOwn(locales, value)) || 'zh-CN';
let words = locales[currentLocale];

const MODE_KEY = 'metafusion_theme_mode';
const ACCENT_KEY = 'metafusion_theme_accent';
const TONE_KEY = 'metafusion_theme_tone';
const PATTERN_KEY = 'metafusion_theme_pattern';
const ACCENTS = [
  'everforest', 'blue', 'sky', 'cyan', 'teal', 'emerald', 'lime', 'amber',
  'orange', 'red', 'rose', 'fuchsia', 'purple', 'violet', 'indigo', 'slate', 'monochrome',
];
const TONES = ['neutral', 'cool', 'warm', 'deep'];
const PATTERNS = ['catalog', 'dots', 'none'];
const MODES = ['system', 'light', 'dark'];

const SVG_NS = 'http://www.w3.org/2000/svg';
function svgIcon(size, children, className = '') {
  const svg = document.createElementNS(SVG_NS, 'svg');
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('fill', 'none');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('stroke-width', '1.9');
  svg.setAttribute('stroke-linecap', 'round');
  svg.setAttribute('stroke-linejoin', 'round');
  svg.setAttribute('aria-hidden', 'true');
  if (className) svg.setAttribute('class', className);
  for (const [tag, attrs] of children) {
    const node = document.createElementNS(SVG_NS, tag);
    for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
    svg.append(node);
  }
  return svg;
}

const ICONS = {
  light: (s = 16) => svgIcon(s, [
    ['circle', { cx: '12', cy: '12', r: '4' }],
    ['path', { d: 'M12 2v2M12 20v2m-7.07-15.07 1.41 1.41m11.32 11.32 1.41 1.41M2 12h2m16 0h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41' }],
  ]),
  dark: (s = 16) => svgIcon(s, [
    ['path', { d: 'M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z' }],
  ]),
  system: (s = 16) => svgIcon(s, [
    ['path', { d: 'M20 16V7a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v9m16 0H4m16 0 1.28 2.55a1 1 0 0 1-.9 1.45H3.62a1 1 0 0 1-.9-1.45L4 16' }],
  ]),
  check: (s = 14) => svgIcon(s, [['path', { d: 'M20 6 9 17l-5-5', 'stroke-width': '2.4' }]]),
  chevron: (s = 14) => svgIcon(s, [['path', { d: 'm6 9 6 6 6-6' }]], 'group-chevron'),
  copy: (s = 13) => svgIcon(s, [
    ['rect', { width: '14', height: '14', x: '8', y: '8', rx: '2', ry: '2' }],
    ['path', { d: 'M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2' }],
  ]),
  catalog: (s = 13) => svgIcon(s, [
    ['path', { d: 'M21 12h-8M21 6H8M21 18h-8M3 6v4c0 1.1.9 2 2 2h3M3 10v6c0 1.1.9 2 2 2h3' }],
  ]),
  dots: (s = 13) => svgIcon(s, [
    ['rect', { width: '18', height: '18', x: '3', y: '3', rx: '2' }],
    ['path', { d: 'M3 12h18M12 3v18' }],
  ]),
  none: (s = 13) => svgIcon(s, [
    ['rect', { width: '18', height: '18', x: '3', y: '3', rx: '2' }],
  ]),
};

function formatTpl(template, params) {
  if (!template) return '';
  return template.replace(/\{(\w+)\}/g, (_, key) => String(params[key] ?? ''));
}

const themeModeBtn = document.getElementById('themeMode');
const paletteBtn = document.getElementById('paletteBtn');
const localeBtn = document.getElementById('localeBtn');
const localePopover = document.getElementById('localePopover');
const modePopover = document.getElementById('modePopover');
const palettePopover = document.getElementById('palettePopover');
const localeList = document.getElementById('localeList');
const modeGrid = document.getElementById('modeGrid');
const accentGrid = document.getElementById('accentGrid');
const toneGrid = document.getElementById('toneGrid');
const patternGrid = document.getElementById('patternGrid');

const POPOVERS = [
  [localeBtn, localePopover, document.getElementById('localeWrap')],
  [themeModeBtn, modePopover, document.getElementById('modeWrap')],
  [paletteBtn, palettePopover, document.getElementById('paletteWrap')],
];

function closePopovers(except = null) {
  for (const [btn, panel] of POPOVERS) {
    if (panel === except) continue;
    panel.hidden = true;
    btn.setAttribute('aria-expanded', 'false');
  }
}

for (const [btn, panel] of POPOVERS) {
  btn.addEventListener('click', () => {
    const opening = panel.hidden;
    closePopovers(opening ? panel : null);
    panel.hidden = !opening;
    btn.setAttribute('aria-expanded', String(opening));
  });
}
document.querySelectorAll('[data-close-popover]').forEach(btn => {
  btn.addEventListener('click', () => closePopovers());
});
document.addEventListener('mousedown', (event) => {
  if (!POPOVERS.some(([, , wrap]) => wrap.contains(event.target))) closePopovers();
});
document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape') closePopovers();
});

function readTheme() {
  let mode = 'dark';
  let accent = 'everforest';
  let tone = 'neutral';
  let pattern = 'catalog';
  try {
    const m = localStorage.getItem(MODE_KEY);
    if (MODES.includes(m)) mode = m;
    const a = localStorage.getItem(ACCENT_KEY);
    if (ACCENTS.includes(a)) accent = a;
    const t = localStorage.getItem(TONE_KEY);
    if (TONES.includes(t)) tone = t;
    const p = localStorage.getItem(PATTERN_KEY);
    if (PATTERNS.includes(p)) pattern = p;
  } catch (_) { /* localStorage disabled */ }
  return { mode, accent, tone, pattern };
}

function saveTheme(key, value) {
  try { localStorage.setItem(key, value); } catch (_) { /* ignore */ }
}

function renderHeaderControls() {
  const current = readTheme();
  const meta = words.themeMeta || {};
  const modeLabel = words.themeMode?.[current.mode] || current.mode;
  const modeTitle = formatTpl(meta.modeControl || '{mode}', { mode: modeLabel });

  localeBtn.title = meta.localeSwitch || 'Language';
  localeBtn.setAttribute('aria-label', localeBtn.title);
  paletteBtn.title = meta.palette || 'Palette';
  paletteBtn.setAttribute('aria-label', paletteBtn.title);
  themeModeBtn.title = modeTitle;
  themeModeBtn.setAttribute('aria-label', meta.displayMode || modeTitle);
  themeModeBtn.replaceChildren((ICONS[current.mode] || ICONS.dark)(16));

  const modePopoverTitle = document.getElementById('modePopoverTitle');
  modePopoverTitle.replaceChildren((ICONS[current.mode] || ICONS.dark)(15));
  const modeText = document.createElement('span');
  modeText.textContent = meta.displayMode || '';
  modePopoverTitle.append(modeText);

  document.querySelectorAll('[data-close-popover]').forEach(el => {
    el.setAttribute('aria-label', meta.close || 'Close');
  });

  localeList.replaceChildren();
  for (const [id, nativeName] of LOCALE_OPTIONS) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = `locale-option${id === currentLocale ? ' is-active' : ''}`;
    const span = document.createElement('span');
    span.textContent = nativeName;
    btn.append(span);
    if (id === currentLocale) btn.append(ICONS.check(14));
    btn.addEventListener('click', () => {
      currentLocale = id;
      writeSiteLocale(id);
      closePopovers();
      translate();
    });
    localeList.append(btn);
  }

  modeGrid.setAttribute('aria-label', meta.displayMode || 'Display mode');
  modeGrid.replaceChildren();
  for (const id of MODES) {
    const btn = document.createElement('button');
    btn.type = 'button';
    const active = current.mode === id;
    btn.className = `mode-option${active ? ' is-active' : ''}`;
    btn.setAttribute('aria-pressed', String(active));
    const label = document.createElement('span');
    label.textContent = words.themeMode?.[id] || id;
    btn.append(ICONS[id](15), label);
    btn.addEventListener('click', () => {
      saveTheme(MODE_KEY, id);
      applyCurrentTheme();
      closePopovers();
    });
    modeGrid.append(btn);
  }

  document.getElementById('accentCurrent').textContent = words.accents?.[current.accent] || current.accent;
  accentGrid.replaceChildren();
  for (const id of ACCENTS) {
    const active = current.accent === id;
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = `swatch-btn${active ? ' is-active' : ''}`;
    btn.dataset.swatch = id;
    const labelText = words.accents?.[id] || id;
    btn.title = labelText;
    btn.setAttribute('aria-label', labelText);
    btn.setAttribute('aria-pressed', String(active));
    const dot = document.createElement('span');
    dot.className = 'swatch-dot';
    btn.append(dot);
    if (active) {
      const checkWrap = document.createElement('span');
      checkWrap.className = 'swatch-check';
      checkWrap.append(ICONS.check(9));
      btn.append(checkWrap);
    }
    btn.addEventListener('click', () => {
      saveTheme(ACCENT_KEY, id);
      applyCurrentTheme();
    });
    accentGrid.append(btn);
  }

  document.getElementById('toneCurrent').textContent = words.tones?.[current.tone] || current.tone;
  toneGrid.replaceChildren();
  for (const id of TONES) {
    const active = current.tone === id;
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = `chip-option${active ? ' is-active' : ''}`;
    btn.setAttribute('aria-pressed', String(active));
    btn.textContent = words.tones?.[id] || id;
    btn.addEventListener('click', () => {
      saveTheme(TONE_KEY, id);
      applyCurrentTheme();
    });
    toneGrid.append(btn);
  }

  document.getElementById('patternCurrent').textContent = words.patterns?.[current.pattern] || current.pattern;
  patternGrid.replaceChildren();
  for (const id of PATTERNS) {
    const active = current.pattern === id;
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = `chip-option${active ? ' is-active' : ''}`;
    btn.setAttribute('aria-pressed', String(active));
    const span = document.createElement('span');
    span.textContent = words.patterns?.[id] || id;
    btn.append(ICONS[id](13), span);
    btn.addEventListener('click', () => {
      saveTheme(PATTERN_KEY, id);
      applyCurrentTheme();
    });
    patternGrid.append(btn);
  }
}

function applyTheme(mode, accent, tone, pattern) {
  const effective = mode === 'system'
    ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
    : mode;
  const root = document.documentElement;
  root.setAttribute('data-theme-mode', effective);
  root.setAttribute('data-theme-accent', accent);
  root.setAttribute('data-theme-tone', tone);
  root.setAttribute('data-theme-pattern', pattern);
  root.classList.toggle('dark', effective === 'dark');
  root.classList.toggle('light', effective === 'light');
  const meta = document.querySelector('meta[name="theme-color"]');
  const bg = getComputedStyle(root).getPropertyValue('--bg-color').trim();
  if (meta && bg) meta.setAttribute('content', bg);
  renderHeaderControls();
}

function applyCurrentTheme() {
  const t = readTheme();
  applyTheme(t.mode, t.accent, t.tone, t.pattern);
}

window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
  const t = readTheme();
  if (t.mode === 'system') applyTheme(t.mode, t.accent, t.tone, t.pattern);
});

const SCOPE_GROUPS = [
  { id: 'catalogRead', scopes: ['mcp.read'] },
  { id: 'web', scopes: ['mcp.web.write'] },
  { id: 'catalogEdit', scopes: ['catalog.entity.edit', 'catalog.relation.edit'] },
  { id: 'community', scopes: ['community.post.create', 'community.post.moderate', 'community.topic.pin', 'community.report.review'] },
  { id: 'storage', scopes: ['storage.asset.upload', 'storage.asset.moderate'] },
  { id: 'audit', scopes: ['auth.audit.read'] },
];

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
const selectedSummary = document.getElementById('selectedSummary');
const toggleExpand = document.getElementById('toggleExpand');
const grantHint = document.getElementById('grantHint');
const approveBtn = document.getElementById('approve');
const denyBtn = document.getElementById('deny');
const copyEndpointBtn = document.getElementById('copyEndpoint');
const scopesContainer = document.getElementById('scopes');
const headerUser = document.getElementById('headerUser');
const headerAvatar = document.getElementById('headerAvatar');
const headerUsername = document.getElementById('headerUsername');
const clientRow = document.getElementById('clientRow');
const callbackRow = document.getElementById('callbackRow');

let currentAllowedScopes = [];
let isSubmitting = false;
let lastErrorCode = '';

manageShell.hidden = false;
copyEndpointBtn.replaceChildren(ICONS.copy(13));

function syncContextRowBorders() {
  const rows = [...document.querySelectorAll('.context-grid > .context-row')];
  let seenFirst = false;
  for (const row of rows) {
    row.classList.remove('is-first-visible');
    if (!row.hidden && !seenFirst) {
      row.classList.add('is-first-visible');
      seenFirst = true;
    }
  }
}

function setNotice(text = '', isOk = false) {
  notice.textContent = text;
  notice.className = `notice-bar${isOk ? ' ok' : ''}`;
  notice.hidden = !text;
}

function updateExpandLabel() {
  const groups = [...scopesContainer.querySelectorAll('details.scope-group')];
  if (!groups.length) {
    toggleExpand.hidden = true;
    return;
  }
  toggleExpand.hidden = false;
  const allOpen = groups.every(d => d.open);
  toggleExpand.textContent = allOpen ? words.collapseAll : words.expandAll;
}

const translate = () => {
  words = locales[currentLocale];
  document.documentElement.lang = currentLocale;
  document.title = `${words.manageTitle} · MetaFusion`;
  document.querySelectorAll('[data-i18n]').forEach(el => {
    el.textContent = words[el.dataset.i18n] || '';
  });
  document.querySelectorAll('[data-i18n-meta]').forEach(el => {
    el.textContent = words.themeMeta?.[el.dataset.i18nMeta] || '';
  });
  copyEndpointBtn.title = words.copyEndpoint || '';
  copyEndpointBtn.setAttribute('aria-label', copyEndpointBtn.title);
  applyCurrentTheme();
  if (currentAllowedScopes.length) {
    const openGroups = new Set(
      [...scopesContainer.querySelectorAll('details.scope-group[open]')].map(d => d.dataset.groupId)
    );
    scopePicker(scopesContainer, currentAllowedScopes, selected(scopesContainer), openGroups);
  }
  if (!sessionBanner.hidden && !login.hidden) {
    sessionMessage.textContent = words.sessionRequired;
  } else if (!sessionBanner.hidden && lastErrorCode) {
    sessionMessage.textContent = (words.errors && words.errors[lastErrorCode]) || words.error;
  }
};

translate();

function showSessionRequired(code) {
  lastErrorCode = code || 'session_required';
  workspace.hidden = true;
  headerUser.hidden = true;
  sessionBanner.hidden = false;
  sessionMessage.textContent = words.sessionRequired;
  login.href = `/login?redirect=${encodeURIComponent(location.pathname + location.search)}`;
  login.hidden = false;
  if (code && code !== 'session_required') {
    errorDetails.hidden = false;
    errorCode.textContent = code;
  } else {
    errorDetails.hidden = true;
    errorCode.textContent = '';
  }
  setNotice('');
}

function showError(error) {
  const code = error && (error.code || error.message);
  if (error && error.status === 401 || code === 'session_required') {
    showSessionRequired(code || 'session_required');
    return;
  }
  lastErrorCode = code || '';
  const human = (words.errors && words.errors[code]) || words.error;
  setNotice(human, false);
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
  return [...container.querySelectorAll('.scope-list input[type=checkbox]:checked')].map(el => el.value);
}

function setGroupChecks(groupEl, checked) {
  groupEl.querySelectorAll('.scope-list input[type=checkbox]').forEach(input => { input.checked = checked; });
}

function syncSelectAll() {
  const boxes = [...document.querySelectorAll('#scopes .scope-list input[type=checkbox]')];
  if (!boxes.length) {
    selectAllScopes.checked = false;
    selectAllScopes.indeterminate = false;
    selectedSummary.textContent = '';
    grantHint.textContent = '';
    approveBtn.disabled = true;
    return;
  }
  const checked = boxes.filter(input => input.checked).length;
  selectAllScopes.checked = checked === boxes.length;
  selectAllScopes.indeterminate = checked > 0 && checked < boxes.length;
  selectedSummary.textContent = formatTpl(words.selectedCount, { checked, total: boxes.length });
  if (checked === 0) {
    grantHint.textContent = words.selectAtLeastOne || '';
    grantHint.classList.add('is-empty');
  } else {
    grantHint.textContent = formatTpl(words.grantSummary, { count: checked });
    grantHint.classList.remove('is-empty');
  }
  if (!isSubmitting) approveBtn.disabled = checked === 0;

  for (const details of scopesContainer.querySelectorAll('details.scope-group')) {
    const groupBoxes = [...details.querySelectorAll('.scope-list input[type=checkbox]')];
    const groupChecked = groupBoxes.filter(i => i.checked).length;
    const groupToggle = details.querySelector('input.group-check');
    const countEl = details.querySelector('.group-count');
    if (groupToggle) {
      groupToggle.checked = groupBoxes.length > 0 && groupChecked === groupBoxes.length;
      groupToggle.indeterminate = groupChecked > 0 && groupChecked < groupBoxes.length;
    }
    if (countEl) {
      countEl.textContent = `${groupChecked}/${groupBoxes.length}`;
      countEl.classList.toggle('is-active', groupChecked > 0);
    }
    details.classList.toggle('has-checked', groupChecked > 0);
  }
}

function scopePicker(container, available, checked, openGroups = new Set()) {
  container.replaceChildren();
  const availableSet = new Set(available);
  const checkedSet = new Set(checked);
  for (const group of SCOPE_GROUPS) {
    const scopes = group.scopes.filter(scope => availableSet.has(scope));
    if (!scopes.length) continue;
    const details = element('details', undefined, 'scope-group');
    details.dataset.groupId = group.id;
    details.open = openGroups.has(group.id);
    details.addEventListener('toggle', updateExpandLabel);

    const summary = element('summary');
    const checkHit = element('span', undefined, 'group-check-hit');
    const groupCheck = document.createElement('input');
    groupCheck.type = 'checkbox';
    groupCheck.className = 'group-check';
    groupCheck.setAttribute('aria-label', words.groups[group.id].title);
    groupCheck.addEventListener('click', (event) => event.stopPropagation());
    groupCheck.addEventListener('change', () => {
      setGroupChecks(details, groupCheck.checked);
      syncSelectAll();
    });
    checkHit.append(groupCheck);

    const main = element('span', undefined, 'group-main');
    main.append(
      element('span', words.groups[group.id].title, 'group-title'),
      element('span', words.groups[group.id].description, 'scope-group-desc'),
    );

    const right = element('span', undefined, 'group-right');
    right.append(element('span', '', 'group-count'), ICONS.chevron(14));

    summary.append(checkHit, main, right);
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
      const info = element('span', undefined, 'scope-info');
      const head = element('span', undefined, 'scope-head');
      head.append(
        element('span', meta.name, 'scope-name'),
        element('code', scope, 'scope-code'),
      );
      info.append(head, element('span', meta.purpose, 'scope-purpose'));
      label.append(input, info);
      list.append(label);
    }
    details.append(list);
    container.append(details);
  }
  if (container.id === 'scopes') {
    syncSelectAll();
    updateExpandLabel();
  }
}

selectAllScopes.addEventListener('change', () => {
  document.querySelectorAll('#scopes .scope-list input[type=checkbox]').forEach(input => {
    input.checked = selectAllScopes.checked;
  });
  syncSelectAll();
});

toggleExpand.addEventListener('click', () => {
  const groups = [...scopesContainer.querySelectorAll('details.scope-group')];
  const shouldOpen = !groups.every(d => d.open);
  groups.forEach(d => { d.open = shouldOpen; });
  updateExpandLabel();
});

let copyTimer = 0;
copyEndpointBtn.addEventListener('click', async () => {
  const value = document.getElementById('endpoint').textContent || '';
  if (!value || !navigator.clipboard?.writeText) return;
  try {
    await navigator.clipboard.writeText(value);
    copyEndpointBtn.classList.add('is-copied');
    copyEndpointBtn.replaceChildren(ICONS.check(13));
    copyEndpointBtn.title = words.copiedEndpoint || '';
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copyEndpointBtn.classList.remove('is-copied');
      copyEndpointBtn.replaceChildren(ICONS.copy(13));
      copyEndpointBtn.title = words.copyEndpoint || '';
    }, 1600);
  } catch (_) { /* ignore clipboard failure */ }
});

function renderCallbackURI(raw) {
  const target = document.getElementById('callback');
  target.replaceChildren();
  try {
    const parsed = new URL(raw);
    target.append(
      element('span', parsed.origin, 'callback-origin'),
      element('span', parsed.pathname + parsed.search, 'callback-path'),
    );
  } catch (_) {
    target.textContent = raw;
  }
}

function busy(button, fn) {
  isSubmitting = true;
  button.disabled = true;
  approveBtn.disabled = true;
  denyBtn.disabled = true;
  setNotice('');
  return fn().catch(showError).finally(() => {
    isSubmitting = false;
    denyBtn.disabled = false;
    syncSelectAll();
  });
}

const request = location.search.slice(1);

async function refresh() {
  lastErrorCode = '';
  sessionBanner.hidden = true;
  login.hidden = true;
  errorDetails.hidden = true;
  setNotice('');
  const { user, endpoint, scopes } = await api('me');
  workspace.hidden = false;
  document.getElementById('identity').textContent = `@${user.username}`;
  document.getElementById('endpoint').textContent = endpoint;
  headerAvatar.textContent = (user.username || 'U').slice(0, 1).toUpperCase();
  headerUsername.textContent = user.username;
  headerUser.hidden = false;
  syncContextRowBorders();

  const authorization = await api(`authorization?${request}`);
  document.getElementById('clientName').textContent = authorization.client.client_name;
  document.getElementById('clientId').textContent = authorization.client.client_id;
  renderCallbackURI(authorization.redirect_uri);
  clientRow.hidden = false;
  callbackRow.hidden = false;
  syncContextRowBorders();

  const allowed = authorization.scopes.filter(scope => scopes.includes(scope) || user.permissions.includes('*'));
  currentAllowedScopes = allowed;
  // Writes, including third-party HTTP writes, require an explicit checkbox selection.
  scopePicker(scopesContainer, allowed, allowed.filter(scope => scope === 'mcp.read'));
  consent.hidden = false;
}

for (const [id, allow] of [['approve', true], ['deny', false]]) {
  const button = document.getElementById(id);
  button.onclick = () => busy(button, async () => {
    const response = await api('approve', {
      method: 'POST',
      body: JSON.stringify({ request, allow, scopes: selected(scopesContainer) }),
    });
    location.assign(response.redirect);
  });
}

await refresh().catch(showError);
