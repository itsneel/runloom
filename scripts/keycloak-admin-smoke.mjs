// Local-only, read-only browser check. Never log cookies, tokens or credentials.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
assert.ok(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({headless:true, ...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME} : {})});
try {
  const page = await browser.newPage({viewport:{width:1440,height:1000}});
  const failedFrames = [];
  page.on('requestfailed', request => {
    if (/\/(3p-cookies|login-status-iframe)/.test(new URL(request.url()).pathname) && request.failure()?.errorText !== 'net::ERR_ABORTED') failedFrames.push('Identity iframe failed');
  });
  const root = await page.request.get(base+'/');
  assert.equal(root.headers()['x-frame-options'], 'DENY', 'Studio must retain its frame protection');
  for (const path of ['/auth/admin/master/console/', '/auth/realms/master/protocol/openid-connect/3p-cookies/step1.html', '/auth/realms/master/protocol/openid-connect/login-status-iframe.html']) {
    const response = await page.request.get(base+path);
    assert.equal(response.status(),200);
    assert.ok(!/deny/i.test(response.headers()['x-frame-options'] || ''), 'Studio frame denial must not override identity-provider policies');
    assert.ok(response.headers()['content-security-policy']?.includes("object-src 'none'"), 'Preserve upstream CSP');
    assert.ok(response.headers()['x-content-type-options']?.includes('nosniff'));
  }
  await page.goto(base+'/auth/admin/master/console/');
  await page.locator('#username').fill('admin');
  await page.locator('#password').fill('development-only');
  await page.locator('#kc-login').click();
  await page.getByRole('link',{name:'Realm settings',exact:true}).waitFor();
  await page.getByRole('link',{name:'Manage realms',exact:true}).click();
  await page.getByRole('link',{name:'runloom',exact:true}).click();
  await page.getByRole('link',{name:'Organizations',exact:true}).click();
  await page.getByRole('heading',{name:'Organizations',exact:true}).waitFor();
  assert.deepEqual(failedFrames,[]);
  assert.equal(await page.getByText('Something went wrong',{exact:true}).count(),0);
  await page.screenshot({path:'/tmp/runloom-keycloak-admin-runloom.png',fullPage:true});
  console.log('PASS Keycloak admin browser login, Runloom realm/organization navigation, session iframe loading, upstream identity security headers and Studio frame protection.');
} catch(error) {
  console.error('Keycloak admin browser check failed: '+error.name);
  process.exitCode=1;
} finally {await browser.close();}
