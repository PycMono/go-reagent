import { requestAdminJSON } from './admin-agent-api.js';
import { mutateTraining, loadTrainingSnapshot, canMutateTraining, canCancelTraining } from './training-api.js';
import { createImageBlock } from './chat-image.js';
import { renderMessageContent } from './chat-message-content.js';
const $ = id => document.getElementById(id);
const trainingID = $('trainingApp').dataset.trainingId;
const prefix = `/api/v1/training-sessions/${encodeURIComponent(trainingID)}`;
const state = { session:null, streaming:false, streamAbort:null, models:[], mutating:false, cancelling:false, previewing:false, refreshing:false, timer:0, checkpointCursor:'', checkpointLoading:false, head:null, disposed:false, agentName:'Agent', previewHistory:[], messageCount:0 };
const statuses = { active:'训练中', validating:'正在校验', ready:'校验通过，可发布', published:'已发布', cancelled:'已结束', stale:'基线已更新', expired:'训练已到期' };
function node(tag,text) { const n=document.createElement(tag); if(text!==undefined)n.textContent=text; return n; }
function showError(error) { $('trainingError').textContent=error.message||'请求失败'; $('trainingError').hidden=false; }
function selectTab(name) {
 for(const button of document.querySelectorAll('[data-training-tab]')) {
  const active=button.dataset.trainingTab===name;
  button.setAttribute('aria-selected',String(active));
  $(button.getAttribute('aria-controls')).hidden=!active;
 }
}
function availability() {
 const allowed=!state.mutating&&!state.cancelling&&!state.previewing&&canMutateTraining(state.session);
 $('trainingRunFields').disabled=!allowed;
 $('trainingConfigFields').disabled=!allowed||!state.models.length;
 $('trainingValidate').disabled=!allowed;
 $('trainingCancel').disabled=state.cancelling||state.previewing||!canCancelTraining(state.session);
 $('trainingPublishFields').disabled=!allowed||state.session?.status!=='ready'||state.session?.candidate_partial;
 $('previewFields').disabled=!allowed||state.session?.candidate_partial;
 for(const button of $('trainingCheckpoints').querySelectorAll('button'))button.disabled=!allowed;
}
function schedule(delay=1200) { clearTimeout(state.timer); if(!state.disposed&&!state.streaming)state.timer=setTimeout(refresh,delay); }
function validationText(session) {
 const report=session.validation;
 if(report) {
  const lines=[report.passed?'结构与配置校验通过':'校验未通过','未经自动质量评测，发布前请人工检查回答。'];
  if(report.validated_at)lines.push(`检查时间：${new Date(report.validated_at).toLocaleString('zh-CN')}`);
  if(report.valid_until)lines.push(`有效至：${new Date(report.valid_until).toLocaleString('zh-CN')}`);
  for(const d of report.diagnostics||[])lines.push(d.message||d.code||JSON.stringify(d));
  return lines.join('\n');
 }
 if(session.operation?.kind==='validate'&&session.operation?.result)return session.operation.result.error||'校验未通过，请检查候选改动。';
 return '尚未校验。完成训练后，点击顶部「校验改动」。';
}
function renderSnapshot({session,messages,diff}) {
 if(state.session?.candidate_head!==session.candidate_head||state.session?.row_version!==session.row_version)$('trainingConfirmed').checked=false;
 if(state.session&&state.session.candidate_head!==session.candidate_head) {
  state.previewHistory=[];
  $('previewNotice').textContent='候选已更新。下一次测试会使用最新规则；可以点击「新测试」清空旧对话。';
 }
 state.session=session;
 $('trainingAgentLink').href=`/admin/agents/${encodeURIComponent(session.agent_id)}`;
 $('trainingStatus').textContent=session.operation?.state==='running'?'正在执行，请稍候':statuses[session.status]||session.status;
 $('trainingStatus').title=`到期时间：${new Date(session.expires_at).toLocaleString('zh-CN')}`;
 $('previewVersion').textContent=session.status==='published'?'已发布候选':'当前候选';
 $('trainingPartial').hidden=!session.candidate_partial;
 $('trainingDiff').textContent=diff.text||'暂无文件改动';
 $('trainingDiffInfo').textContent=`${diff.changed_paths||0} 个文件${diff.truncated?'，内容已截断':''}`;
 $('trainingValidation').textContent=validationText(session);
 if(!state.streaming) {
 const fragment=document.createDocumentFragment();
 for(const message of messages.items||[]) {
  if(message.role==='tool') { fragment.append(toolCard(message.tool_name||'文件操作', message.is_error?'失败':'完成',message.content).element);continue; }
  const article=node('article'); article.className=`training-message${message.role==='user'?' is-user':''}`;
  article.append(node('strong',message.role==='user'?'你':message.role==='tool'?'文件操作':'训练助手'));
  for(const block of message.content||[]) {
   if(block.type==='text') { const text=node('div'); renderMessageContent(text,block.text); article.append(text); }
   else if(block.type==='image'&&block.image?.url)article.append(createImageBlock(block.image.url));
  }
  fragment.append(article);
 }
 if(!messages.items?.length) {
  const welcome=node('article');welcome.className='training-message';welcome.append(node('strong','开始训练'),node('div','你希望这个 Agent 有哪些改进？可以描述回答风格、补充资料，或者指出左侧测试中发现的问题。'));
  fragment.append(welcome);
 }
 $('trainingMessages').replaceChildren(fragment);
 if(state.messageCount!==(messages.items?.length||0))$('trainingMessages').scrollTop=$('trainingMessages').scrollHeight;
 state.messageCount=messages.items?.length||0;
 }
 availability();
}
async function refresh() {
 if(state.disposed)return;
 if(state.refreshing){state.refreshAgain=true;return;}
 state.refreshing=true;
 try {
  const snapshot=await loadTrainingSnapshot(trainingID,requestAdminJSON);
  renderSnapshot(snapshot);
  if(!state.agentLoaded) {
   const agent=await requestAdminJSON(`/api/v1/agents/${encodeURIComponent(snapshot.session.agent_id)}`);
   state.agentName=agent.name;state.agentLoaded=true;
   $('trainingIdentity').textContent=agent.name;$('trainingAgentAvatar').textContent=Array.from(agent.name)[0]||'A';
  }
  if(snapshot.session.operation?.state!=='running'&&state.head!==snapshot.session.candidate_head) { state.head=snapshot.session.candidate_head; await loadCheckpoints(true); }
  if(state.mutating||snapshot.session.operation?.state==='running')schedule();
 } catch(error) { showError(error); if(state.mutating||state.session?.operation?.state==='running')schedule(3000); }
 finally { state.refreshing=false;if(state.refreshAgain){state.refreshAgain=false;schedule(0);} }
}
async function loadCheckpoints(reset=true) {
 if(state.checkpointLoading)return;
 state.checkpointLoading=true;
 if(reset) { state.checkpointCursor=''; $('trainingCheckpoints').replaceChildren(); }
 try {
  const page=await requestAdminJSON(`${prefix}/checkpoints?${new URLSearchParams({limit:'20',cursor:state.checkpointCursor})}`);
  for(const checkpoint of page.items||[]) {
   const at=checkpoint.metadata?.at;
   const item=node('li',`${at?new Date(at).toLocaleString('zh-CN'):checkpoint.head?.slice(0,8)||'检查点'}${checkpoint.metadata?.partial?'（未完成）':''}`);
   const button=node('button','恢复');button.type='button';
   button.addEventListener('click',()=>{if(window.confirm('恢复此检查点将替换当前候选改动，继续？'))perform(`checkpoints/${encodeURIComponent(checkpoint.id)}/restore`,{});});
   item.append(button);$('trainingCheckpoints').append(item);
  }
  state.checkpointCursor=page.next_cursor||'';$('checkpointsMore').hidden=!state.checkpointCursor;
  if(reset&&!page.items?.length)$('trainingCheckpoints').append(node('li','完成第一轮训练后，检查点会出现在这里。'));
 } catch(error) { showError(error); }
 finally { state.checkpointLoading=false; availability(); }
}
async function perform(action,values,onSuccess,onError) {
 const busyKey=action==='cancel'?'cancelling':'mutating';
 if(state[busyKey]||state.cancelling||state.previewing)return;
 state[busyKey]=true;availability();$('trainingError').hidden=true;schedule();
 try { await mutateTraining(state.session,action,values,{request:requestAdminJSON,newID:()=>crypto.randomUUID()});onSuccess?.(); }
 catch(error) { showError(new Error(`${error.message}。请刷新状态确认结果；不会自动重发。`));onError?.(); }
 finally { state[busyKey]=false;await refresh();availability(); }
}
function previewMessage(role,content) {
 $('previewEmpty')?.remove();
 const article=node('article');article.className=`preview-message${role==='user'?' is-user':''}`;
 const avatar=node('span',role==='user'?'我':Array.from(state.agentName)[0]||'A');avatar.className='preview-message-avatar';
 const body=node('div');const label=node('p',role==='user'?'你':state.agentName);label.className='preview-message-label';
 const text=node('div');text.className='preview-message-content';renderMessageContent(text,content);body.append(label,text);article.append(avatar,body);$('previewMessages').append(article);$('previewMessages').scrollTop=$('previewMessages').scrollHeight;
 return article;
}
$('previewRunForm').addEventListener('submit',async event=>{
 event.preventDefault();const content=$('previewInput').value.trim();if(!content||state.previewing||$('previewFields').disabled)return;
 state.previewing=true;availability();$('previewError').hidden=true;$('previewStatus').textContent='正在使用当前候选回答…';
 const draft=previewMessage('user',content);$('previewInput').value='';
 try {
  const result=await requestAdminJSON(`${prefix}/preview`,{method:'POST',body:JSON.stringify({content,history:state.previewHistory.slice(-40)})});
  previewMessage('assistant',result.content||'本次没有返回可见回复。');
  state.previewHistory.push({role:'user',content});if(result.content)state.previewHistory.push({role:'assistant',content:result.content});
 } catch(error) { draft.remove();$('previewInput').value=content;$('previewError').textContent=`${error.message}。输入已保留，请稍后重试。`;$('previewError').hidden=false; }
 finally { state.previewing=false;$('previewStatus').textContent='仅聊天测试，不修改行为';availability(); }
});
$('previewClear').addEventListener('click',()=>{if(state.previewing)return;state.previewHistory=[];$('previewMessages').replaceChildren();$('previewError').hidden=true;});
for(const button of document.querySelectorAll('[data-preview-prompt]'))button.addEventListener('click',()=>{$('previewInput').value=button.dataset.previewPrompt;$('previewInput').focus();});
for(const button of document.querySelectorAll('[data-training-tab]'))button.addEventListener('click',()=>selectTab(button.dataset.trainingTab));
$('trainingOpenPublish').addEventListener('click',()=>{selectTab('changes');$('trainingPublishForm').scrollIntoView({block:'nearest'});});
let liveMessage=null;
const liveTools=new Map();
function toolCard(name,status,content=[]) {
 const element=node('details');element.className='training-tool';
 const summary=node('summary',`${name} · ${status}`);const body=node('pre');
 body.textContent=content.filter(block=>block.type==='text').map(block=>block.text).join('\n').slice(0,4000);
 element.append(summary,body);return {element,summary,body};
}
function startLiveMessage() {
 if(liveMessage&&!liveMessage.text)liveMessage.element.remove();
 const element=node('article');element.className='training-message';
 const label=node('strong','训练助手 · 正在分析…');const body=node('div');body.className='training-live-text';
 element.append(label,body);$('trainingMessages').append(element);
 liveMessage={element,label,body,text:''};
}
function trainingEvent(event) {
 switch(event.type) {
 case 'message_start': $('trainingStatus').textContent='正在分析…';startLiveMessage();break;
 case 'message_update':
  if(event.delta?.type!=='text'||!event.delta.text)break;
  if(!liveMessage)startLiveMessage();
  liveMessage.label.textContent='训练助手 · 正在输出';
  liveMessage.text+=event.delta.text;liveMessage.body.textContent=liveMessage.text;break;
 case 'message_end':
  if(!liveMessage)startLiveMessage();
  liveMessage.text=(event.message?.content||[]).filter(block=>block.type==='text').map(block=>block.text).join('\n');
  if(liveMessage.text) { liveMessage.label.textContent='训练助手';renderMessageContent(liveMessage.body,liveMessage.text); }
  else liveMessage.element.remove();
  liveMessage=null;break;
 case 'tool_start': case 'tool_update': case 'tool_end': {
  const tool=event.tool;if(!tool?.call)break;
  if(liveMessage&&!liveMessage.text){liveMessage.element.remove();liveMessage=null;}
  let card=liveTools.get(tool.call.id);
  if(!card){card=toolCard(tool.call.name,'执行中');liveTools.set(tool.call.id,card);$('trainingMessages').append(card.element);}
  const args=tool.call.arguments||{};
  card.summary.textContent=`${tool.call.name}${args.action?' · '+({list:'列出文件',read:'读取文件',write:'写入文件',edit:'编辑文件',delete:'删除文件'}[args.action]||args.action):''}${args.path?' · '+args.path:''} · ${event.type==='tool_end'?(tool.is_error?'失败':'完成'):'执行中'}`;
  const content=event.type==='tool_update'?tool.update?.content:tool.content;
  if(content)card.body.textContent=content.filter(block=>block.type==='text').map(block=>block.text).join('\n').slice(0,4000);
  $('trainingStatus').textContent=event.type==='tool_end'?'正在继续分析…':'正在执行工具…';
  break;
 }
 case 'run.completed': $('trainingStatus').textContent='训练完成';break;
 case 'run.failed': $('trainingStatus').textContent='训练未完成';break;
 }
 $('trainingMessages').scrollTop=$('trainingMessages').scrollHeight;
}
$('trainingRunForm').addEventListener('submit',async event=>{
 event.preventDefault();const content=$('trainingInput').value;const image=$('trainingImage').value.trim();
 if(!content.trim()||state.mutating||$('trainingRunFields').disabled)return;
 state.mutating=true;state.streaming=true;clearTimeout(state.timer);state.streamAbort=new AbortController();liveTools.clear();
 availability();$('trainingError').hidden=true;
 const draft=node('article');draft.className='training-message is-user';draft.append(node('strong','你'),node('div',content));
 if(image)draft.append(createImageBlock(image));$('trainingMessages').append(draft);startLiveMessage();
 $('trainingInput').value='';$('trainingImage').value='';
 let received=false;
 try {
  await mutateTraining(state.session,'runs',{content,image_urls:image?[image]:[]},{newID:()=>crypto.randomUUID(),signal:state.streamAbort.signal,onEvent:event=>{
   trainingEvent(event);
   // Refresh the admitted row version once so cancellation uses the current version.
   if(!received){received=true;refresh();}
  }});
 } catch(error) {
  showError(error);$('trainingInput').value=content;$('trainingImage').value=image;
 } finally {
  state.streaming=false;state.mutating=false;state.streamAbort=null;liveMessage=null;
  await refresh();availability();
 }
});
$('trainingValidate').addEventListener('click',()=>{selectTab('changes');perform('validate',{});});
$('trainingPublishForm').addEventListener('submit',event=>{event.preventDefault();perform('publish',{human_confirmed:$('trainingConfirmed').checked,change_summary:$('trainingSummary').value.trim()});});
$('trainingCancel').addEventListener('click',()=>{if(window.confirm('结束这次训练？候选将不再可编辑。'))perform('cancel',{});});
$('trainingConfigForm').addEventListener('submit',event=>{event.preventDefault();const model=state.models[$('trainingModel').value];if(model)perform('config',{model_config:{provider_ref:model.provider_ref,model_id:model.model_id,parameters:model.parameters||{}}},()=>{state.previewHistory=[];});});
$('trainingRefresh').addEventListener('click',()=>{state.head=null;$('trainingError').hidden=true;refresh();});
$('checkpointsMore').addEventListener('click',()=>loadCheckpoints(false));
for(const id of ['previewInput','trainingInput'])$(id).addEventListener('keydown',event=>{if(event.key==='Enter'&&!event.shiftKey&&!event.isComposing){event.preventDefault();$(id).form.requestSubmit();}});
window.addEventListener('pagehide',()=>{state.disposed=true;clearTimeout(state.timer);state.streamAbort?.abort();});
requestAdminJSON('/api/v1/agent-model-options').then(options=>{state.models=options.items;$('trainingModel').replaceChildren(...state.models.map((model,index)=>{const option=node('option',model.model_id);option.value=String(index);return option;}));availability();}).catch(showError);
refresh();
