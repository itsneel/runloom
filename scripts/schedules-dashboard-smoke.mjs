// Browser regression against synthetic APIs only; no real scheduled work is started.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true, ...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME} : {})});
const page = await browser.newPage({viewport:{width:1440,height:1100}});
page.setDefaultTimeout(10000);
const errors = [], calls = [];
page.on('pageerror', e => errors.push(e.message));
const schedules = new Map([['team',[]],['other',[]]]);
let failCreate = false, failDetails = false, failList = false;
const profile = {user:{id:'test',name:'Tester',email:'test@example.test'},organizations:[{id:'org',name:'Runloom',role:'owner',teams:[{id:'team',name:'Product',role:'admin'},{id:'other',name:'Other team',role:'admin'}]}]};
await page.route('**/api/**', async route => {
  const req = route.request(), path = new URL(req.url()).pathname, method = req.method(), team = req.headers()['x-runloom-team'];
  if (path.endsWith('/auth/config')) return route.fulfill({status:503,json:{}});
  if (path.endsWith('/health')) return route.fulfill({json:{database:true,temporal:true}});
  if (path.endsWith('/me')) return route.fulfill({json:profile});
  if (path.endsWith('/projects')) return route.fulfill({json:[{id:'default',name:'General'}]});
  if (path.endsWith('/edit-lock')) return route.fulfill({json:{locked:false,writing:false,ttl_seconds:60}});
  if (path.endsWith('/definitions')) return route.fulfill({json:[
    {id:'main',name:'Daily review',latest_version:2,published_kind:'main'},
    {id:'child',name:'Child',latest_version:1,published_kind:'child'},
    {id:'draft',name:'Draft only',latest_version:0},
  ]});
  if (path.includes('/schedules')) {
    assert(['team','other'].includes(team), 'schedule request must carry explicit team scope');
    calls.push({path,method,team});
    const items = schedules.get(team);
    if (path.endsWith('/schedules')) {
      if (method === 'GET') return route.fulfill(failList ? {status:503,json:{detail:'Schedule list unavailable'}} : {json:items});
      if (failCreate) return route.fulfill({status:503,json:{detail:'Schedule service unavailable'}});
      const item = {...req.postDataJSON(),id:`schedule-${items.length}`,next_runs:['2030-01-01T09:00:00Z'],recent_runs:[]};
      items.push(item); return route.fulfill({status:201,json:item});
    }
    const [, suffix] = path.split('/schedules/'), [id, action] = suffix.split('/');
    const item = items.find(s => s.id === decodeURIComponent(id));
    if (!item) return route.fulfill({status:404,json:{detail:'Schedule not found'}});
    if (method === 'DELETE') { items.splice(items.indexOf(item),1); return route.fulfill({json:{action:'delete'}}); }
    if (method === 'POST') {
      if (action === 'pause') item.paused = true;
      if (action === 'resume') item.paused = false;
      if (action === 'trigger') item.recent_runs = ['scheduled-run'];
      return route.fulfill({json:{schedule_id:id,action}});
    }
    return route.fulfill(failDetails ? {status:503,json:{detail:'Details unavailable'}} : {json:item});
  }
  if (path.endsWith('/runs/scheduled-run/history')) return route.fulfill({json:{run:{workflow_id:'scheduled-run',definition_id:'main',definition_version:2,temporal_status:'completed',created_at:'2026-10-01T00:00:00Z',input:{name:'Avery'},output:{ok:true}},events:[]}});
  return route.fulfill({json:[]});
});
const button = name => page.getByRole('button',{name,exact:true});
const modal = page.locator('runloom-dialogs').getByRole('dialog');
async function decision(accept) { await modal.getByTestId(accept ? 'dialog-confirm' : 'dialog-cancel').click(); await modal.waitFor({state:'hidden'}); }
async function dirty(expected) {
  assert.equal(await page.evaluate(() => {
    const event = new Event('beforeunload',{cancelable:true}); window.dispatchEvent(event); return event.defaultPrevented;
  }), expected);
}
async function create(name, mode = 'interval') {
  await button('New schedule').click();
  await page.getByRole('textbox',{name:'Schedule name',exact:true}).fill(name);
  await page.getByRole('combobox',{name:'Schedule frequency'}).selectOption(mode);
}
try {
  await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:4420');
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(profile => {
    const shell = window.ng.getComponent(document.querySelector('app-root')), auth = shell.auth;
    shell.error='';auth.bearer=async()=>'synthetic';auth.profile.set(profile);auth.organizationId.set('org');auth.teamId.set('team');auth.signedIn.set(true);
  },profile);
  await button('New workflow').waitFor();
  assert.equal(await button('Schedules').count(),0, 'schedules removed from workflow navigation');
  // The builder shortcut must use the normal workflow discard guard.
  await page.evaluate(() => window.ng.getComponent(document.querySelector('runloom-studio')).definition.name='Unsaved workflow');
  await page.evaluate(() => { void window.ng.getComponent(document.querySelector('runloom-studio')).openSchedules(); });
  await decision(false);
  await button('New workflow').waitFor();
  await page.evaluate(() => { void window.ng.getComponent(document.querySelector('runloom-studio')).openSchedules(); });
  await decision(true);
  await page.getByRole('heading',{name:'Workflow schedules',exact:true}).waitFor();
  await button('New schedule').waitFor();
  assert.equal(await button('Schedules').getAttribute('aria-current'),'page');
  await create('Weekday checks','cron');
  const workflow = page.getByRole('combobox',{name:'Scheduled workflow'});
  assert.equal(await workflow.locator('option[value="child"], option[value="draft"]').count(),0);
  assert.equal(await page.getByRole('spinbutton',{name:'Schedule version'}).inputValue(),'2');
  await page.getByRole('textbox',{name:'Schedule time zone'}).fill('Asia/Kolkata');
  await page.getByRole('textbox',{name:'Schedule input'}).fill('[]');
  await button('Create schedule').click();
  await page.getByText('Schedule input must be a JSON object.',{exact:true}).waitFor();
  assert.equal(calls.filter(c => c.method === 'POST').length,0);
  await page.getByRole('textbox',{name:'Schedule input'}).fill('{"name":"Avery"}');
  await page.setViewportSize({width:390,height:844});
  assert(await page.locator('runloom-schedules').evaluate(el=>el.scrollWidth<=el.clientWidth+1));
  await page.locator('.editor').screenshot({path:'/tmp/runloom-schedule-editor-mobile.png'});
  await page.setViewportSize({width:1440,height:1100});
  await dirty(true);
  for (const name of ['Close editor','Run history','Workflows','Sign out']) {
    await button(name).click(); await decision(false);
    await page.getByRole('textbox',{name:'Schedule name',exact:true}).waitFor();
  }
  await button('Switch organization or team').click();
  await page.getByRole('combobox',{name:'Team',exact:true}).selectOption('other'); await decision(false);
  assert.equal(await page.getByRole('combobox',{name:'Team',exact:true}).inputValue(),'team');
  await page.keyboard.press('Escape');
  failCreate=true; await button('Create schedule').click();
  await page.getByText('Schedule service unavailable',{exact:true}).waitFor(); await dirty(true);
  failCreate=false; await button('Create schedule').click();
  await page.locator('.schedule-card').waitFor(); await dirty(false);
  assert.equal(schedules.get('team')[0].cron,'0 9 * * 1-5');
  assert.equal(schedules.get('team')[0].time_zone,'Asia/Kolkata');
  assert.equal(schedules.get('team')[0].version,2);
  await button('Pause').click(); await button('Resume').waitFor();
  await button('Resume').click(); await button('Pause').waitFor();
  failDetails=true; await button('Run now').click();
  await page.getByText('The action succeeded, but updated details could not be loaded. Refresh schedules; do not repeat Run now.',{exact:true}).waitFor();
  assert.equal(calls.filter(c=>c.path.endsWith('/trigger')).length,1);
  failDetails=false; await button('Refresh schedules').click();
  await button('View run scheduled-run').click();
  await page.getByRole('heading',{name:'Workflow run history',exact:true}).waitFor();
  await page.locator('runloom-run-history pre').filter({hasText:'Avery'}).waitFor();
  await button('Schedules').click();
  await create('Hourly checks'); await button('Create schedule').click();
  await page.waitForFunction(()=>document.querySelectorAll('.schedule-card').length===2);
  await create('Once only','once');
  await page.getByRole('textbox',{name:'Schedule date'}).fill('2030-01-01T12:30');
  await button('Create schedule').click();
  await page.waitForFunction(()=>document.querySelectorAll('.schedule-card').length===3);
  assert.equal(schedules.get('team')[1].interval_seconds,3600);
  assert(schedules.get('team')[2].at.endsWith('Z'));
  assert.equal(await page.locator('.schedule-card h3').first().innerText(),'Hourly checks');
  await button('Name ↑').click();
  await page.waitForFunction(()=>document.querySelector('.schedule-card h3')?.textContent==='Weekday checks');
  await button('Name ↓').click();
  await page.getByRole('searchbox',{name:'Search schedules'}).fill('Weekday');
  await page.waitForFunction(()=>document.querySelectorAll('.schedule-card').length===1);
  await button('Delete').click(); await decision(false); assert.equal(schedules.get('team').length,3);
  await button('Delete').click(); await decision(true);
  await page.locator('.schedule-card').waitFor({state:'hidden'});
  assert.equal(schedules.get('team').length,2);
  await page.getByRole('searchbox',{name:'Search schedules'}).fill('');
  await page.getByRole('combobox',{name:'Status',exact:true}).selectOption('paused');
  await page.locator('.schedule-card').waitFor({state:'hidden'});
  await page.getByRole('combobox',{name:'Status',exact:true}).selectOption('');
  failList=true; await button('Refresh schedules').click();
  await page.getByText('Schedule list unavailable',{exact:true}).waitFor();
  failList=false; await button('Refresh schedules').click();
  await page.getByText('Schedule list unavailable',{exact:true}).waitFor({state:'hidden'});
  // Viewer controls stay read-only, including direct handler calls.
  await page.evaluate(() => {
    const auth=window.ng.getComponent(document.querySelector('app-root')).auth;
    const p=structuredClone(auth.profile());p.organizations[0].role='member';p.organizations[0].teams[0].role='viewer';auth.profile.set(p);
  });
  await page.waitForFunction(()=>document.querySelector('runloom-schedules button.primary').disabled);
  assert(await button('New schedule').isDisabled());
  const mutations = calls.filter(c=>c.method!=='GET').length;
  await page.evaluate(async()=> {const c=window.ng.getComponent(document.querySelector('runloom-schedules'));await c.create();await c.change(c.entries[0],'trigger');});
  assert.equal(calls.filter(c=>c.method!=='GET').length,mutations);
  for(const theme of ['light','dark']) {
    await page.evaluate(t=>window.ng.getComponent(document.querySelector('app-root')).theme.setPreference(t),theme);
    await page.waitForTimeout(300);
    await page.screenshot({path:'/tmp/runloom-schedules-'+theme+'.png'});
  }
  await button('Switch organization or team').click();
  await page.getByRole('combobox',{name:'Team',exact:true}).selectOption('other');
  await page.getByText('No schedules yet. Publish a main workflow, then create one here.',{exact:true}).waitFor();
  await page.keyboard.press('Escape');
  await page.setViewportSize({width:390,height:844});
  assert(await page.locator('runloom-schedules').evaluate(el=>el.scrollWidth<=el.clientWidth+1));
  assert.deepEqual(errors,[]);
  console.log('PASS Management navigation, schedule creation modes/actions, team scope, read-only controls, guards, errors, recent-run links and light/dark/mobile rendering (synthetic APIs)');
} finally { await page.unrouteAll({behavior:'ignoreErrors'}); await browser.close(); }
