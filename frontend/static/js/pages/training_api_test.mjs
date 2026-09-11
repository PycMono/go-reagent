import test from 'node:test';
import assert from 'node:assert/strict';
import { mutateTraining, loadTrainingSnapshot, canMutateTraining } from './training-api.js';

test('publish requires ready validated state and explicit human confirmation', async () => {
 const session = {id:'t1',status:'ready',row_version:'9007199254740993'};
 const requests=[]; const io={request:async(path,opts)=>{requests.push([path,JSON.parse(opts.body)]);return session;},newID:()=> 'r1'};
 await assert.rejects(mutateTraining(session,'publish',{change_summary:'release'},io),/确认/);
 await mutateTraining(session,'publish',{human_confirmed:true,change_summary:'release'},io);
 assert.deepEqual(requests[0],['/api/v1/training-sessions/t1/publish',{expected_row_version:'9007199254740993',request_id:'r1',human_confirmed:true,change_summary:'release'}]);
 await assert.rejects(mutateTraining({...session,status:'active'},'publish',{human_confirmed:true},io),/校验/);
});

test('failed run transport does not replay POST and keeps supplied draft', async () => {
 const draft={content:'keep me',image_urls:['https://example.test/a.png']}; let calls=0;
 await assert.rejects(mutateTraining({id:'t1',status:'active',row_version:'1'},'runs',draft,{newID:()=> 'r1',request:async()=>{calls++;throw new Error('offline');}}),/offline/);
 assert.equal(calls,1); assert.equal(draft.content,'keep me'); assert.equal(draft.image_urls.length,1);
});

test('recovery reads training-authorized messages and snapshot without posting', async () => {
 const calls=[];
 const snapshot=await loadTrainingSnapshot('t/1',async(path)=>{calls.push(path);return path.endsWith('/diff')?{text:'changes'}:path.endsWith('/messages')?{items:[{role:'user'}]}:{id:'t/1',status:'active'};});
 assert.equal(snapshot.messages.items.length,1);
 assert.deepEqual(calls,['/api/v1/training-sessions/t%2F1','/api/v1/training-sessions/t%2F1/messages','/api/v1/training-sessions/t%2F1/diff']);
});

test('busy and terminal sessions disallow mutations',()=>{
 assert.equal(canMutateTraining({status:'published'}),false);
 assert.equal(canMutateTraining({status:'active',operation:{state:'running'}}),false);
 assert.equal(canMutateTraining({status:'active'}),true);
});

test('running snapshot remains readable without racing a candidate diff',async()=>{
 const calls=[];
 const result=await loadTrainingSnapshot('t',async path=>{
  calls.push(path);
  if(path.endsWith('/diff')) throw new Error('candidate busy');
  return path.endsWith('/messages')?{items:[]}:{id:'t',status:'active',operation:{state:'running'}};
 });
 assert.equal(result.session.operation.state,'running');
 assert.equal(calls.some(path=>path.endsWith('/diff')),false);
});

test('administrator can cancel a running training operation',async()=>{
 let request;
 await mutateTraining({id:'t',row_version:'3',status:'validating',operation:{state:'running'}},'cancel',{}, {newID:()=> 'cancel-1',request:async(path,options)=>{request={path,body:JSON.parse(options.body)};}});
 assert.equal(request.path,'/api/v1/training-sessions/t/cancel');
 assert.equal(request.body.expected_row_version,'3');
});

test('training delivers split UTF-8 events before the request completes', async()=>{
 const {requestTrainingStream}=await import('./training-api.js');
 let controller;
 const body=new ReadableStream({start(c){controller=c;}});
 const events=[];
 const pending=requestTrainingStream('/runs',{method:'POST'},e=>events.push(e),async()=>new Response(body,{headers:{'Content-Type':'text/event-stream'}}));
 const bytes=new TextEncoder().encode('event: message_update\r\ndata: {"type":"message_update","delta":{"type":"text","text":"你好"}}\r\n\r\n');
 for(const byte of bytes)controller.enqueue(new Uint8Array([byte]));
 await new Promise(resolve=>setTimeout(resolve,10));
 assert.equal(events[0].delta.text,'你好');
 controller.enqueue(new TextEncoder().encode('event: run.completed\ndata: {"type":"run.completed","session":{"id":"t"}}\n\n'));
 controller.close();
 assert.equal((await pending).id,'t');
});

test('training does not treat a disconnected or failed stream as completed',async()=>{
 const {requestTrainingStream}=await import('./training-api.js');
 const fetcher=text=>async()=>new Response(text,{headers:{'Content-Type':'text/event-stream'}});
 await assert.rejects(requestTrainingStream('/runs',{},()=>{},fetcher('data: {"type":"message_start"}\n\n')),/中断/);
 await assert.rejects(requestTrainingStream('/runs',{},()=>{},fetcher('data: {"type":"run.failed","message":"训练失败"}\n\n')),/训练失败/);
 await assert.rejects(requestTrainingStream('/runs',{},()=>{},async()=>new Response('{"code":10003,"msg":"forbidden"}',{status:403})),/forbidden/);
});

test('training page renders live text and tool progress without waiting for completion',async()=>{
 const {readFile}=await import('node:fs/promises');
 const {runInNewContext}=await import('node:vm');
 const {requestTrainingStream}=await import('./training-api.js');
 class Element {
  constructor(tag='div'){this.tagName=tag;this.children=[];this.dataset={};this.textContent='';this.value='';this.hidden=false;this.disabled=false;this.listeners={};this.scrollHeight=100;}
  append(...children){for(const child of children){child.parent=this;this.children.push(child);}}
  replaceChildren(...children){this.children=[];this.append(...children);}
  remove(){if(this.parent)this.parent.children=this.parent.children.filter(child=>child!==this);}
  addEventListener(name,fn){this.listeners[name]=fn;}
  querySelectorAll(){return [];}
  setAttribute(){} focus(){} scrollIntoView(){}
 }
 const elements=new Map();const get=id=>{if(!elements.has(id))elements.set(id,new Element());return elements.get(id);};
 get('trainingApp').dataset.trainingId='t';
 let controller,finished=false;
 const body=new ReadableStream({start(c){controller=c;}});
 const session={id:'t',agent_id:'a',status:'active',row_version:'1',candidate_head:'h'};
 const request=async path=>path==='/api/v1/agent-model-options'?{items:[]}:path.endsWith('/messages')?{items:finished?[{role:'assistant',content:[{type:'text',text:'完成'}]}]:[]}:path.endsWith('/diff')?{text:''}:path.endsWith('/checkpoints?limit=20&cursor=')?{items:[]}:path==='/api/v1/agents/a'?{name:'测试'}:session;
 const sandbox={document:{getElementById:get,createElement:tag=>new Element(tag),createDocumentFragment:()=>new Element('fragment'),querySelectorAll:()=>[]},window:{addEventListener(){},confirm(){return true;}},requestAdminJSON:request,mutateTraining:(s,a,v,io)=>mutateTraining(s,a,v,{...io,fetcher:async()=>new Response(body,{headers:{'Content-Type':'text/event-stream'}})}),loadTrainingSnapshot,canMutateTraining,canCancelTraining:()=>true,createImageBlock:()=>new Element('img'),renderMessageContent:(element,text)=>{element.textContent=text;},crypto:{randomUUID:()=> 'r'},AbortController,clearTimeout,setTimeout:()=>0,console};
 const source=(await readFile(new URL('./training.js',import.meta.url),'utf8')).replace(/^import .*;\n/gm,'');
 runInNewContext(source,sandbox);
 await new Promise(resolve=>setTimeout(resolve,10));
 get('trainingInput').value='改进回答';
 const pending=get('trainingRunForm').listeners.submit({preventDefault(){}});
 const send=event=>controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(event)}\n\n`));
 send({type:'message_start'});send({type:'message_update',delta:{type:'text',text:'先查看文件'}});
 await new Promise(resolve=>setTimeout(resolve,10));
 const text=element=>element.textContent+element.children.map(text).join('');
 assert.match(text(get('trainingMessages')),/先查看文件/);
 assert.equal(get('trainingRunFields').disabled,true);
 send({type:'message_end',message:{content:[{type:'text',text:'先查看文件'}]}});
 send({type:'tool_start',tool:{call:{id:'c',name:'training_file',arguments:{action:'read',path:'AGENTS.md'}}}});
 await new Promise(resolve=>setTimeout(resolve,10));
 assert.match(text(get('trainingMessages')),/读取文件 · AGENTS.md · 执行中/);
 send({type:'tool_end',tool:{call:{id:'c',name:'training_file',arguments:{action:'read',path:'AGENTS.md'}},content:[{type:'text',text:'当前规则'}],is_error:false}});
 await new Promise(resolve=>setTimeout(resolve,10));
 assert.match(text(get('trainingMessages')),/AGENTS.md · 完成/);
 assert.match(text(get('trainingMessages')),/当前规则/);
 finished=true;send({type:'run.completed',session});controller.close();
 await pending;
 assert.match(text(get('trainingMessages')),/完成/);
 assert.equal(get('trainingRunFields').disabled,false);
});
