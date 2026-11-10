// Local deployed services only. Leaves one synthetic tagged prompt for inspection.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
assert.ok(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({headless: true, ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {})});
try {
  const page = await browser.newPage({viewport: {width: 1440, height: 1050}});
  page.setDefaultTimeout(30000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.name));
  await page.goto(base + '/management/prompts');
  await page.locator('#username').fill('admin@runloom.test');
  await page.locator('#password').fill('RunloomLocal!123');
  const identity = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/studio/me' && response.ok());
  await page.locator('#kc-login').click();
  const response = await identity, profile = await response.json();
  const team = profile.organizations.find(org => org.name === 'Runloom').teams.find(team => team.name === 'General');
  const headers = {authorization: (await response.request().allHeaders()).authorization, 'x-runloom-team': team.id};
  const library = page.locator('runloom-prompt-library');
  const suffix = Date.now(), name = 'Demo · Tagged prompt ' + suffix, uniqueTag = 'qa-' + suffix;
  await library.getByRole('button', {name: 'New prompt', exact: true}).click();
  const editor = library.getByRole('region', {name: 'Library prompt editor'});
  await editor.getByLabel('Prompt name', {exact: true}).fill(name);
  const id = await editor.getByLabel('Prompt ID', {exact: true}).inputValue();
  await editor.getByLabel('Prompt template', {exact: true}).fill('Write a friendly customer greeting.');
  const tags = editor.getByLabel('Prompt tags', {exact: true});
  await tags.fill('a'.repeat(41));
  await editor.getByRole('button', {name: 'Publish prompt', exact: true}).click();
  await editor.locator('#prompt-tags-error').waitFor();
  assert.equal(await tags.getAttribute('aria-invalid'), 'true');
  await tags.fill('Support, BILLING, support, customer care');
  const published = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith(`/${id}/publish`));
  await editor.getByRole('button', {name: 'Publish prompt', exact: true}).click();
  const firstResponse = await published;
  assert.equal(firstResponse.status(), 200);
  const first = await firstResponse.json();
  assert.deepEqual(first.tags, ['support', 'billing', 'customer care']);
  await tags.fill('billing, onboarding, ' + uniqueTag);
  const saved = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith(`/${id}/save`));
  await editor.getByRole('button', {name: 'Save draft', exact: true}).click();
  const draft = await (await saved).json();
  assert.deepEqual(draft.tags, ['billing', 'onboarding', uniqueTag]);
  assert.equal(draft.hash, first.hash);
  const pinnedResponse = await page.request.get(`${base}/api/v1/studio/prompts/${id}?version=1`, {headers});
  assert.equal(pinnedResponse.status(), 200);
  assert.deepEqual((await pinnedResponse.json()).tags, first.tags);
  await page.reload();
  await library.getByRole('button', {name, exact: true}).click();
  assert.equal(await tags.inputValue(), 'billing, onboarding, ' + uniqueTag);
  await editor.getByRole('button', {name: 'Close editor', exact: true}).click();
  const search = library.getByRole('searchbox', {name: 'Search prompts'});
  const waitForRows = count => page.waitForFunction(count => document.querySelectorAll('runloom-prompt-library tbody tr').length === count, count);
  await search.fill(uniqueTag.toUpperCase());
  await waitForRows(1);
  await library.getByLabel('Filter prompt tag', {exact: true}).selectOption('billing');
  await waitForRows(1);
  assert.equal(await library.locator('tbody .prompt-tag').count(), 3);
  await search.fill('no matching tag');
  await waitForRows(0);
  await search.fill('');
  await library.getByLabel('Filter prompt tag', {exact: true}).selectOption('');
  await page.emulateMedia({colorScheme: 'dark'});
  for (const width of [1440, 1000, 390]) {
    await page.setViewportSize({width, height: 1050});
    for (const path of ['/management/prompts', '/management/runs']) {
      await page.goto(base + path);
      await page.locator(path.endsWith('prompts') ? 'runloom-prompt-library .toolbar' : 'runloom-run-history .toolbar').first().waitFor();
      const controls = await page.locator('.toolbar').evaluateAll(toolbars => toolbars.map(toolbar => [...toolbar.querySelectorAll('input, select, :scope > button')].map(control => {const box = control.getBoundingClientRect();return {top: box.top, bottom: box.bottom, height: box.height};})));
      for (const row of controls) for (const a of row) for (const b of row) {
        assert.ok(Math.abs(a.height - 38) < 1, `Control height at ${width}px ${path}`);
        if (Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 20)
          assert.ok(Math.abs(a.bottom - b.bottom) < 1, `Controls on the same visual row must align at ${width}px ${path}`);
      }
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `No horizontal page overflow at ${width}px ${path}`);
      if (width === 390) assert.ok(await page.locator('.table-wrap').first().evaluate(element => element.scrollWidth > element.clientWidth), 'Narrow tables must scroll rather than squeeze tags and names into individual letters');
      if (width === 1440 || width === 390) await page.screenshot({path: `/tmp/runloom-${path.split('/').at(-1)}-${width}.png`, fullPage: true});
    }
  }
  assert.deepEqual(errors, []);
  console.log('PASS live prompt tags: validation, normalization, persistence after reload, immutable published tags, tag-only search, exact filtering, and aligned prompt/run controls at desktop/tablet/mobile widths.');
} catch (error) {
  console.error('Prompt tags/layout browser check failed: ' + error.message);
  process.exitCode = 1;
} finally { await browser.close(); }
