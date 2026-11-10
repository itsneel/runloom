// Local browser/API acceptance tests. No credentials or customer payloads are logged.
import assert from 'node:assert/strict';
import {existsSync} from 'node:fs';
import {plan} from './local-services.mjs';
const selected=plan(process.env.SERVICES_DATABASE || 'mongodb');
const base=process.env.RUNLOOM_UI_URL || `http://localhost:${selected.ports.UI_PORT}`;
assert(['localhost','127.0.0.1'].includes(new URL(base).hostname),'Local verification only');
const {chromium}=await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || new URL('../Website/node_modules/playwright/index.mjs',import.meta.url).href);
const chrome='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const browser=await chromium.launch({headless:true,...(existsSync(chrome)?{executablePath:chrome}:{})});
let phase='login', restore, memberContext;
try {
  async function login(email,password) {
    const context=await browser.newContext(),page=await context.newPage();
    await page.goto(base+'/platform/health');
    await page.locator('#username').fill(email); await page.locator('#password').fill(password);
    const pending=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1/studio/me' && r.ok());
    await page.locator('#kc-login').click();
    const response=await pending;
    const token=(await response.request().allHeaders()).authorization;
    await page.locator('runloom-platform-console h1').waitFor();
    return {context,page,token,profile:await response.json()};
  }
  const admin=await login('platform-admin@runloom.test',process.env.RUNLOOM_TEST_PLATFORM_ADMIN_PASSWORD || 'RunloomPlatformAdmin!123');
  const request=(token,path,body)=>fetch(base+'/api/v1/platform/'+path,{method:body?'PUT':'GET',headers:{Authorization:token,'Content-Type':'application/json'},...(body?{body:JSON.stringify(body)}:{}),signal:AbortSignal.timeout(15000)});
  assert.equal((await (await request(admin.token,'me')).json()).role,'platform_admin');
  await admin.page.getByRole('heading',{name:'Service health',exact:true}).waitFor();
  const health=await (await request(admin.token,'health')).json();
  assert.equal(health.items.length,6);
  for(const item of health.items) assert.equal(item.status,'healthy',item.service+' health');
  assert.equal(await admin.page.locator('runloom-assistant').count(),0,'Tenant assistant must not appear in platform console');
  assert.deepEqual(admin.profile.organizations, [], 'Platform bootstrap does not grant tenant access');
  phase='grant restricted staff in separate Angular app';
  const member=await login('member@runloom.test',process.env.RUNLOOM_TEST_MEMBER_PASSWORD || 'RunloomMember!123'); memberContext=member.context;
  const prior=(await (await request(member.token,'me')).json()).role;
  assert.equal(prior,'','Demo member must initially have no platform role; refusing to modify an existing assignment');
  assert.equal((await request(member.token,'organizations')).status,403);
  await member.page.getByRole('heading',{name:'Access restricted'}).waitFor();
  restore=async()=>{ const r=await request(admin.token,'staff',{email:'member@runloom.test',role:'',reason:'Restore demo user after platform verification'}); assert.equal(r.status,200); };
  await admin.page.getByRole('link',{name:'Staff & access'}).click();
  await admin.page.getByLabel("Existing user's email").fill('member@runloom.test');
  await admin.page.getByLabel('Reason',{exact:true}).fill('Verify restricted staff access');
  const saved=admin.page.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1/platform/staff' && r.request().method()==='PUT');
  await admin.page.getByRole('button',{name:'Update platform access'}).click(); assert.equal((await saved).status(),200);
  await member.page.reload(); await member.page.getByRole('heading',{name:'Service health',exact:true}).waitFor();
  assert.equal(await member.page.getByRole('link',{name:'Staff & access'}).count(),0);
  assert.equal(await member.page.getByRole('link',{name:'Platform audit'}).count(),0);
  const metadata=await (await request(member.token,'organizations')).json();
  assert(metadata.items.length>0);
  for(const org of metadata.items) assert.deepEqual(Object.keys(org).sort(),['id','member_count','name','team_count']);
  for(const path of ['staff','audit']) assert.equal((await request(member.token,path)).status,403);
  assert.equal((await request(member.token,'staff',{email:'member@runloom.test',role:'platform_admin',reason:'Forbidden escalation'})).status,403);
  await member.page.goto(base+'/platform/staff'); await member.page.getByRole('heading',{name:'Access restricted'}).waitFor();
  phase='last admin and trusted audit';
  const staffList=await (await request(admin.token,'staff')).json();
  // Never revoke an admin in an upgraded setup that legitimately has multiple admins.
  if (staffList.total <= staffList.items.length && staffList.items.filter(item=>item.role==='platform_admin').length===1) {
    const last=await request(admin.token,'staff',{email:'platform-admin@runloom.test',role:'',reason:'Verify last administrator protection'});
    assert.equal(last.status,400); assert.match((await last.json()).detail,/last platform administrator/);
  }
  const events=await (await request(admin.token,'audit')).json();
  assert(events.items.some(e=>e.actor===admin.profile.user.id && e.target===member.profile.user.id && e.role==='platform_staff'));
  phase='revocation and existing tenant access';
  await restore(); restore=undefined;
  assert.equal((await request(member.token,'health')).status,403,'Revocation applies to the existing bearer token');
  await member.page.goto(base+'/platform/organizations'); await member.page.getByRole('heading',{name:'Access restricted'}).waitFor();
  await member.page.getByRole('link',{name:'Back to Studio'}).click(); await member.page.locator('runloom-studio').waitFor();
  await admin.page.getByRole('link',{name:'Organizations',exact:true}).click(); await admin.page.getByRole('heading',{name:'Organizations',exact:true}).waitFor();
  await admin.page.screenshot({path:`/tmp/runloom-platform-${selected.driver}.png`,fullPage:true});
  const spec=await fetch(base+'/api/v1/platform/openapi.json'); assert.equal(spec.status,200); assert((await spec.json()).paths['/staff'].put);
  console.log(`PASS ${selected.driver}: platform admin/member login, separate federated platform app, six service checks, metadata-only staff, denied escalation/audit, last-admin rejection when the installation has only one admin, audited role changes, immediate revocation and unchanged tenant access.`);
} catch(error) { console.error(`Platform verification failed during ${phase}: ${error.name}: ${error.message}`); process.exitCode=1; }
finally { if(restore) await restore(); await memberContext?.close(); await browser.close(); }
