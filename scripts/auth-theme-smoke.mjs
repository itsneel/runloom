// Presentation/navigation checks against a running local Studio and Keycloak.
// No passwords or tokens are logged, and no accounts are created by this script.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
const browser = await chromium.launch({
  headless: true,
  ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}),
});
try {
  for (const width of [1440, 768, 390]) {
    const page = await browser.newPage({ viewport: { width, height: 950 } });
    await page.goto(base + '/?project=local-test#workflow');
    await page.locator('#username').waitFor();
    assert.equal(await page.title(), 'Sign in to Runloom');
    const faviconURL = await page.locator('link[rel="icon"]').getAttribute('href');
    const favicon = await page.request.get(new URL(faviconURL, page.url()).href);
    assert.equal(favicon.status(), 200, 'Login favicon must be available');
    assert.deepEqual(await favicon.body(), readFileSync(new URL('../charts/runloom/files/keycloak-theme/runloom/login/resources/img/favicon.ico', import.meta.url)), 'Login must serve the Runloom favicon, not the inherited Keycloak icon');
    assert.equal(await page.locator('.runloom-wordmark').innerText(), 'Runloom');
    const auth = new URL(page.url());
    assert.equal(auth.searchParams.get('code_challenge_method'), 'S256');
    assert.equal(auth.searchParams.get('redirect_uri'), base + '/?project=local-test#workflow');
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    assert.equal(await page.locator('.card-pf').evaluate(el => getComputedStyle(el).borderRadius), width <= 720 ? '17px' : '22px');
    await page.getByRole('link', { name: 'Create an account', exact: true }).click();
    await page.locator('#password-confirm').waitFor();
    assert.equal(await page.locator('#kc-page-title').innerText(), 'Create your Runloom account');
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.goto(base);
    await page.getByRole('link', { name: /forgot.*password/i }).click();
    await page.locator('#username').waitFor();
    assert.equal(await page.locator('#kc-page-title').innerText(), 'Reset your password');
    await page.goto(base + '/?signedOut=1');
    await page.locator('#username').waitFor();
    assert.equal(new URL(new URL(page.url()).searchParams.get('redirect_uri')).searchParams.has('signedOut'), false);
    await page.locator('#username').fill('theme-nonexistent@runloom.invalid');
    await page.locator('#password').fill('deliberately-invalid-test-password');
    await page.locator('#kc-login').click();
    await page.getByText('Invalid username or password.', { exact: true }).waitFor();
    assert(await page.locator('.runloom-wordmark').isVisible());
    await page.close();
    console.log('PASS branded login, registration, recovery, PKCE, return URL, legacy signed-out URL redirects to login: ' + width + 'px');
  }
} catch (error) {
  // Playwright error messages can include transient authentication URLs.
  console.error('Authentication appearance smoke failed: ' + error.name);
  process.exitCode = 1;
} finally {
  await browser.close();
}
