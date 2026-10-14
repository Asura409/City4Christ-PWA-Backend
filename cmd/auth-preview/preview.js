'use strict';
let accessToken = '';
const form = document.querySelector('#register');
const email = () => document.querySelector('#email').value.trim();
async function request(path, body) {
  document.querySelector('#status').textContent = 'Working…';
  try {
    const headers = body === undefined ? {} : {'Content-Type': 'application/json'};
    if ((path === '/api/v1/me' || path === '/api/v1/auth/google/link') && accessToken) headers.Authorization = 'Bearer ' + accessToken;
    const response = await fetch(path, {method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', headers, body: body === undefined ? undefined : JSON.stringify(body)});
    const text = await response.text();
    if (response.ok) { try { const data = JSON.parse(text).data; if (data?.accessToken) accessToken = data.accessToken; } catch {} }
    if (path.endsWith('/logout') || (path.endsWith('/refresh') && !response.ok)) accessToken = '';
    document.querySelector('#status').textContent = `HTTP ${response.status}`;
    document.querySelector('#result').textContent = text;
    return {response, text};
  } catch (error) {
    document.querySelector('#status').textContent = 'Request failed';
    document.querySelector('#result').textContent = error.message;
  }
}
form.addEventListener('submit', event => {event.preventDefault(); request('/api/v1/auth/register', Object.fromEntries(new FormData(form)));});
document.querySelector('#inbox').addEventListener('click', async () => {
  const result = await request('/auth-test/inbox?email=' + encodeURIComponent(email()));
  if (result?.response.ok) { const message = JSON.parse(result.text); document.querySelector('#code').value = message.code; }
});
document.querySelector('#verify').addEventListener('click', () => request('/api/v1/auth/verification/verify', {email: email(), code: document.querySelector('#code').value}));
document.querySelector('#resend').addEventListener('click', () => request('/api/v1/auth/verification/resend', {email: email()}));
document.querySelector('#login').addEventListener('click', () => request('/api/v1/auth/login', {email: email(), password: form.elements.password.value}));
for (const action of ['refresh', 'logout']) document.querySelector('#' + action).addEventListener('click', () => request('/api/v1/auth/' + action, {}));

document.querySelector('#me').addEventListener('click', () => request('/api/v1/me'));
async function beginGoogle(path, body) {
  const result = await request(path, body);
  if (result?.response.ok) window.location.assign(JSON.parse(result.text).data.url);
}
document.querySelector('#google').addEventListener('click', () => beginGoogle('/api/v1/auth/google/start'));
document.querySelector('#google-link').addEventListener('click', () => beginGoogle('/api/v1/auth/google/link', {}));
