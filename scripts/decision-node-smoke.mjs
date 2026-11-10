// Isolated builder regression; synthetic session/API, no billable calls.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true, ...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME} : {})});
const page = await browser.newPage({viewport:{width:1600,height:1000}});
const errors = [];
page.on('pageerror', e => errors.push(e.message));
let saved = {schema_version:'1.0',id:'decision-test',name:'Decision test',kind:'main',version:0,trigger:{type:'manual'},input_schema:{type:'object'},steps:[],output:null};
await page.route('**/api/**', route => {
  const path = new URL(route.request().url()).pathname, method = route.request().method();
  const reply = json => route.fulfill({json});
  if(path.endsWith('/auth/config')) return route.fulfill({status:503,json:{}});
  if(path.endsWith('/health')) return reply({temporal:true,database:true});
  if(path.endsWith('/edit-lock')) return reply({locked:method!=='DELETE',owner:'tester',owner_name:'Tester',ttl_seconds:90,writing:false});
  if(path.endsWith('/projects')) return reply([{id:'default',name:'General'}]);
  if(path.endsWith('/definitions') && method==='GET') return reply([{id:saved.id,name:saved.name,latest_version:0,has_draft:true,project_id:'default'}]);
  if(path.endsWith('/definitions') && method==='POST'){saved=route.request().postDataJSON();return reply(saved);}
  if(path.endsWith('/definitions/decision-test')) return reply(saved);
  return reply([]);
});
const app = expression => page.evaluate(expression => new Function('app',`return (${expression})`)(window.ng.getComponent(document.querySelector('runloom-studio'))),expression);
try {
  await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:4200');
  await page.waitForFunction(()=>window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(()=>{
    const auth=window.ng.getComponent(document.querySelector('app-root')).auth;
    auth.bearer=async ()=>'synthetic-token';
    auth.profile.set({user:{id:'tester',email:'test@example.test',name:'Tester'},organizations:[{id:'org',name:'Test org',role:'owner',teams:[{id:'team',name:'Test team',role:'admin'}]}]});
    auth.organizationId.set('org');auth.teamId.set('team');auth.signedIn.set(true);
  });
  await page.getByRole('button',{name:/^Details for /}).first().click();
  await page.getByRole('button',{name:'Edit workflow',exact:true}).click();
  await page.getByRole('button',{name:'Add node',exact:true}).click();
  await page.locator('.picker-row').filter({has:page.locator('strong',{hasText:/^AI Decision$/})}).click();
  assert.equal(await app('app.editor.selectedStep.type'),'decision');
  await page.locator('.branch-editor > section').nth(1).waitFor();
  assert.equal(await page.locator('.branch-editor > section').count(),2);
  const mode=page.getByRole('combobox',{name:'Decision mode',exact:true});
  assert.equal(await mode.inputValue(),'single');
  await page.locator('.join-node').getByText('Join · selected actions').waitFor();
  await page.getByRole('textbox',{name:'Decision action 1 description',exact:true}).fill('Use for routine customer requests');
  await mode.selectOption('multiple');
  await page.getByRole('combobox',{name:'Decision execution',exact:true}).selectOption('parallel');
  assert.equal(await app('app.editor.selectedStep.decision.execution'),'parallel');
  await mode.selectOption('agent');
  assert.equal(await app('app.editor.selectedStep.decision.execution'),'sequential');
  await page.getByRole('spinbutton',{name:'Decision round limit',exact:true}).fill('4');
  await page.getByRole('spinbutton',{name:'Decision action limit',exact:true}).fill('3');
  await page.locator('.join-node').getByText('Decide again · until finished').waitFor();
  await page.getByRole('button',{name:'Save',exact:true}).click();
  await page.waitForFunction(()=>!window.ng.getComponent(document.querySelector('runloom-studio')).hasUnsavedChanges);
  assert.deepEqual(saved.steps[0].decision,{mode:'agent',execution:'sequential',max_rounds:4,max_actions:3});
  assert.equal(saved.steps[0].branches[0].description,'Use for routine customer requests');
  await app("app.openDefinition('decision-test', true)");
  assert.deepEqual(await app('app.definition.steps[0].decision'),saved.steps[0].decision);
  await page.getByRole('button',{name:'Edit workflow',exact:true}).click();
  await page.locator(`[data-node-id="${saved.steps[0].id}"] .node-select`).click();
  assert.equal(await mode.inputValue(),'agent');
  await page.locator('.inspector').getByRole('button',{name:'Add node to respond',exact:true}).click();
  await page.locator('.picker-row').filter({has:page.locator('strong',{hasText:/^AI Decision$/})}).click();
  assert.match(await app('app.message'),/cannot be nested/);
  assert.equal(await app('app.definition.steps[0].branches[0].steps.length'),1);
  await page.getByRole('button',{name:'Close node library'}).click();
  await page.getByRole('button',{name:'Center canvas',exact:true}).click();
  if(process.env.RUNLOOM_DECISION_SCREENSHOT) await page.screenshot({path:process.env.RUNLOOM_DECISION_SCREENSHOT});
  assert.deepEqual(errors,[]);
  console.log('PASS AI Decision palette, named action paths, all modes, parallel settings, limits and save/reload');
} finally {await browser.close();}
