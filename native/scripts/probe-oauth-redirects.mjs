// Public authorization-page probe only: no login, saved profile or token exchange.
import assert from 'node:assert/strict';
import { access, readFile } from 'node:fs/promises';
import { randomBytes, createHash } from 'node:crypto';
import { chromium } from 'playwright-core';

const source = await readFile(new URL('../internal/huawei/auth.go', import.meta.url), 'utf8');
const constant = name => {
  const value = source.match(new RegExp(`\\b${name}\\s*=\\s*"([^"]+)"`))?.[1];
  assert.ok(value, `Missing protocol constant: ${name}`);
  return value;
};
const candidates = process.env.HWMUSIC_AUTH_BROWSER
  ? [process.env.HWMUSIC_AUTH_BROWSER]
  : ['/Applications/Helium.app/Contents/MacOS/Helium', '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'];
let executablePath;
for (const candidate of candidates) {
  try { await access(candidate); executablePath = candidate; break; } catch {}
}
assert.ok(executablePath, 'Set HWMUSIC_AUTH_BROWSER to a Chromium executable');
const browser = await chromium.launch({ executablePath, headless: true });
const results = [];
try {
  for (const redirect of [constant('redirectURI'), 'tinglan://oauth/callback', 'tinglan:/oauth2redirect', 'http://127.0.0.1:8765/callback', 'http://127.0.0.1:49152/callback']) {
    // Each candidate gets a fresh, nonpersistent context, state and PKCE verifier.
    const context = await browser.newContext({ serviceWorkers: 'block' });
    try {
      const page = await context.newPage();
      const verifier = randomBytes(48).toString('base64url');
      const params = new URLSearchParams({
        client_id: constant('clientID'), redirect_uri: redirect,
        response_type: 'code', access_type: 'offline', scope: 'openid',
        state: randomBytes(32).toString('base64url'),
        code_challenge: createHash('sha256').update(verifier).digest('base64url'),
        code_challenge_method: 'S256', countryCode: 'cn', ui_locales: 'zh-cn',
      });
      await page.goto(`${constant('AuthorizeEndpoint')}?${params}`, { waitUntil: 'domcontentloaded', timeout: 30000 });
      await page.waitForFunction(() => {
        if (location.hostname === 'id1.cloud.huawei.com' && location.pathname === '/CAS/portal/loginAuth.html') return true;
        try { return JSON.parse(document.body.innerText).error != null; } catch { return false; }
      }, { }, { timeout: 30000 });
      const location = new URL(page.url());
      if (location.hostname === 'id1.cloud.huawei.com' && location.pathname === '/CAS/portal/loginAuth.html') {
        await page.getByText('华为账号登录', { exact: true }).waitFor({ timeout: 15000 });
        results.push({ redirect, outcome: 'login-page' });
      } else {
        const error = JSON.parse(await page.locator('body').innerText());
        // Never print the URL, page body, cookies or arbitrary provider errors.
        assert.equal(typeof error.error, 'number');
        assert.equal(typeof error.sub_error, 'number');
        const descriptions = new Set(['invalid redirect_uri', 'redirect_uri not registered']);
        results.push({ redirect, outcome: 'rejected', error: error.error, sub_error: error.sub_error,
          description: descriptions.has(error.error_description) ? error.error_description : 'other provider error' });
      }
    } finally {
      await context.close();
    }
  }
} finally {
  await browser.close();
}
console.log(JSON.stringify({ checkedAt: new Date().toISOString(), results }, null, 2));
