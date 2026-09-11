import test from 'node:test';
import assert from 'node:assert/strict';
import { buildCreateAgent, buildAgentPatch, requestAdminJSON } from './admin-agent-api.js';

test('creation requires an allowed template and model and preserves model parameters', () => {
  const values = { name: ' Helper ', description: 'Research', template: 'general', model: '0', welcome: 'Hello', icon: 'sparkles', starters: [], order: 0 };
  const templates = [{ code: 'general' }];
  const models = [{ provider_ref: 'p1', model_id: 'm1', parameters: { temperature: 0.2 } }];
  assert.deepEqual(buildCreateAgent(values, templates, models), {
    name: 'Helper', description: 'Research', template_code: 'general',
    model_config: { provider_ref: 'p1', model_id: 'm1', parameters: { temperature: 0.2 } },
    presentation: { welcome: 'Hello', icon: 'sparkles', starters: [], order: 0 },
  });
  assert.throws(() => buildCreateAgent({ ...values, template: 'unknown' }, templates, models), /模板/);
  assert.throws(() => buildCreateAgent({ ...values, model: '9' }, templates, models), /模型/);
});

test('patch preserves opaque row version and presentation ordering', () => {
  const agent = { row_version: '18446744073709551614', presentation: { order: 8 } };
  const values = { name: 'Helper', description: '', welcome: '', icon: 'general', starters: [], order: 8, status: 'archived' };
  const patch = buildAgentPatch(agent, values);
  assert.equal(patch.expected_row_version, '18446744073709551614');
  assert.equal(patch.status, 'archived');
  assert.equal(patch.presentation.order, 8);
});

test('API conflict surfaces without automatically replaying a write', async () => {
  let calls = 0;
  await assert.rejects(requestAdminJSON('/api/v1/agents/a', { method: 'PATCH' }, async () => {
    calls++;
    return { ok: false, status: 409, json: async () => ({ code: 409, msg: '版本冲突，请重新加载' }) };
  }), /版本冲突/);
  assert.equal(calls, 1);
});

test('training creation uses Agent CAS and no client ownership fields', async () => {
 const { createAgentTraining } = await import('./admin-agent-api.js');
 const result=await createAgentTraining({id:'a/1',row_version:'9007199254740999'},async(path,options)=>({path,body:JSON.parse(options.body)}));
 assert.deepEqual(result,{path:'/api/v1/agents/a%2F1/training-sessions',body:{expected_row_version:'9007199254740999'}});
});

test('model release requires confirmation and binds the current version', async()=>{
 const { buildModelRelease }=await import('./admin-agent-api.js');
 const agent={row_version:'77',active_version_id:'v1'};
 const model={provider_ref:'p1',model_id:'m2',parameters:{}};
 assert.throws(()=>buildModelRelease(agent,model,'summary',false),/确认/);
 assert.deepEqual(buildModelRelease(agent,model,' summary ',true),{expected_row_version:'77',base_version_id:'v1',model_config:model,change_summary:'summary',confirmed:true});
});
