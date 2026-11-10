// Browser regression with synthetic API/session. No provider calls or credentials.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME} : {})});
const page = await browser.newPage({viewport:{width:1600,height:1050}});
const errors=[]; page.on('pageerror',e=>errors.push(e.message));
const backend=process.env.RUNLOOM_PROMPT_API;
const fixture=backend ? await (await fetch(backend+'/fixture')).json() : {team:'team',org:'org'};
let saved={schema_version:'1.0',id:'prompt-test',name:'Prompt test',kind:'main',version:0,trigger:{type:'manual'},input_schema:{type:'object',properties:{name:{type:'string'}}},steps:[{id:'ask',type:'llm',model:'default',prompt:'Hello',system_prompt:''}],output:null};
let entry; const history=[]; const versions=new Map();
await page.route('**/api/**',route=>{
 const path=new URL(route.request().url()).pathname, method=route.request().method();
 const reply=json=>route.fulfill({json});
 if(path.endsWith('/auth/config'))return route.fulfill({status:503,json:{}});
 if(path.endsWith('/health'))return reply({temporal:true,database:true});
 if(backend) return route.fetch({url:backend+path+new URL(route.request().url()).search,headers:{...route.request().headers(),authorization:'Bearer admin','X-Runloom-Team':fixture.team}}).then(response=>route.fulfill({response}));
 if(path.endsWith('/edit-lock'))return reply({locked:method!=='DELETE',owner:'tester',owner_name:'Tester',ttl_seconds:90,writing:false});
 if(path.endsWith('/projects'))return reply([{id:'default',name:'General'}]);
 if(path.endsWith('/definitions') && method==='GET')return reply([{id:saved.id,name:saved.name,latest_version:0,has_draft:true,project_id:'default'}]);
 if(path.endsWith('/definitions') && method==='POST'){saved=route.request().postDataJSON();return reply(saved);}
 if(path.endsWith('/definitions/prompt-test'))return reply(saved);
 if(path.endsWith('/variables'))return reply([{path:'input.name',type:'string',optional:true,description:'Customer name'}]);
 if(path.endsWith('/prompts'))return reply(entry?[entry]:[]);
 if(path.endsWith('/prompts/greeting/history'))return reply(history);
 if(path.endsWith('/prompts/greeting')){const v=Number(new URL(route.request().url()).searchParams.get('version'));return reply(v?versions.get(v):entry);}
 if(path.endsWith('/prompts/greeting/publish') || path.endsWith('/prompts/greeting/save')){
  const published=path.endsWith('/publish');entry={...route.request().postDataJSON(),revision:(entry?.revision||0)+1,version:(entry?.version||0)+(published?1:0),published,hash:'test-hash',actor:'tester',updated_at:new Date().toISOString()};
  history.unshift(structuredClone(entry));if(published)versions.set(entry.version,structuredClone(entry));return reply(entry);
 }
 if(path.endsWith('/prompts/preview')){const b=route.request().postDataJSON();assert.equal(b.inputs.value1,'Avery');return reply({prompt:'Hello Avery',system_prompt:'',prompt_hash:'test-hash'});}
 return reply([]);
});
const app=expression=>page.evaluate(expression=>new Function('app',`return (${expression})`)(window.ng.getComponent(document.querySelector('runloom-studio'))),expression);
try {
 await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:4200');
 await page.waitForFunction(()=>window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
 await page.evaluate(({team,org})=>{const auth=window.ng.getComponent(document.querySelector('app-root')).auth;auth.bearer=async()=>'synthetic-token';auth.profile.set({user:{id:'admin',email:'admin@example.test',name:'Tester'},organizations:[{id:org,name:'Test org',role:'owner',teams:[{id:team,name:'Test team',role:'admin'}]}]});auth.organizationId.set(org);auth.teamId.set(team);auth.signedIn.set(true);},fixture);
 await page.waitForFunction(()=>document.querySelector('runloom-studio') && !!window.ng?.getComponent(document.querySelector('runloom-studio')));
 if(backend) { await page.waitForTimeout(400); await app("app.openDefinition('prompt-test', true)"); }
 await page.getByRole('button',{name:/^Details for /}).first().click();
 await page.getByRole('button',{name:'Edit workflow',exact:true}).click();
 await page.locator('[data-node-id="ask"] .node-select').click();
 const editor=page.locator('runloom-prompt-editor');
 const prompt=editor.getByRole('textbox',{name:'Prompt and context',exact:true});
 await prompt.fill('Hello ${input.na');await prompt.press('Control+Space');
 await editor.getByRole('option').filter({hasText:'input.name'}).waitFor();
 await prompt.press('Enter');assert.equal(await prompt.inputValue(),'Hello ${input.name}');
 await editor.getByRole('button',{name:'Use typed template',exact:true}).click();
 assert.equal(await app('app.editor.selectedStep.prompt_template.text'),'Hello ${vars.value1}');
 assert.equal(await editor.getByRole('textbox',{name:'Map value1 (string)',exact:true}).inputValue(),'${input.name}');
 await editor.getByText('Preview and test',{exact:true}).click();
 await editor.getByRole('textbox',{name:'Sample inputs (JSON)',exact:true}).fill('{"value1":"Avery"}');
 await editor.getByRole('button',{name:'Preview rendered prompt',exact:true}).click();
 await editor.locator('pre').filter({hasText:'Hello Avery'}).waitFor();
 // Library authoring has its own management dashboard; seed a published prompt
 // here so this test remains focused on the node picker and typed editor.
 entry={id:'greeting',name:'Greeting',description:'',revision:0,template:await app('app.editor.selectedStep.prompt_template')};
 if(backend){const result=await fetch(backend+'/api/v1/studio/prompts/greeting/publish',{method:'POST',headers:{authorization:'Bearer admin','X-Runloom-Team':fixture.team,'Content-Type':'application/json'},body:JSON.stringify(entry)});assert.equal(result.status,200);entry=await result.json();}
 else entry={...entry,revision:1,version:1,published:true,hash:'test-hash'};
 history.push(structuredClone(entry));versions.set(1,structuredClone(entry));
 await editor.getByRole('button',{name:'Prompt library',exact:true}).click();
 await editor.getByRole('combobox',{name:'Library prompt',exact:true}).selectOption('greeting');
 await editor.getByRole('button',{name:'Use v1',exact:true}).first().click();
 await editor.getByText('Library: greeting · pinned v1',{exact:false}).waitFor();
 assert.equal(await editor.getByRole('textbox',{name:'Prompt template',exact:true}).getAttribute('readonly'),'');
 await editor.getByRole('button',{name:'Make local copy',exact:true}).click();
 await editor.getByRole('textbox',{name:'Prompt template',exact:true}).fill('Welcome ${vars.value1}');
 assert.equal(await app('app.hasUnsavedChanges'),true);
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await page.waitForFunction(()=>!window.ng.getComponent(document.querySelector('runloom-studio')).hasUnsavedChanges);
 if(backend) { const read=async path=>(await fetch(backend+path,{headers:{authorization:'Bearer admin','X-Runloom-Team':fixture.team}})).json();saved=await read('/api/v1/studio/definitions/prompt-test?version=0');versions.set(1,await read('/api/v1/studio/prompts/greeting?version=1')); }
 assert.equal(saved.steps[0].prompt_template.text,'Welcome ${vars.value1}');assert.equal(versions.get(1).template.text,'Hello ${vars.value1}');
 assert.deepEqual(errors,[]);
 if(process.env.RUNLOOM_PROMPT_SCREENSHOT)await page.screenshot({path:process.env.RUNLOOM_PROMPT_SCREENSHOT});
 console.log('PASS prompt autocomplete keyboard, typed conversion/mappings, preview, library picker/pin/detach and workflow dirty/save state');
}finally{await page.unrouteAll({behavior:'ignoreErrors'});await browser.close();if(backend)await fetch(backend+'/done',{method:'POST'});}
