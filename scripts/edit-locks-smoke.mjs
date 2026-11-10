// Local-only integration test. Leaves clearly named synthetic users/org/workflow.
// Credentials, bearer tokens and edit-session tokens are kept in memory only.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
const mail = process.env.RUNLOOM_MAILPIT_URL || 'http://localhost:8025';
assert(['localhost', '127.0.0.1'].includes(new URL(base).hostname), 'Local test only');
const browser = await chromium.launch({headless:true, ...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME} : {})});
let phase='registration';
async function register(name) {
  const context=await browser.newContext();
  const page=await context.newPage();
  page.setDefaultTimeout(10000);
  const user={context,page,email:`lock-${randomUUID()}@example.test`,token:''};
  page.on('response',async r=>{if(r.url().endsWith('/protocol/openid-connect/token')&&r.ok()) user.token=(await r.json()).access_token;});
  page.on('dialog', async dialog => {
    assert.equal(dialog.type(), 'beforeunload', 'In-app confirmations must use Angular dialogs');
    await dialog.accept();
  });
  await page.goto(base);
  await page.getByRole('link',{name:'Create an account',exact:true}).click();
  const password='Test!'+randomUUID();
  await page.locator('#email').fill(user.email);
  await page.locator('#password').fill(password);
  await page.locator('#password-confirm').fill(password);
  await page.locator('#firstName').fill(name);
  await page.locator('#lastName').fill('Lock Test');
  await page.locator('input[type=submit],button[type=submit]').click();
  let link;
  for(let i=0;i<40;i++) {
    const {messages=[]}=await (await fetch(mail+'/api/v1/messages')).json();
    const message=messages.find(m=>m.To?.some(to=>to.Address===user.email)&&m.Subject.toLowerCase().includes('verif'));
    if(message) {
      const body=await (await fetch(mail+'/api/v1/message/'+message.ID)).json();
      link=(body.Text||'').match(/https?:\/\/[^\s<>]+/)?.[0];break;
    }
    await delay(250);
  }
  assert(link);
  await page.goto(link);
  await page.getByRole('textbox',{name:'Organization name',exact:true}).waitFor();
  return user;
}
async function api(user,path,method='GET',body,team='',edit='',status=200) {
  assert(user.token);
  const response=await fetch(base+path,{method,headers:{Authorization:'Bearer '+user.token,'Content-Type':'application/json',...(team?{'X-Runloom-Team':team}:{}),...(edit?{'X-Runloom-Edit-Token':edit}:{})},body:body===undefined?undefined:JSON.stringify(body)});
  assert.equal(response.status,status,`${method} ${path}`);
  return response.json();
}
try {
  const alice=await register('Alice'), bob=await register('Bob');
  phase='team setup';
  const org=await api(alice,'/api/v1/studio/orgs','POST',{name:'Editing lock smoke'},'', '',201);
  const profile=await api(alice,'/api/v1/studio/me');
  const team=profile.organizations.find(o=>o.id===org.id).teams[0].id;
  await api(alice,`/api/v1/studio/orgs/${org.id}/members`,'POST',{email:bob.email,role:'member'});
  await api(alice,`/api/v1/studio/orgs/${org.id}/teams/${team}/members`,'POST',{email:bob.email,role:'editor'});
  const id='lock-'+randomUUID();
  const draft={schema_version:'1.0',id,name:'Shared lock test',trigger:{type:'manual'},steps:[{id:'echo',type:'activity',activity:'runloom.echo'}]};
  await api(alice,'/api/v1/studio/definitions','POST',draft,team);
  for(const user of [alice,bob]) {
    await user.page.goto(base);
    await user.page.getByRole('button',{name:'Details for Shared lock test',exact:true}).click();
    await user.page.locator('[aria-label="Workflow name"]:disabled').waitFor();
  }
  let ownerToken='';
  alice.page.on('request',r=>{if(r.url()===base+'/api/v1/studio/definitions'&&r.method()==='POST') ownerToken=r.headers()['x-runloom-edit-token']||'';});
  phase='exclusive editor';
  await alice.page.getByRole('button',{name:'Edit workflow',exact:true}).click();
  phase='owner edit acquisition';
  await alice.page.locator('[aria-label="Workflow name"]:enabled').waitFor();
  phase='second member blocked';
  await bob.page.getByRole('button',{name:'Edit workflow',exact:true}).click();
  await bob.page.getByText('Alice Lock Test is editing this workflow.',{exact:false}).waitFor();
  assert(await bob.page.getByLabel('Workflow name',{exact:true}).isDisabled());
  await api(bob,'/api/v1/studio/definitions','POST',draft,team,'',423);
  await api(bob,`/api/v1/studio/definitions/${id}/publish`,'POST',{},team,'',423);
  phase='second tab blocked';
  const secondTab=await alice.context.newPage();
  await secondTab.goto(base);
  await secondTab.getByRole('button',{name:'Details for Shared lock test',exact:true}).click();
  await secondTab.getByRole('button',{name:'Edit workflow',exact:true}).click();
  await secondTab.getByText('Alice Lock Test is editing this workflow.',{exact:false}).waitFor();
  assert(await secondTab.getByLabel('Workflow name',{exact:true}).isDisabled());
  await secondTab.close();
  phase='owner saves';
  await alice.page.getByLabel('Workflow name',{exact:true}).fill('Alice saved this');
  await alice.page.getByRole('button',{name:'Save',exact:true}).click();
  await alice.page.getByText('Draft committed to workflow history.',{exact:true}).waitFor();
  assert(ownerToken);
  console.log('PASS two members and same-user tabs cannot edit simultaneously; API bypass blocked');

  phase='release and latest draft';
  const released=alice.page.waitForResponse(r=>r.url().endsWith('/edit-lock')&&r.request().method()==='DELETE'&&r.ok());
  await alice.page.getByRole('button',{name:'Finish editing',exact:true}).click();
  await released;
  await bob.page.getByRole('button',{name:'Edit workflow',exact:true}).click();
  await bob.page.locator('[aria-label="Workflow name"]:enabled').waitFor();
  assert.equal(await bob.page.getByLabel('Workflow name',{exact:true}).inputValue(),'Alice saved this');
  const bobReleased=bob.page.waitForResponse(r=>r.url().endsWith('/edit-lock')&&r.request().method()==='DELETE'&&r.ok());
  await bob.page.getByRole('button',{name:'Finish editing',exact:true}).click();
  await bobReleased;
  console.log('PASS release transfers editing and reloads the latest saved draft');

  phase='lost lock recovery';
  await alice.page.getByRole('button',{name:'Edit workflow',exact:true}).click();
  await alice.page.locator('[aria-label="Workflow name"]:enabled').waitFor();
  // Capture the new session by committing once before simulating a stale tab.
  await alice.page.getByRole('button',{name:'Save',exact:true}).click();
  await alice.page.getByText('Draft committed to workflow history.',{exact:true}).waitFor();
  await alice.page.getByLabel('Workflow name',{exact:true}).fill('Unsaved content must survive');
  await api(alice,`/api/v1/studio/definitions/${id}/edit-lock`,'DELETE',undefined,team,ownerToken);
  const bobToken=randomUUID();
  await api(bob,`/api/v1/studio/definitions/${id}/edit-lock`,'POST',undefined,team,bobToken);
  await api(bob,'/api/v1/studio/definitions','POST',{...draft,name:'Newer server draft'},team,bobToken);
  await alice.page.evaluate(()=>window.dispatchEvent(new Event('focus')));
  await alice.page.locator('[aria-label="Workflow name"]:disabled').waitFor();
  assert.equal(await alice.page.getByLabel('Workflow name',{exact:true}).inputValue(),'Unsaved content must survive');
  assert(await alice.page.getByRole('button',{name:'Save',exact:true}).isDisabled());
  await api(alice,'/api/v1/studio/definitions','POST',draft,team,ownerToken,423);
  await alice.page.locator('.editor-header').getByRole('button',{name:'JSON',exact:true}).click();
  assert((await alice.page.getByLabel('Workflow JSON',{exact:true}).inputValue()).includes('Unsaved content must survive'));
  await alice.page.getByRole('button',{name:'Close JSON editor',exact:true}).click();
  await api(bob,`/api/v1/studio/definitions/${id}/edit-lock`,'DELETE',undefined,team,bobToken);
  await alice.page.getByRole('button',{name:'Edit workflow',exact:true}).click();
  await alice.page.locator('runloom-dialogs').getByTestId('dialog-confirm').click();
  await alice.page.locator('[aria-label="Workflow name"]:enabled').waitFor();
  assert.equal(await alice.page.getByLabel('Workflow name',{exact:true}).inputValue(),'Newer server draft');
  await alice.page.getByRole('button',{name:'Finish editing',exact:true}).click();
  await alice.page.locator('[aria-label="Workflow name"]:disabled').waitFor();
  console.log('PASS lost lock blocks stale saves, preserves JSON, and reacquisition reloads latest');
} catch(error) {
  console.error('Editing lock smoke failed during '+phase+': '+error.name);
  for (const context of browser.contexts()) for (const page of context.pages()) {
    if (await page.locator('runloom-studio').count()) {
      console.error('Visible editor status:', await page.locator('.edit-lock-banner, .notice').allTextContents());
      await page.screenshot({path:'var/runloom/edit-lock-debug.png',fullPage:true});
    }
  }
  process.exitCode=1;
} finally { await browser.close(); }
