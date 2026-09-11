export function canMutateTraining(session) {
 return Boolean(session && ['active','ready'].includes(session.status) && session.operation?.state !== 'running');
}
export function canCancelTraining(session) { return Boolean(session&&['active','validating','ready'].includes(session.status)); }
export async function mutateTraining(session, action, values, io) {
 if (!(action==='cancel'?canCancelTraining(session):canMutateTraining(session))) throw new Error('训练正在执行或已结束，请刷新状态');
 if (action === 'publish') {
  if (!values.human_confirmed) throw new Error('请先确认人工检查');
  if (session.status !== 'ready') throw new Error('请先完成校验');
 }
 const requestID=io.newID();
 const body={expected_row_version:session.row_version,request_id:requestID,...values};
 if(action==='runs') body.run_id=requestID;
 const request=action==='runs'&&io.onEvent ? (path,options)=>requestTrainingStream(path,{...options,signal:io.signal},io.onEvent,io.fetcher) : io.request;
 return request(`/api/v1/training-sessions/${encodeURIComponent(session.id)}/${action}`,{method:action==='config'?'PATCH':'POST',body:JSON.stringify(body)});
}
export async function loadTrainingSnapshot(id, request) {
 const prefix=`/api/v1/training-sessions/${encodeURIComponent(id)}`;
 const session=await request(prefix);
 const [messages,diff]=await Promise.all([request(`${prefix}/messages`),session.operation?.state==='running'?Promise.resolve({text:'正在执行，完成后显示文件差异',changed_paths:0}):request(`${prefix}/diff`)]);
 return {session,messages,diff};
}

// POST once and consume live events; interrupted runs are never automatically replayed.
export async function requestTrainingStream(path, options, onEvent, fetcher=globalThis.fetch) {
 const response=await fetcher(path,{...options,headers:{'Content-Type':'application/json',Accept:'text/event-stream',...options.headers}});
 if(!response.ok||!response.headers.get('content-type')?.includes('text/event-stream')) {
  let envelope;try { envelope=await response.json(); } catch(_) {}
  throw new Error(envelope?.msg||'无法开始流式训练，请刷新状态');
 }
 if(!response.body)throw new Error('浏览器无法读取训练输出');
 const reader=response.body.getReader(),decoder=new TextDecoder();
 let buffer='';
 try {
  while(true) {
   const part=await reader.read();
   buffer+=decoder.decode(part.value||new Uint8Array(),{stream:!part.done});
   let boundary;
   while((boundary=/\r?\n\r?\n/.exec(buffer))) {
    const frame=buffer.slice(0,boundary.index);buffer=buffer.slice(boundary.index+boundary[0].length);
    const data=frame.split(/\r?\n/).filter(line=>line.startsWith('data:')).map(line=>line.slice(5).trimStart()).join('\n');
    if(!data)continue;
    const event=JSON.parse(data);
    await onEvent(event);
    if(event.type==='run.failed')throw new Error(event.message||'训练未完成');
    if(event.type==='run.completed')return event.session;
   }
   if(part.done)throw new Error('训练连接中断，请刷新查看已保存结果');
  }
 } finally { await reader.cancel().catch(()=>{});reader.releaseLock(); }
}
