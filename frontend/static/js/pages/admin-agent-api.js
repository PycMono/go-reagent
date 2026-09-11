export async function requestAdminJSON(path, options = {}, fetcher = globalThis.fetch) {
  const response = await fetcher(path, {
    ...options,
    headers: { Accept: 'application/json', 'Content-Type': 'application/json', ...(options.headers || {}) },
  });
  let envelope;
  try { envelope = await response.json(); } catch (_) { throw new Error('服务返回了无法识别的内容'); }
  if (!response.ok || envelope?.code !== 0) throw new Error(envelope?.msg || '请求失败，请重新加载后重试');
  return envelope.data;
}

function presentation(values) {
  return { icon: values.icon, welcome: values.welcome, starters: values.starters, order: values.order };
}

export function buildCreateAgent(values, templates, models) {
  if (!templates.some(item => item.code === values.template)) throw new Error('请选择可用模板');
  const model = models[values.model];
  if (!model) throw new Error('请选择允许的模型');
  return {
    name: values.name.trim(), description: values.description.trim(), template_code: values.template,
    model_config: { provider_ref: model.provider_ref, model_id: model.model_id, parameters: model.parameters || {} },
    presentation: presentation(values),
  };
}

export function buildAgentPatch(agent, values) {
  return { expected_row_version: agent.row_version, name: values.name.trim(), description: values.description.trim(),
    status: values.status, presentation: presentation(values) };
}

export async function createAgentTraining(agent, request = requestAdminJSON) {
 return request(`/api/v1/agents/${encodeURIComponent(agent.id)}/training-sessions`, {method:'POST',body:JSON.stringify({expected_row_version:agent.row_version})});
}

export function buildModelRelease(agent, model, summary, confirmed) {
 if(!confirmed) throw new Error('请先确认发布模型变更');
 if(!model||!agent.active_version_id||!summary.trim())throw new Error('请选择模型并填写发布摘要');
 return {expected_row_version:agent.row_version,base_version_id:agent.active_version_id,model_config:{provider_ref:model.provider_ref,model_id:model.model_id,parameters:model.parameters||{}},change_summary:summary.trim(),confirmed:true};
}
