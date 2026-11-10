import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { readFile } from 'node:fs/promises';

test('quick guides keep reference detail optional and preserve deep links', async ({
  page,
}, info) => {
  await page.goto('/integrations/connections');
  const details = page.locator('.technical-reference');
  await expect(details).not.toHaveAttribute('open');
  await expect(page.getByRole('heading', { name: 'Connect and query', exact: true })).toBeVisible();
  await expect(
    page.getByText('Tests execute real queries and writes.', { exact: false }).first(),
  ).toBeVisible();
  await page.screenshot({ path: info.outputPath('concise-connections.png'), fullPage: true });
  const summary = details.locator('summary');
  await summary.focus();
  await page.keyboard.press('Enter');
  await expect(details).toHaveAttribute('open');
  await expect(
    details.getByRole('heading', { name: '1. Create a connection', exact: true }),
  ).toBeVisible();
  const accessibility = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
    .analyze();
  expect(accessibility.violations).toEqual([]);
  await page.goto('/integrations/connections#1-create-a-connection');
  await expect(details).toHaveAttribute('open');
  await expect(page.locator('[id="1-create-a-connection"]')).toBeInViewport();
  await page.reload();
  await expect(details).toHaveAttribute('open');
  await page.locator('.article-pagination .next').click();
  await expect(page.locator('.technical-reference')).not.toHaveAttribute('open');
});

test('full reference remains readable without JavaScript', async ({ browser, baseURL }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  await page.goto(baseURL + '/integrations/connections');
  const details = page.locator('.technical-reference');
  await expect(details).not.toHaveAttribute('open');
  await details.locator('summary').click();
  await expect(
    details.getByRole('heading', { name: '1. Create a connection', exact: true }),
  ).toBeVisible();
  await context.close();
});

test('home matches the documentation layout and navigates to a real tutorial', async ({
  page,
}, info) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(
    'Build workflows.Make work flow.',
  );
  await page.getByRole('link', { name: 'Get started', exact: true }).click();
  await expect(page).toHaveURL(/getting-started\/docker-compose\/?$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Run Runloom locally');
  await expect(page.getByRole('navigation', { name: 'Installation method' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Run Runloom locally');
  await expect(page.locator('body')).not.toContainText('NG0500');
  expect(errors).toEqual([]);
  await page.screenshot({ path: info.outputPath('getting-started.png'), fullPage: true });
});
test('navigation, installation tabs, back and forward preserve URLs', async ({ page }, info) => {
  await page.goto('/getting-started/docker-compose');
  await page
    .getByRole('navigation', { name: 'Installation method' })
    .getByRole('link', { name: 'Kubernetes' })
    .click();
  await expect(page).toHaveURL(/getting-started\/kubernetes\/?$/);
  await page.goBack();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Run Runloom locally');
  await page.goForward();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Kubernetes');
  if (info.project.name === 'mobile')
    await page.getByRole('button', { name: 'Toggle documentation navigation' }).click();
  await page
    .getByRole('navigation', { name: 'Documentation', exact: true })
    .getByRole('link', { name: 'Lumi assistant', exact: true })
    .click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Lumi assistant');
  if (info.project.name === 'mobile')
    await expect(
      page.getByRole('button', { name: 'Toggle documentation navigation' }),
    ).toHaveAttribute('aria-expanded', 'false');
});
test('theme toggles persist and both themes have accessible contrast', async ({ page }, info) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await page.goto('/');
  for (const theme of ['dark', 'light']) {
    await page.getByRole('button', { name: `Switch to ${theme} theme` }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    const result = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze();
    expect(result.violations).toEqual([]);
    await page.screenshot({ path: info.outputPath(`home-${theme}.png`), fullPage: true });
  }
});
test('keyboard search is local, handles empty results and opens selected articles', async ({
  page,
}) => {
  await page.goto('/');
  await page.keyboard.press('Control+k');
  const input = page.getByRole('searchbox', { name: 'Search documentation' });
  await expect(input).toBeFocused();
  await input.fill('zyxwvut99999');
  await expect(page.getByText('No results for', { exact: false })).toBeVisible();
  await input.fill('database');
  await expect(page.locator('.results a').first()).toBeVisible();
  await input.press('Enter');
  await expect(page.getByRole('dialog', { name: 'Search documentation' })).not.toBeVisible();
  await expect(page).toHaveURL((url) => url.pathname !== '/');
});
test('search can retry a failed index and Escape restores focus', async ({ page }) => {
  let fail = true;
  await page.route('**/generated/search.json', (route) =>
    fail ? route.fulfill({ status: 503, body: 'Unavailable' }) : route.continue(),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Search documentation', exact: true }).click();
  await expect(page.getByText('Search could not load.', { exact: false })).toBeVisible();
  fail = false;
  await page.getByRole('button', { name: 'Retry search' }).click();
  const input = page.getByRole('searchbox', { name: 'Search documentation' });
  await input.fill('Lumi');
  await expect(page.locator('.results a').first()).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(
    page.getByRole('button', { name: 'Search documentation', exact: true }),
  ).toBeFocused();
});
test('copy buttons copy the real setup command', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.goto('/getting-started/docker-compose');
  await page.getByRole('button', { name: 'Copy code' }).first().click();
  await expect(page.getByRole('status')).toHaveText('Code copied to clipboard.');
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
    'make local-services-up SERVICES_DATABASE=mongodb',
  );
});
test('Studio destination presets persist and unsafe origins are rejected', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Studio connection', exact: true }).click();
  const input = page.getByLabel('Studio origin', { exact: true });
  await expect(input).toHaveValue('http://localhost:4200');
  await input.fill('http://public.example.com');
  await page.getByRole('button', { name: 'Save connection' }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await page.getByRole('button', { name: 'Compose · MongoDB', exact: true }).click();
  await page.getByRole('button', { name: 'Save connection' }).click();
  await page.reload();
  await expect(page.locator('.studio-link')).toHaveAttribute('href', 'http://localhost:34200');
});
test('mounted public configuration sets the Studio destination without copying credentials', async ({
  page,
}) => {
  await page.route('**/studio.json', (route) =>
    route.fulfill({ json: { studioUrl: 'https://studio.example.com' } }),
  );
  await page.goto('/');
  await expect(page.locator('.studio-link')).toHaveAttribute('href', 'https://studio.example.com');
  await expect(page.locator('.studio-link')).toHaveAttribute('target', '_blank');
  await expect(page.locator('.studio-link')).toHaveAttribute('rel', 'noopener noreferrer');
});
test('API reference loads contracts locally without operations or authorization controls', async ({
  page,
  baseURL,
}) => {
  const external: string[] = [];
  page.on('request', (request) => {
    if (new URL(request.url()).origin !== new URL(baseURL!).origin) external.push(request.url());
  });
  await page.goto('/api/platform');
  const frame = page.frameLocator('iframe');
  await page.locator('iframe').scrollIntoViewIfNeeded();
  await expect(frame.locator('.opblock').first()).toBeVisible();
  await expect(frame.getByRole('button', { name: /Authorize|Try it out|Execute/ })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Open installed Swagger' })).toHaveAttribute(
    'href',
    'http://localhost:4200/api/docs/platform/',
  );
  expect(external).toEqual([]);
});
test('prerendered article content works with JavaScript disabled and unknown URLs are 404', async ({
  browser,
  request,
  baseURL,
}) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  await page.goto(baseURL + '/guides/first-workflow');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Your first workflow');
  await expect(page.getByRole('heading', { name: 'Save and test' })).toBeVisible();
  expect((await request.get('/does-not-exist')).status()).toBe(404);
  await context.close();
});
test('every published article has real static HTML at its own URL', async ({ request }) => {
  const pages = JSON.parse(
    await readFile(new URL('../../content/pages.json', import.meta.url), 'utf8'),
  );
  for (const article of pages) {
    const response = await request.get('/' + article.slug);
    expect(response.status(), article.slug).toBe(200);
    expect(await response.text(), article.slug).toContain('class="prose"');
  }
});
test('article controls and search dialog pass automated accessibility checks', async ({ page }) => {
  await page.goto('/getting-started/docker-compose');
  expect(
    (await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze())
      .violations,
  ).toEqual([]);
  await page.getByRole('button', { name: 'Search documentation', exact: true }).click();
  await page.getByRole('searchbox', { name: 'Search documentation' }).fill('Lumi');
  await expect(page.locator('.results a').first()).toBeVisible();
  expect(
    (await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze())
      .violations,
  ).toEqual([]);
});
test('long guides fit mobile widths and expose working table-of-contents anchors', async ({
  page,
}, info) => {
  await page.goto('/nodes/reference');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  if (info.project.name === 'desktop') {
    await page.locator('.technical-reference summary').click();
    const link = page.locator('.page-aside a').filter({ hasText: 'Database' }).first();
    await link.click();
    await expect(page).toHaveURL(/#database$/);
    await expect(page.locator('h3#database')).toBeInViewport();
  }
});
