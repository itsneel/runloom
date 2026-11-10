// Local Mailpit only. Retains one unique workflow/run/message for inspection.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
const { chromium }=await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base=process.env.RUNLOOM_UI_URL || 'http://localhost:14200';
const mailpit=process.env.RUNLOOM_MAILPIT_URL || 'http://localhost:8025';
for(const url of [base,mailpit])assert(['localhost','127.0.0.1'].includes(new URL(url).hostname),'Local test only');
const browser=await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME?{executablePath:process.env.RUNLOOM_CHROME}:{})});
try{
  const page=await browser.newPage({viewport:{width:1600,height:1000}});
  const errors=[];page.on('pageerror',error=>errors.push(error.name));
  await page.goto(base);await page.locator('#username').fill('admin@runloom.test');await page.locator('#password').fill('RunloomLocal!123');
  const ready=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/v1/studio/me' && response.ok());
  await page.locator('#kc-login').click();const response=await ready;const me=await response.json();
  const team=me.organizations.find(org=>org.name==='Runloom').teams.find(team=>team.name==='General');
  const headers={authorization:(await response.request().allHeaders()).authorization,'X-Runloom-Team':team.id};
  const api=async(method,path,data,expected=200)=>{
    const result=await page.request.fetch(`${base}${path}`,{method,headers,...(data===undefined?{}:{data})});
    assert.equal(result.status(),expected,`${method} ${path}: ${result.status()}`);return result.json();
  };
  const definition=JSON.parse(await readFile(new URL('../examples/email-notification.json',import.meta.url),'utf8'));
  definition.id=`email-local-check-${Date.now()}`;definition.name='Local email verification';
  definition.steps[0].email.subject=definition.id;
  definition.steps[0].email.bcc=['hidden@example.test'];
  await api('POST','/api/v1/studio/definitions',definition);await api('POST',`/api/v1/studio/definitions/${definition.id}/publish`,{});
  const started=await api('POST','/api/v1/studio/runs',{definition_id:definition.id,version:1,input:{email:'recipient@example.test',name:'Runloom'}},201);
  let status;
  for(let i=0;i<60;i++){
    status=await api('GET',`/api/v1/studio/runs/${started.workflow_id}`);if(status.temporal_status!=='running')break;
    await new Promise(resolve=>setTimeout(resolve,500));
  }
  assert.equal(status.temporal_status,'completed',JSON.stringify(status.state));
  assert.equal(status.state.output.delivery.accepted,true);assert.equal(status.state.output.delivery.recipient_count,2);
  const listing=await (await page.request.get(`${mailpit}/api/v1/messages`)).json();
  const email=listing.messages.find(item=>item.Subject===definition.id);assert(email,'Expected local Mailpit message');
  const message=await (await page.request.get(`${mailpit}/api/v1/message/${email.ID}`)).json();
  assert.match(message.Text,/Hello Runloom, your workflow has completed/);
  const usage=await api('GET',`/api/v1/studio/runs/${started.workflow_id}/llm-usage`);
  assert.equal(usage.summary.requests,0);assert.equal(usage.summary.complete,false);
  await page.reload();await page.locator('runloom-studio').waitFor();
  await page.getByRole('button', {name:'Run history', exact:true}).click();
  await page.locator('.history-list button').filter({hasText:definition.id}).click();
  await page.locator('runloom-run-costs').getByText('Not recorded',{exact:true}).waitFor();
  assert.deepEqual(errors,[]);
  console.log(`PASS local login, published Email workflow, Temporal SMTP delivery, Mailpit, MongoDB history and production cost panel. Sample: ${definition.id}`);
}finally{await browser.close();}
