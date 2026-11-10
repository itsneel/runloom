// Real Go connection routes and vault; synthetic identity and schema response.
// Start TestPromptBrowserAPI with PROMPT/SECRETS/DASHBOARD browser flags and the
// three independent Angular dev servers. Does not touch the application stack.
import assert from 'node:assert/strict';
const backend=process.env.RUNLOOM_PROMPT_API||'http://127.0.0.1:17290';
const fixture=await(await fetch(backend+'/fixture')).json();
const {chromium}=await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE||'playwright');
const browser=await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME?{executablePath:process.env.RUNLOOM_CHROME}:{})});
const page=await browser.newPage({viewport:{width:1500,height:1050}});page.setDefaultTimeout(15000);
const errors=[];page.on('pageerror',error=>errors.push(error.message));
await page.route('**/api/**',async route=>{
 const req=route.request(),url=new URL(req.url());
 if(url.pathname.endsWith('/auth/config'))return route.fulfill({status:503,json:{}});
 if(url.pathname.endsWith('/health'))return route.fulfill({json:{temporal:true,database:true}});
 if(url.pathname.endsWith('/connections/reporting/schema'))return route.fulfill({json:{tables:[{schema:'public',name:'customers',columns:[{name:'email',type:'text',nullable:false}]}]}});
 const response=await route.fetch({url:backend+url.pathname+url.search,headers:{...req.headers(),authorization:'Bearer admin'}});return route.fulfill({response});
});
try{
 await page.goto(process.env.RUNLOOM_UI_URL||'http://127.0.0.1:4420');
 await page.waitForFunction(()=>window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
 await page.evaluate(f=>{const s=window.ng.getComponent(document.querySelector('app-root')),a=s.auth;s.error='';a.bearer=async()=>'synthetic';a.profile.set({user:{id:'admin',email:'admin@example.test'},organizations:[{id:f.org,name:'Test org',role:'owner',teams:[{id:f.team,name:'General',role:'admin'}]}]});a.organizationId.set(f.org);a.teamId.set(f.team);a.signedIn.set(true)},fixture);
 await page.getByRole('button',{name:'Management',exact:true}).click();
 await page.getByRole('button',{name:'Database connections',exact:true}).click();
 await page.waitForURL('**/management/connections');
 const connections=page.locator('runloom-connections');
 await connections.getByRole('button',{name:'Add connection',exact:true}).click();
 await connections.locator('input[name="name"]').fill('Reporting');
 await connections.locator('input[name="id"]').fill('reporting');
 await connections.locator('input[name="host"]').fill('db.example.test');
 await connections.locator('input[name="database"]').fill('reporting');
 await connections.locator('input[name="username"]').fill('reader');
 await connections.locator('input[name="password"]').fill('synthetic-browser-password');
 await connections.getByRole('button',{name:'Save connection',exact:true}).click();
 await connections.getByText('Reporting',{exact:true}).waitFor();
 assert.ok(!(await connections.innerText()).includes('synthetic-browser-password'));
 await connections.getByRole('button',{name:'Edit',exact:true}).click();
 assert.equal(await connections.locator('input[name="password"]').inputValue(),'');
 await connections.getByRole('button',{name:'Close',exact:true}).click();
 await page.getByRole('button',{name:'Workflows',exact:true}).click();
 await page.getByRole('button',{name:'Details for Prompt test',exact:true}).click();
 await page.getByRole('button',{name:'Edit workflow',exact:true}).click();
 await page.getByRole('button',{name:'Add node',exact:true}).click();
 await page.locator('.picker-row').filter({hasText:'Query a database'}).click();
 const editor=page.locator('runloom-database-editor');
 await editor.getByLabel('Database connection',{exact:true}).selectOption('reporting');
 await editor.getByRole('button',{name:'Discover schema',exact:true}).click();
 await editor.getByLabel('Discovered table',{exact:true}).selectOption('public.customers');
 await editor.getByLabel('Database operation',{exact:true}).selectOption('sql');
 await editor.getByLabel('Database SQL',{exact:true}).fill('SELECT * FROM customers WHERE email=$1');
 await editor.getByLabel('Database JSON configuration',{exact:true}).fill('{broken');
 await editor.getByRole('alert').waitFor();
 await editor.getByLabel('Database JSON configuration',{exact:true}).fill('');
 await page.waitForFunction(()=>window.ng.getComponent(document.querySelector('runloom-studio')).editor.databaseDraft(window.ng.getComponent(document.querySelector('runloom-database-editor')).step)==='');
 await editor.getByLabel('Database JSON configuration',{exact:true}).fill(JSON.stringify({parameters:['${input.name}'],filter:{},values:{},pipeline:[]}));
 const op=await page.evaluate(()=>window.ng.getComponent(document.querySelector('runloom-database-editor')).op);
 assert.equal(op.connection_id,'reporting');assert.deepEqual(op.parameters,['${input.name}']);assert.ok(!JSON.stringify(op).includes('password'));
 assert.deepEqual(errors,[]);
 console.log('PASS browser: Management connection save/edit, credential non-disclosure, navigation, Database node, discovered table and SQL parameter editing. Schema response mocked; CRUD tested separately against live databases.');
}finally{await browser.close();await fetch(backend+'/done',{method:'POST'});}
