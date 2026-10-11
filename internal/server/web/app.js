const locales = await fetch('/mcp/assets/locales.json').then(r => r.json());
const language = document.getElementById('language');
const requestedLanguages = (new URLSearchParams(location.search).get('ui_locales') || '').split(/\s+/);
language.value = [...requestedLanguages, navigator.language, 'en-US'].find(value => Object.hasOwn(locales, value));
let words = locales[language.value];
const translate = () => { words = locales[language.value]; document.documentElement.lang = language.value; document.title = `${words.title} · MetaFusion`; document.querySelectorAll('[data-i18n]').forEach(el => { el.textContent = words[el.dataset.i18n] }); };
translate();
language.addEventListener('change', () => { translate(); void refresh().catch(showError); });
const notice = document.getElementById('notice');
function showError(error) { notice.textContent = `${words.error}: ${error.message || error}`; }
async function api(path, options = {}) { const response = await fetch(`/mcp/manage/api/${path}`, {credentials:'same-origin', ...options, headers:{'Content-Type':'application/json', ...options.headers}}); const data = await response.json(); if (!response.ok) { const error = new Error(data.error || String(response.status)); error.status = response.status; throw error; } return data; }
function element(tag, text, className) { const el = document.createElement(tag); if (text !== undefined) el.textContent = text; if (className) el.className = className; return el; }
function scopePicker(container, scopes, selected) { container.replaceChildren(); for (const scope of [...new Set(scopes)]) { const label = element('label', undefined, 'scope'); const input = document.createElement('input'); input.type = 'checkbox'; input.value = scope; input.checked = selected.includes(scope); label.append(input, element('span', scope === 'mcp.read' ? words.read : scope)); container.append(label); } }
function selected(container) { return [...container.querySelectorAll('input:checked')].map(el => el.value); }
function busy(button, fn) { button.disabled = true; notice.textContent = ''; return fn().catch(showError).finally(() => { button.disabled = false; }); }
const request = location.search.slice(1);
const consent = document.getElementById('consent');
async function refresh() {
 const {user, endpoint, scopes} = await api('me');
 document.getElementById('workspace').hidden = false; document.getElementById('login').hidden = true;
 document.getElementById('identity').textContent = `${words.signedIn}: ${user.username}`;
 document.getElementById('endpoint').textContent = endpoint;
 if (location.pathname === '/mcp/oauth/authorize') {
  const authorization = await api(`authorization?${request}`);
  document.getElementById('client').textContent = `${authorization.client.client_name} (${authorization.client.client_id})`;
  document.getElementById('callback').textContent = `${words.callback}: ${authorization.redirect_uri}`;
  const allowed = authorization.scopes.filter(scope => scopes.includes(scope) || user.permissions.includes('*'));
  scopePicker(document.getElementById('scopes'), allowed, ['mcp.read']);
  consent.hidden = false;
 }
 const {items} = await api('grants'); const container = document.getElementById('grants'); container.replaceChildren();
 if (!items.length) container.append(element('p', words.empty));
 for (const grant of items) {
  const card = element('article', undefined, 'grant'); card.append(element('h3', grant.client_name));
  card.append(element('p', `${words.expires}: ${new Date(grant.expires_at).toLocaleString(language.value)}`, 'meta'));
  if (grant.revoked_at) { card.append(element('p', words.revoked)); container.append(card); continue; }
  const picker = element('div'); scopePicker(picker, grant.scopes, grant.scopes); card.append(picker);
  const buttons = element('div', undefined, 'buttons'); const save = element('button', words.save); const revoke = element('button', words.revoke, 'secondary'); buttons.append(save, revoke); card.append(buttons);
  save.onclick = () => busy(save, async () => { const response = await api(`grants/${encodeURIComponent(grant.id)}`, {method:'PATCH', body:JSON.stringify({scopes:selected(picker)})}); await refresh(); notice.textContent = response.upstream_token_revoked ? words.copied : words.cleanup; });
  revoke.onclick = () => { if (!window.confirm(words.confirmRevoke)) return; return busy(revoke, async () => { const response = await api(`grants/${encodeURIComponent(grant.id)}`, {method:'DELETE'}); await refresh(); if (!response.upstream_token_revoked) notice.textContent = words.cleanup; }); };
  container.append(card);
 }
}
for (const [id, allow] of [['approve', true], ['deny', false]]) {
 const button = document.getElementById(id);
 button.onclick = () => busy(button, async () => { const days = Number(document.getElementById('days').value); if (!Number.isInteger(days) || days < 1 || days > 30) throw new Error(words.invalidExpiry); const response = await api('approve', {method:'POST', body:JSON.stringify({request, allow, scopes:selected(document.getElementById('scopes')), expires_in_days:days})}); location.assign(response.redirect); });
}
await refresh().catch(error => { if (error.status === 401) { const link = document.getElementById('login'); link.href = `/login?redirect=${encodeURIComponent(location.pathname + location.search)}`; link.hidden = false; } else showError(error); });
