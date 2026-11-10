// Synthetic API/profile; measures actual browser geometry without changing user data.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true, ...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME} : {})});
try {
  const page = await browser.newPage({viewport:{width:1440,height:900}});
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const profile = {user:{id:'test',name:'Icon check',email:'test@example.test'},organizations:[{id:'org',name:'Runloom',role:'owner',teams:[{id:'team',name:'Demo team',role:'admin'}]}]};
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/auth/config') return route.fulfill({status:503,json:{}});
    if (path === '/api/v1/studio/me') return route.fulfill({json:profile});
    if (path === '/api/v1/health') return route.fulfill({json:{temporal:true,database:true}});
    if (path === '/api/v1/studio/projects') return route.fulfill({json:[{id:'default',name:'General'}]});
    if (path.endsWith('/members')) return route.fulfill({json:[{email:'test@example.test',user_id:'test',role:'admin'}]});
    if (path.endsWith('/edit-lock')) return route.fulfill({json:{locked:false,writing:false,ttl_seconds:60}});
    return route.fulfill({json:[]});
  });
  await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:4200');
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(profile => {
    const shell = window.ng.getComponent(document.querySelector('app-root'));
    shell.auth.bearer = async () => 'synthetic';
    shell.auth.profile.set(profile);shell.auth.organizationId.set('org');shell.auth.teamId.set('team');shell.auth.signedIn.set(true);
  }, profile);
  await page.getByRole('button',{name:'New workflow',exact:true}).waitFor();

  async function check(label) {
    const result = await page.evaluate(() => {
      const issues = [];let count = 0;
      for (const icon of document.querySelectorAll('runloom-icon')) {
        if (!icon.checkVisibility({checkOpacity:true,checkVisibilityCSS:true})) continue;
        const r = icon.getBoundingClientRect(), svg = icon.querySelector('svg').getBoundingClientRect();
        const name = icon.getAttribute('name') || icon.className || 'dynamic';count++;
        if (r.width < 12 || Math.abs(r.width-r.height) > 0.5) issues.push(`${name}: non-square ${r.width} x ${r.height}`);
        if (Math.abs(svg.width-r.width)>0.5 || Math.abs(svg.height-r.height)>0.5) issues.push(`${name}: SVG does not fill icon`);
        const button = icon.parentElement;
        if (button.tagName === 'BUTTON') {
          const b = button.getBoundingClientRect();
          if (r.left < b.left || r.right > b.right || r.top < b.top || r.bottom > b.bottom) issues.push(`${name}: outside button`);
          if (Math.abs(r.top+r.height/2-b.top-b.height/2)>1) issues.push(`${name}: not vertically centered`);
          const visibleText = [...button.children].some(e => e.tagName !== 'RUNLOOM-ICON' && e.checkVisibility());
          if (!visibleText && !button.textContent.trim() && Math.abs(r.left+r.width/2-b.left-b.width/2)>1) issues.push(`${name}: not horizontally centered`);
        }
      }
      const chevron = document.querySelector('.workspace-chevron');
      if (chevron?.checkVisibility()) {
        const c=chevron.getBoundingClientRect(),b=chevron.parentElement.getBoundingClientRect();
        if(c.width<16 || b.right-c.right<9) issues.push('Workspace chevron is clipped or lacks right padding');
      }
      for (const select of document.querySelectorAll('select:not([multiple]):not([size]), select:not([multiple])[size="1"]')) {
        if (!select.checkVisibility()) continue;
        const css = getComputedStyle(select);
        const label = select.getAttribute('aria-label') || select.name;
        if (css.appearance !== 'none' || !css.backgroundImage.includes('data:image/svg+xml')) issues.push(`${label}: native or missing chevron`);
        if (css.backgroundSize !== '16px 16px' || css.backgroundPositionY !== '50%' || !css.backgroundPositionX.includes('12px')) issues.push(`${label}: inconsistent chevron alignment`);
        if (parseFloat(css.paddingInlineEnd) < 40) issues.push(`${label}: text overlaps chevron`);
      }
      for (const search of document.querySelectorAll('.member-search')) {
        if (!search.checkVisibility()) continue;
        const box = search.getBoundingClientRect();
        for (const part of search.querySelectorAll('runloom-icon, input')) {
          const r = part.getBoundingClientRect();
          if (Math.abs(r.top+r.height/2-box.top-box.height/2)>0.5) issues.push('Member search icon/input not centered');
        }
      }
      return {issues,count};
    });
    assert(result.count >= 3, label + ': expected rendered icons');
    assert.deepEqual(result.issues, [], label);
  }
  for (const theme of ['light','dark']) {
    await page.evaluate(theme => window.ng.getComponent(document.querySelector('app-root')).theme.setPreference(theme), theme);
    await page.waitForFunction(theme => document.documentElement.dataset.theme === theme, theme);
    await check(theme+' workflow');
    await page.getByRole('button',{name:'Switch organization or team',exact:true}).click();
    await page.getByRole('combobox',{name:'Organization',exact:true}).waitFor();
    await check(theme+' workspace switcher');
    await page.keyboard.press('Escape');
    await page.getByRole('combobox',{name:'Organization',exact:true}).waitFor({state:'hidden'});
    await page.getByRole('button',{name:'Collapse sidebar',exact:true}).click();
    await page.getByRole('button',{name:'Expand sidebar',exact:true}).waitFor();
    await check(theme+' icon rail');
    await page.getByRole('button',{name:'Expand sidebar',exact:true}).click();
    await page.getByRole('button',{name:'Management',exact:true}).click();
    await page.getByRole('heading',{name:'Teams & access',exact:true}).waitFor();
    await check(theme+' management');
    await page.getByRole('combobox',{name:'Change team role',exact:true}).waitFor();
    // Native keyboard interaction is preserved despite the custom arrow.
    const role = page.getByRole('combobox',{name:'Team member role',exact:true});
    await role.focus();
    await page.keyboard.press('e');
    await page.keyboard.press('Enter');
    assert.equal(await role.inputValue(), 'editor');
    await role.selectOption('viewer');
    await page.waitForTimeout(300);
    await page.locator('.management-card').screenshot({path:`/tmp/runloom-member-icons-${theme}.png`});
    await page.emulateMedia({forcedColors:'active'});
    assert.equal(await role.evaluate(el => getComputedStyle(el).appearance), 'auto');
    assert.equal(await role.evaluate(el => getComputedStyle(el).backgroundImage), 'none');
    await page.emulateMedia({forcedColors:'none'});
    await page.setViewportSize({width:390,height:844});
    await check(theme+' mobile members');
    await page.setViewportSize({width:1440,height:900});
    await page.getByRole('button',{name:'Organization',exact:true}).click();
    await page.getByRole('textbox',{name:'Search organization members',exact:true}).waitFor();
    await check(theme+' organization');
    await page.getByRole('button',{name:'Workflows',exact:true}).click();
    await page.getByRole('button',{name:'New workflow',exact:true}).waitFor();
  }
  await page.screenshot({path:'/tmp/runloom-icons-dark.png'});
  await page.getByRole('button',{name:'New workflow',exact:true}).click();
  await page.getByRole('button',{name:'Close node settings',exact:true}).click();
  await page.locator('.graph-toolbar').getByRole('button',{name:'Add node',exact:true}).click();
  await page.getByRole('button',{name:'Close node library',exact:true}).waitFor();
  await check('node library');
  await page.getByRole('button',{name:'Close node library',exact:true}).click();
  await page.evaluate(() => window.ng.getComponent(document.querySelector('runloom-studio')).editor.addStep('wait'));
  await page.locator('.workflow-node').waitFor();
  await check('node actions and inspector');
  await page.locator('.wire').first().hover();
  await check('inline add-node button');
  await page.setViewportSize({width:390,height:844});
  await page.getByRole('button',{name:'Toggle workspace sidebar',exact:true}).click();
  await page.getByRole('button',{name:'Close sidebar',exact:true}).waitFor();
  await check('mobile drawer');
  assert.deepEqual(errors, []);
  console.log('PASS icon geometry, search alignment, inset select chevrons, native keyboard and forced-colors support across workflows, management, icon rail, light/dark and mobile');
} finally {await browser.close();}
