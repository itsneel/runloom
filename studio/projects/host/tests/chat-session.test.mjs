import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import {webcrypto} from 'node:crypto';
import {posix} from 'node:path';

const source = name => readFileSync(new URL('../src/app/features/assistant/'+name, import.meta.url),'utf8');
const evaluate = (name, globals={}) => {
  const exports={};
  vm.runInNewContext(ts.transpileModule(source(name),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS,experimentalDecorators:true}}).outputText,
    {exports,TextEncoder,URLSearchParams,crypto:webcrypto,setTimeout,clearTimeout,queueMicrotask,console,...globals});
  return exports;
};
const session=evaluate('chat-session.ts');
const item=()=>({id:webcrypto.randomUUID(),content:'Hello',mode:'queue',state:'sending',attempts:1,createdAt:new Date().toISOString()});
test('outbox acknowledgements use request IDs, not text',()=>{
 const a=item(),b=item();
 assert.equal(session.unacknowledged([a,b],{jobs:[{request_id:a.id}]}).length,1);
 assert.equal(session.unacknowledged([a,b],{jobs:[{request_id:a.id}]})[0].id,b.id);
});
test('message rows preserve identity through acknowledgement and streamed responses',()=>{
 const a=item(),b=item();
 const pending=session.displayMessages(undefined,[a,b]);
 assert.notEqual(pending[0].key,pending[1].key,'Equal text remains two distinct messages');
 const conversation={id:'chat',messages:[{id:'user-a',role:'user',content:a.content,job_id:'job-a',created_at:a.createdAt},{id:'answer',role:'assistant',content:'First token',job_id:'job-a',created_at:a.createdAt}],jobs:[{id:'job-a',request_id:a.id}]};
 const acknowledged=session.displayMessages(conversation,[a,b]);
 assert.equal(acknowledged.length,3,'Acknowledged pending row must not be duplicated');
 assert.equal(acknowledged[0].key,pending[0].key);assert.equal(acknowledged[0].delivery,undefined);
 assert.equal(acknowledged[2].key,pending[1].key);assert.equal(acknowledged[2].delivery.id,b.id);
 conversation.messages[1].content='First token and more';
 const streamed=session.displayMessages(conversation,[b]);
 assert.equal(streamed[1].key,acknowledged[1].key);assert.equal(streamed[1].content,'First token and more');
});
test('tab restore keeps IDs and turns interrupted sends into retryable messages',()=>{
 const a=item(),restored=session.restoreSession(JSON.stringify({draft:'Next',needsCreate:false,outbox:[a]}));
 assert.equal(restored.outbox[0].id,a.id);assert.equal(restored.outbox[0].state,'pending');assert.equal(restored.draft,'Next');
 assert.equal(session.restoreSession('{invalid'),undefined);
 assert.equal(session.restoreSession(JSON.stringify({draft:'',needsCreate:true,outbox:[{...a,mode:'execute'}]})),undefined);
 assert.notEqual(session.sessionKey('user:org:one','chat'),session.sessionKey('user:org:two','chat'));
});
function component(platform = ''){
 const signal=value=>{const fn=()=>value;fn.set=v=>{value=v};fn.update=f=>{value=f(value)};return fn;};
 const storage=new Map(),store={setItem:(k,v)=>storage.set(k,v),getItem:k=>storage.get(k)||null,removeItem:k=>storage.delete(k)};
 const auth={profile:()=>({user:{id:'user'}}),organizationId:()=> 'org',teamId:()=> 'team',team:()=>({name:'team'})};
 const effects=[];
 const listeners=new Map(),destroyCallbacks=[];
 const document={querySelector:()=>null};
 const core={Component:()=>klass=>klass,ViewChild:()=>()=>{},computed:f=>f,signal,input:signal,output:()=>({emit:()=>{}}),effect:f=>effects.push(f),inject:token=>token==='destroy'?{onDestroy:f=>destroyCallbacks.push(f)}:auth,DestroyRef:'destroy'};
 const modules=new Map([['chat-session.ts',session]]);
 const load=name=>{
  if(modules.has(name))return modules.get(name);
  const exports=evaluate(name,{
   require:specifier=>specifier==='@angular/core'?core:specifier.startsWith('.')?load(posix.normalize(posix.join(posix.dirname(name),specifier+'.ts'))):{},
   window:{addEventListener:(name,handler)=>listeners.set(name,handler),removeEventListener:name=>listeners.delete(name)},sessionStorage:store,localStorage:store,
   document,navigator:{platform},
  });
  modules.set(name,exports);return exports;
 };
 const {AssistantComponent}=load('assistant.component.ts');
 const view=new AssistantComponent();
 // The assertions exercise the same behavior across the view/session boundary.
 // Route test controls to their single owner without adding production aliases.
 const c=new Proxy(view,{
  get:(target,key)=>{const owner=key in target?target:target.chat;const value=owner[key];return typeof value==='function'&&!('set' in value)?value.bind(owner):value;},
  set:(target,key,value)=>{(key in target.chat && !(key in target)?target.chat:target)[key]=value;return true;},
 });
 c.scope='user:org:team';c.ready.set(true);c.watch=async()=>{};c.runEffects=()=>effects.forEach(f=>f());
 c.testStorage=store;c.listeners=listeners;c.testDocument=document;c.destroy=()=>destroyCallbacks.forEach(f=>f());return c;
}
const tick=()=>new Promise(resolve=>setTimeout(resolve,0));
test('Lumi toggles with Command/Ctrl+K, not L or J, and preserves modal and team guards',()=>{
 for(const [platform,modifier,hint] of [['MacIntel','metaKey','⌘ K'],['Win32','ctrlKey','Ctrl K']]){
  const c=component(platform);let toggles=0,prevented=0;
  c.toggle=()=>{toggles++};
  assert.equal(c.shortcut,hint);
  const key=overrides=>c.listeners.get('keydown')({key:'k',metaKey:false,ctrlKey:false,altKey:false,shiftKey:false,[modifier]:true,preventDefault:()=>{prevented++},...overrides});
  key({});key({key:'K'});
  assert.equal(toggles,2);assert.equal(prevented,2);
  key({key:'l'});key({key:'L'});key({key:'j'});key({[modifier]:false});key({altKey:true});key({shiftKey:true});
  c.testDocument.querySelector=()=>({});key({});
  c.testDocument.querySelector=()=>null;c.auth.team=()=>undefined;key({});
  assert.equal(toggles,2);assert.equal(prevented,2);
  c.destroy();assert.equal(c.listeners.has('keydown'),false);
 }
});
test('message view query refresh does not replay modal expansion or steal focus',async()=>{
 const c=component();let openings=0,animations=0,focuses=0;
 const dialog=()=>({isConnected:true,open:false,showModal(){this.open=true;openings++},classList:{add:()=>{animations++}}});
 c.open.set(true);c.measureOrigin=()=>{};c.composer={nativeElement:{focus:()=>{focuses++}}};
 const first=dialog();c.panel={nativeElement:first};await tick();
 c.panel={nativeElement:first};await tick();
 assert.equal(openings,1);assert.equal(animations,1);assert.equal(focuses,1);
 c.panel=undefined;c.panel={nativeElement:dialog()};await tick();
 assert.equal(openings,2);assert.equal(animations,2,'Explicit reopen still animates');
});
test('closing Spotlight keeps the draft, queued messages and active job intact',async()=>{
 const c=component();let aborted=false,focused=false;
 c.open.set(true);c.setDraft('Keep this draft');const pending=item();c.outbox.set([pending]);
 c.current.set({id:'chat',revision:1,messages:[],jobs:[{id:'job',status:'running'}]});
 c.stream={abort:()=>{aborted=true}};c.returnFocus={isConnected:true,focus:()=>{focused=true}};
 c.close();await tick();
 assert.equal(c.open(),false);assert.equal(aborted,true);assert.equal(focused,true);
 assert.equal(c.draft,'Keep this draft');assert.equal(c.outbox()[0].id,pending.id);assert.equal(c.activeJob.status,'running');
});
test('suggestions populate the composer without submitting a message',()=>{
 const c=component();let focused=false;c.composer={nativeElement:{focus:()=>{focused=true}}};
 c.suggest('Plan an approval');
 assert.equal(c.draft,'Plan an approval');assert.equal(focused,true);assert.equal(c.outbox().length,0);
});
test('scoped CSS animation completion cleans up motion and ignores child events',()=>{
 const c=component();let removed='',closed=0;
 c.panelElement={classList:{remove:value=>{removed=value}}};c.finishClose=()=>{closed++};
 c.motionEnded({target:{},animationName:'_ngcontent-test_assistant-collapse'});assert.equal(closed,0);
 c.motionEnded({target:c.panelElement,animationName:'_ngcontent-test_assistant-expand'});assert.equal(removed,'is-opening');
 c.motionEnded({target:c.panelElement,animationName:'_ngcontent-test_assistant-collapse'});assert.equal(closed,1);
});
test('Escape dismisses Spotlight but not during input method composition',()=>{
 const c=component();let closed=0;c.close=()=>{closed++};
 const event={key:'Escape',isComposing:true,preventDefault:()=>{},stopPropagation:()=>{}};
 c.keydown(event);assert.equal(closed,0);c.keydown({...event,isComposing:false});assert.equal(closed,1);
});
test('proposal application requires explicit UI eligibility and preserves the next draft',async()=>{
 const c=component();c.workflowId.set('parent');c.newID='chat';c.current.set({id:'chat',revision:1,messages:[],jobs:[]});c.draft='Next message';
 const job={id:'job',status:'completed',proposal:{name:'Child',steps:[{id:'done',type:'end'}],before_step_id:''},proposal_hash:'hash'};
 let calls=0,changed='';c.draftChanged.emit=id=>{changed=id};
 c.request=async(path,body)=>{calls++;assert.match(path,/jobs\/job\/apply/);assert.equal(body.approved,true);assert.equal(body.proposal_hash,'hash');return {id:'chat',revision:2,messages:[],jobs:[{...job,applied:{child_id:'child-job'}}]};};
 await c.applyProposal(job);assert.equal(calls,0);
 c.canApply.set(true);await c.applyProposal(job);assert.equal(calls,1);assert.equal(changed,'parent');assert.equal(c.draft,'Next message');assert.equal(c.applying(),false);
});
test('optimistic send clears only the submitted draft and keeps the next draft editable',async()=>{
 const c=component();let create,submit;const calls=[];
 c.composer={nativeElement:{value:'First',focus:()=>{}}};
 c.request=(path,body)=>{calls.push(body);return new Promise(resolve=>{if(path.includes('/messages'))submit=resolve;else create=resolve;});};
 c.setDraft('First');await c.send();assert.equal(c.draft,'');assert.equal(c.outbox()[0].content,'First');
 assert.equal(c.composer.nativeElement.value,'','Native input clears before the next render frame');
 c.setDraft('Next draft');
 create({id:c.newID,revision:1,jobs:[],messages:[]});await tick();
 const id=c.outbox()[0].id;
 assert.equal(c.enteringMessages().has(id),true);
 submit({id:c.newID,revision:2,jobs:[{id:'job',request_id:id,status:'queued'}],messages:[]});await tick();
 assert.equal(c.draft,'Next draft');assert.equal(c.outbox().length,0);assert.equal(calls[1].request_id,id);
 assert.equal(c.enteringMessages().has(id),true,'Acknowledgement must not interrupt the entrance');
 const target={};c.finishMessageEntrance({target,currentTarget:target,animationName:'_ngcontent-test_message-send-in'},id);
 assert.equal(c.enteringMessages().size,0);
});
test('a stale snapshot cannot remove new queue state or overwrite the draft',()=>{
 const c=component();c.needsCreate=false;c.setDraft('Keep me');
 c.update({id:c.newID,revision:5,jobs:[{id:'job',status:'running'}],messages:[]});
 c.update({id:c.newID,revision:2,jobs:[],messages:[]});
 assert.equal(c.current().revision,5);assert.equal(c.draft,'Keep me');
});
test('unknown delivery is retried with the original request ID and mode',async()=>{
 const c=component();c.needsCreate=false;let first=true;const sent=[];
 c.request=async(_path,body)=>{sent.push(body);if(first){first=false;throw new Error('Lost acknowledgement')}
  return {id:c.newID,revision:2,jobs:[{request_id:body.request_id}],messages:[]};};
 c.setDraft('Interrupt');await c.send('stop_and_send');await tick();
 assert.equal(c.outbox()[0].state,'failed');c.retryDelivery();await tick();
 assert.equal(sent[0].request_id,sent[1].request_id);assert.equal(sent[1].mode,'stop_and_send');assert.equal(c.outbox().length,0);
});
test('old request acknowledgements cannot overwrite a different chat or its draft',async()=>{
 const c=component();c.needsCreate=false;let acknowledge;
 c.request=()=>new Promise(resolve=>{acknowledge=resolve});
 const oldID=c.newID;c.setDraft('Old message');await c.send();
 const requestID=c.outbox()[0].id;
 c.reset();c.setDraft('New chat draft');
 acknowledge({id:oldID,revision:2,jobs:[{request_id:requestID}],messages:[]});await tick();
 assert.equal(c.current(),undefined);assert.equal(c.draft,'New chat draft');assert.equal(c.outbox().length,0);
});

test('workflow changes isolate drafts, pending requests and selected conversation history',async()=>{
 const c=component();c.workflowId.set('parent-a');c.workflowSaved.set(true);c.runEffects();c.ready.set(true);
 const scopeA=c.scope;c.needsCreate=false;let acknowledge;
 c.request=()=>new Promise(resolve=>{acknowledge=resolve});
 c.setDraft('Message for A');await c.send();const idA=c.newID,requestID=c.outbox()[0].id;c.setDraft('Draft A');
 assert.equal(new URLSearchParams(c.url('config').split('?')[1]).get('workflow_id'),'parent-a');
 c.workflowId.set('parent-b');c.runEffects();
 assert.notEqual(c.scope,scopeA);assert.equal(c.draft,'');assert.equal(c.outbox().length,0);assert.equal(c.current(),undefined);
 acknowledge({id:idA,revision:2,jobs:[{request_id:requestID}],messages:[]});await tick();
 assert.equal(c.current(),undefined);assert.equal(c.outbox().length,0);
 c.workflowId.set('parent-a');c.runEffects();c.restore(idA);
 assert.equal(c.draft,'Draft A');assert.equal(c.outbox()[0].id,requestID);
 c.workflowId.set(undefined);c.runEffects();
 assert.equal(new URLSearchParams(c.url('config').split('?')[1]).has('workflow_id'),false);assert.equal(c.outbox().length,0);
});

test('unsaved workflow cannot silently fall back to a general chat',async()=>{
 const c=component();c.workflowId.set('new-parent');c.workflowSaved.set(false);c.runEffects();
 c.request=()=>{throw new Error('Must not request general chat')};await c.load();
 assert.equal(c.ready(),false);assert.match(c.error(),/Save this workflow draft/);
});

test('full conversations stop delivery and leave the draft editable',async()=>{
 const c=component();c.needsCreate=false;
 c.current.set({id:c.newID,revision:40,jobs:Array.from({length:40},()=>({status:'completed'})),messages:[]});
 const pending={...item(),state:'pending'};c.outbox.set([pending]);c.draft='Keep drafting';
 c.request=()=>{throw new Error('Must not submit to a full conversation')};
 await c.send();await c.deliver();
 assert.match(c.conversationLimit(),/40 submissions/);assert.equal(c.draft,'Keep drafting');assert.equal(c.outbox()[0].id,pending.id);
});
test('move reconciles acknowledgements and preserves order, IDs, scope and draft across reload',async()=>{
 const c=component();c.needsCreate=false;c.workflowId.set('parent');
 const oldID=c.newID,a=item(),b=item(),d=item();c.outbox.set([a,b,d]);c.draft='Next draft';c.persist();
 c.request=async path=>{assert.ok(path.includes(oldID));assert.match(path,/workflow_id=parent/);return {id:oldID,revision:40,messages:[],jobs:[{request_id:a.id}]};};
 let deliveries=0;c.deliver=async()=>{deliveries++};
 await c.moveToNewChat();
 assert.notEqual(c.newID,oldID);assert.equal(c.needsCreate,true);assert.equal(c.current(),undefined);
 assert.equal(c.outbox().map(i=>i.id).join(','),[b.id,d.id].join(','));assert.equal(c.draft,'Next draft');assert.equal(deliveries,1);
 assert.equal(c.testStorage.getItem(session.sessionKey(c.scope,oldID)),null);
 const restored=session.restoreSession(c.testStorage.getItem(session.sessionKey(c.scope,c.newID)));
 assert.equal(restored.draft,'Next draft');assert.equal(restored.outbox[0].content,b.content);assert.equal(restored.outbox.length,2);
});
test('failed reconciliation or storage leaves unsent messages in the original chat',async()=>{
 for(const failure of ['network','storage']){
  const c=component();c.needsCreate=false;const id=c.newID,a=item();c.outbox.set([a]);c.draft='Preserve';c.persist();
  c.request=async()=>{if(failure==='network')throw new Error('offline');return {id,revision:1,messages:[],jobs:[]}};
  if(failure==='storage')c.testStorage.setItem=()=>{throw new Error('storage full')};
  c.deliver=()=>{throw new Error('Must not submit')};await c.moveToNewChat();
  assert.equal(c.newID,id);assert.equal(c.outbox()[0].id,a.id);assert.equal(c.draft,'Preserve');assert.equal(c.busy(),false);assert.match(c.error(),/Could not move messages/);
 }
});
test('new chat cannot discard an in-flight send and moves stopped sends instead',()=>{
 const c=component(),id=c.newID;c.outbox.set([item()]);let moves=0;c.moveToNewChat=()=>{moves++};
 c.busy.set(true);c.newChat();assert.equal(c.newID,id);assert.equal(moves,0);
 c.busy.set(false);c.newChat();assert.equal(moves,1);
 c.outbox.set([]);c.draft='Keep draft';c.newChat();assert.notEqual(c.newID,id);assert.equal(c.draft,'Keep draft');
});

test('general-chat creation applies only an explicitly eligible proposal and reports the created root',async()=>{
 const c=component();c.workflowId.set(undefined);c.current.set({id:'chat',revision:1,messages:[],jobs:[]});
 const job={id:'create-job',status:'completed',proposal:{operation:'create',name:'New workflow',definition:{id:'workflow-create-job',steps:[]}},proposal_hash:'exact-hash'};
 let calls=0,changed='';c.draftChanged.emit=id=>{changed=id};
 c.request=async()=>{calls++;return {id:'chat',revision:2,messages:[],jobs:[{...job,applied:{parent_id:'workflow-create-job',child_id:'',draft_only:true}}]};};
 await c.applyProposal(job);assert.equal(calls,0);
 c.canApply.set(true);await c.applyProposal(job);assert.equal(calls,1);assert.equal(changed,'workflow-create-job');
 await c.applyProposal({...job,proposal:{...job.proposal,operation:'update'}});assert.equal(calls,1,'general chat cannot update an arbitrary workflow');
});

test('authorized new draft results save automatically once; older jobs remain manual',async()=>{
 const c=component();c.newID='chat';c.canApply.set(true);let calls=0,changed='';c.draftChanged.emit=id=>{changed=id};
 const old={id:'old',status:'completed',proposal:{operation:'create',name:'Old',definition:{id:'old'}},proposal_hash:'old-hash'};
 const job={id:'new',save_draft:true,status:'completed',proposal:{operation:'create',name:'New',definition:{id:'workflow-new'}},proposal_hash:'hash'};
 c.request=async(path,body)=>{calls++;assert.match(path,/jobs\/new\/apply/);assert.equal(body.proposal_hash,'hash');return {id:'chat',revision:3,messages:[],jobs:[old,{...job,applied:{parent_id:'workflow-new',draft_only:true}}]};};
 c.update({id:'chat',revision:1,messages:[],jobs:[old]});await tick();assert.equal(calls,0);
 c.update({id:'chat',revision:2,messages:[],jobs:[old,job]});await tick();assert.equal(calls,1);assert.equal(changed,'workflow-new');
 c.update(c.current());await tick();assert.equal(calls,1);
});
test('automatic draft saving respects permission and does not loop after a save failure',async()=>{
 const c=component();c.newID='chat';let calls=0;
 const job={id:'new',save_draft:true,status:'completed',proposal:{operation:'create',name:'New'},proposal_hash:'hash'};
 const snapshot={id:'chat',revision:1,messages:[],jobs:[job]};
 c.request=async()=>{calls++;throw new Error('Workflow revision changed');};
 c.update(snapshot);await tick();assert.equal(calls,0);
 c.canApply.set(true);c.update(snapshot);await tick();assert.equal(calls,1);assert.match(c.error(),/Draft could not be saved/);
 c.update(snapshot);await tick();assert.equal(calls,1);
});

test('scoped Lumi saves through the owning editor and reads back its durable receipt',async()=>{
 const c=component();c.workflowId.set('parent');c.canApply.set(true);c.newID='chat';
 const job={id:'job',status:'completed',proposal:{operation:'update',name:'Updated'},proposal_hash:'hash'};
 c.current.set({id:'chat',revision:1,messages:[],jobs:[job]});let saves=0,reads=0;
 c.applyWorkflowDraft.set(async request=>{saves++;assert.equal(request.workflowId,'parent');assert.equal(request.proposalHash,'hash');assert.equal('token' in request,false);});
 c.request=async(path,body)=>{reads++;assert.equal(body,undefined);assert(!path.includes('/apply'));return {id:'chat',revision:2,messages:[],jobs:[{...job,applied:{parent_id:'parent',draft_only:true}}]};};
 await c.applyProposal(job);assert.equal(saves,1);assert.equal(reads,1);assert(c.current().jobs[0].applied);
});

test('a denied owning-editor save never falls back to an unleased write',async()=>{
 const c=component();c.workflowId.set('parent');c.canApply.set(true);c.newID='chat';
 const job={id:'job',status:'completed',proposal:{operation:'update',name:'Updated'},proposal_hash:'hash'};c.current.set({id:'chat',revision:1,messages:[],jobs:[job]});
 c.applyWorkflowDraft.set(async()=>{throw new Error('Editing session expired');});let calls=0;c.request=async()=>{calls++;};
 await c.applyProposal(job);assert.equal(calls,0);assert.equal(c.current().jobs[0].applied,undefined);assert(c.error());
});

test('Lumi revert uses the owning editor, records restoration and preserves newer edits on conflict',async()=>{
 const c=component();c.workflowId.set('parent');c.canApply.set(true);c.newID='chat';
 const job={id:'job',status:'completed',proposal:{operation:'update',name:'Updated'},proposal_hash:'hash',applied:{parent_id:'parent',draft_only:true}};
 c.current.set({id:'chat',revision:1,messages:[],jobs:[job]});let saves=0,reads=0;
 c.applyWorkflowDraft.set(async request=>{saves++;assert.equal(request.revert,true);assert.equal(request.jobId,'job');});
 c.request=async(path,body)=>{reads++;assert.equal(body,undefined);return {id:'chat',revision:2,messages:[],jobs:[{...job,reverted:{parent_id:'parent',draft_only:true}}]};};
 await c.chat.revertProposal(job);assert.equal(saves,1);assert.equal(reads,1);assert(c.current().jobs[0].reverted);
 await c.chat.revertProposal(c.current().jobs[0]);assert.equal(saves,1);
 c.current.set({id:'chat',revision:3,messages:[],jobs:[job]});
 c.applyWorkflowDraft.set(async()=>{throw Error('The saved draft changed. Your newer edits were kept.');});
 await c.chat.revertProposal(job);assert.equal(reads,1);assert.equal(c.current().jobs[0].reverted,undefined);assert.match(c.error(),/draft was kept/);
});
