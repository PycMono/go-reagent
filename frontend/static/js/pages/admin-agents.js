import { buildCreateAgent, buildAgentPatch, buildModelRelease, createAgentTraining, requestAdminJSON } from './admin-agent-api.js';

const $ = id => document.getElementById(id);
const app = $('adminApp');
const mode = app.dataset.mode;
const agentID = app.dataset.agentId;
const agentPath = `/api/v1/agents/${encodeURIComponent(agentID)}`;
const state = { agent: null, templates: [], models: [], cursor: '', versionCursor: '', sequence: 0, loading: false, saving: false, versionLoading: false, trainingCursor: '', trainingLoading: false };
function element(tag, text) { const node = document.createElement(tag); if (text !== undefined) node.textContent = text; return node; }
function errorMessage(error) { $('adminErrorText').textContent = error.message || '操作失败'; $('adminError').hidden = false; }
function clearError() { $('adminError').hidden = true; }
function status(text) { $('adminStatus').textContent = text; }

async function loadList(reset = true) {
  if (!reset && state.loading) return;
  const sequence = ++state.sequence;
  state.loading = true;
  clearError();
  status('正在加载…');
  $('adminMore').disabled = true;
  if (reset) { state.cursor = ''; $('adminAgentList').replaceChildren(); $('adminEmpty').hidden = true; }
  const query = new URLSearchParams({ include_archived: 'true', keyword: $('adminSearch').value.trim(), limit: '20', cursor: state.cursor });
  try {
    const page = await requestAdminJSON(`/api/v1/agents?${query}`);
    if (sequence !== state.sequence) return;
    for (const agent of page.items) {
      const row = element('tr');
      const description = element('td');
      const identity=element('div');identity.className='admin-agent-identity';
      const avatar=element('span',Array.from(agent.name)[0]||'A');avatar.className='admin-agent-avatar';avatar.setAttribute('aria-hidden','true');
      const copy=element('div');copy.append(element('strong', agent.name), element('p', agent.description || '还没有填写用途说明'));
      identity.append(avatar,copy);description.append(identity);
      const action = element('td');
      const link = element('a', '管理');
      link.className='admin-row-link';
      link.href = `/admin/agents/${encodeURIComponent(agent.id)}`;
      link.setAttribute('aria-label', `管理 ${agent.name}`);
      action.append(link);
      const statusCell=element('td');const badge=element('span',agent.status==='enabled'?'已启用':'已停用');badge.className=`admin-badge${agent.status==='enabled'?'':' admin-badge--inactive'}`;statusCell.append(badge);
      const versionCell=element('td',agent.active_version_id?'已发布':'等待初始化');versionCell.className='admin-version-label';versionCell.title=agent.active_version_id||'';
      row.append(description,statusCell,versionCell,action);
      $('adminAgentList').append(row);
    }
    state.cursor = page.next_cursor || '';
    $('adminMore').hidden = !state.cursor;
    $('adminEmpty').hidden = $('adminAgentList').children.length !== 0;
    status(`已显示 ${$('adminAgentList').children.length} 个 Agent`);
  } catch (error) { if (sequence === state.sequence) { errorMessage(error); status('加载失败'); } }
  finally { if (sequence === state.sequence) { state.loading = false; $('adminMore').disabled = false; } }
}

function addStarter(starter = { title: '', prompt: '' }) {
  if ($('agentStartersEditor').children.length >= 8) return;
  const row = element('div'); row.className = 'admin-starter';
  const titleLabel = element('label', '问题标题');
  const title = element('input'); title.value = starter.title; title.required = true; title.maxLength = 64; title.dataset.field = 'title';
  titleLabel.append(title);
  const promptLabel = element('label', '发送内容');
  const prompt = element('textarea'); prompt.value = starter.prompt; prompt.required = true; prompt.maxLength = 1000; prompt.rows = 2; prompt.dataset.field = 'prompt';
  promptLabel.append(prompt);
  const remove = element('button', '移除问题'); remove.type = 'button';
  remove.addEventListener('click', () => { row.remove(); $('addStarter').disabled = false; });
  row.append(titleLabel, promptLabel, remove);
  $('agentStartersEditor').append(row);
  $('addStarter').disabled = $('agentStartersEditor').children.length >= 8;
}

function formValues() {
  return {
    name: $('agentName').value, description: $('agentDescription').value,
    template: $('agentTemplate')?.value, model: $('agentModel')?.value,
    status: $('agentState')?.value, icon: $('agentIcon').value, welcome: $('agentWelcome').value,
    order: Number($('agentOrder').value),
    starters: Array.from($('agentStartersEditor').children, row => ({ title: row.querySelector('[data-field="title"]').value, prompt: row.querySelector('[data-field="prompt"]').value })),
  };
}

function fillAgent(agent) {
  state.agent = agent;
  const presentation = agent.presentation || {};
  $('agentName').value = agent.name;
  $('agentDescription').value = agent.description || '';
  $('agentState').value = agent.status;
  $('agentIcon').value = presentation.icon || '';
  $('agentWelcome').value = presentation.welcome || '';
  $('agentOrder').value = presentation.order || 0;
  updateDetailAvailability();
  $('agentVersion').textContent = `正式版本：${agent.active_version_id || '尚未发布'}`;
  $('agentStartersEditor').replaceChildren();
  $('addStarter').disabled = false;
  (presentation.starters || []).forEach(addStarter);
}

async function loadVersions(reset = true) {
  if (state.versionLoading) return;
  state.versionLoading = true;
  $('versionsMore').disabled = true;
  if (reset) { state.versionCursor = ''; $('versionList').replaceChildren(); }
  try {
    const query = new URLSearchParams({ limit: '20', cursor: state.versionCursor });
    const page = await requestAdminJSON(`${agentPath}/versions?${query}`);
    for (const version of page.items) {
      const item = element('li');
      item.append(element('strong', `版本 ${version.version}${version.active ? '（当前正式版）' : ''}`));
      item.append(element('p', version.change_summary || '无发布摘要'));
      const date = new Date(version.published_at);
      item.append(element('small', `${version.published_by} · ${Number.isNaN(date.getTime()) ? version.published_at : date.toLocaleString('zh-CN')}`));
      if (!version.active) {
        const activate = element('button', '激活此版本'); activate.type = 'button'; activate.dataset.activate = 'true';
        activate.addEventListener('click', () => {
          if (window.confirm(`确认将版本 ${version.version} 设为当前正式版本？后续对话将使用该版本。`)) {
            mutateAgent(`${agentPath}/versions/${encodeURIComponent(version.id)}/activate`, { expected_row_version: state.agent.row_version });
          }
        });
        item.append(activate);
      }
      $('versionList').append(item);
    }
    state.versionCursor = page.next_cursor || '';
    $('versionsMore').hidden = !state.versionCursor;
    $('versionsEmpty').hidden = $('versionList').children.length !== 0;
  } catch (error) { errorMessage(error); }
  finally { state.versionLoading = false; $('versionsMore').disabled = false; updateDetailAvailability(); }
}

function updateDetailAvailability() {
  if (mode !== 'detail') return;
  const disabled = state.saving || state.loading || !state.agent || Boolean(state.agent.active_training_session_id);
  $('createTraining').disabled = disabled || state.agent?.status !== 'enabled';
  $('modelReleaseFields').disabled = disabled || !state.models.length || !state.agent?.active_version_id;
  for (const button of $('versionList').querySelectorAll('[data-activate]')) button.disabled = disabled;
}
async function loadReleaseModels() {
  try {
    const options = await requestAdminJSON('/api/v1/agent-model-options'); state.models = options.items;
    $('releaseModel').replaceChildren(...state.models.map((model, index) => { const option = element('option', `${model.model_id} (${model.provider_ref})`); option.value = String(index); return option; }));
  } catch (error) { errorMessage(error); }
}
async function mutateAgent(path, body) {
  if (state.saving || state.loading) return;
  state.saving = true; $('adminFields').disabled = true; updateDetailAvailability(); clearError();
  try {
    const agent = await requestAdminJSON(path, { method: 'POST', body: JSON.stringify(body) });
    fillAgent(agent); $('modelReleaseConfirmed').checked = false;
    await loadVersions(true); status('正式版本已更新');
  } catch (error) { errorMessage(error); status('未确认发布结果，请重新加载后核对；不会自动重发。'); }
  finally { state.saving = false; $('adminFields').disabled = false; updateDetailAvailability(); }
}

async function loadTrainingList(reset = true) {
  if (state.trainingLoading) return;
  state.trainingLoading = true;
  if (reset) { state.trainingCursor = ''; $('agentTrainingList').replaceChildren(); }
  try {
    const page = await requestAdminJSON(`${agentPath}/training-sessions?${new URLSearchParams({ limit: '20', cursor: state.trainingCursor })}`);
    for (const session of page.items || []) {
      const item = element('li'); const link = element('a', `${session.id} · ${session.status}`);
      link.href = `/admin/training-sessions/${encodeURIComponent(session.id)}`; item.append(link); $('agentTrainingList').append(item);
    }
    state.trainingCursor = page.next_cursor || '';
    $('trainingListMore').hidden = !state.trainingCursor;
    $('trainingListEmpty').hidden = $('agentTrainingList').children.length !== 0;
  } catch (error) { errorMessage(error); }
  finally { state.trainingLoading = false; }
}

async function loadForm() {
  if (state.loading || state.saving) return;
  state.loading = true;
  $('adminFields').disabled = true;
  clearError(); status('正在加载…');
  try {
    if (mode === 'create') {
      const [templates, options] = await Promise.all([requestAdminJSON('/api/v1/agent-templates'), requestAdminJSON('/api/v1/agent-model-options')]);
      state.templates = templates;
      state.models = options.items;
      $('agentTemplate').replaceChildren(...templates.map(template => { const option = element('option', template.name); option.value = template.code; return option; }));
      $('agentModel').replaceChildren(...options.items.map((model, index) => { const option = element('option', `${model.model_id} (${model.provider_ref})`); option.value = String(index); return option; }));
      if (!templates.length || !options.items.length) throw new Error('暂无可用模板或模型，请联系系统管理员完成配置。');
    } else {
      fillAgent(await requestAdminJSON(agentPath));
      await Promise.all([loadVersions(true), loadTrainingList(true), loadReleaseModels()]);
    }
    $('adminFields').disabled = false;
    status('');
  } catch (error) { errorMessage(error); status('加载失败'); }
  finally { state.loading = false; updateDetailAvailability(); }
}

if (mode === 'list') {
  $('adminSearchForm').addEventListener('submit', event => { event.preventDefault(); loadList(true); });
  $('adminMore').addEventListener('click', () => loadList(false));
  $('adminRetry').addEventListener('click', () => loadList(true));
  loadList(true);
} else {
  const icons = [['', '模板默认'], ['sparkles', '闪光'], ['general', '通用'], ['writing', '写作'], ['learning', '学习'], ['workplace', '工作'], ['health', '健康'], ['parenting', '育儿'], ['automotive', '汽车'], ['legal', '法律'], ...['message-circle','book-open','pen-tool','briefcase','heart','car','scale','graduation-cap','pen-line','heart-pulse','car-front','baby'].map(icon => [icon, icon])];
  $('agentIcon').replaceChildren(...icons.map(([value, label]) => { const option = element('option', label); option.value = value; return option; }));
  $('addStarter').addEventListener('click', () => addStarter());
  $('adminRetry').addEventListener('click', loadForm);
  $('modelReleaseForm')?.addEventListener('submit', event => {
    event.preventDefault();
    try { mutateAgent(`${agentPath}/model-config-releases`, buildModelRelease(state.agent, state.models[$('releaseModel').value], $('modelReleaseSummary').value, $('modelReleaseConfirmed').checked)); }
    catch (error) { errorMessage(error); }
  });
  $('createTraining')?.addEventListener('click', async () => {
    if (state.saving || state.loading || !state.agent) return;
    state.saving = true; $('createTraining').disabled = true; $('adminFields').disabled = true; clearError();
    try { const session = await createAgentTraining(state.agent); window.location.assign(`/admin/training-sessions/${encodeURIComponent(session.id)}`); }
    catch (error) { errorMessage(error); status('创建结果未确认，请重新加载训练记录后再操作。'); }
    finally { state.saving = false; $('adminFields').disabled = false; updateDetailAvailability(); }
  });
  $('trainingListMore')?.addEventListener('click', () => loadTrainingList(false));
  $('versionsMore')?.addEventListener('click', () => loadVersions(false));
  $('adminAgentForm').addEventListener('submit', async event => {
    event.preventDefault();
    if (state.saving || state.loading) return;
    state.saving = true; clearError();
    try {
      const values = formValues();
      const body = mode === 'create' ? buildCreateAgent(values, state.templates, state.models) : buildAgentPatch(state.agent, values);
      $('adminFields').disabled = true;
      status(mode === 'create' ? '正在创建…' : '正在保存…');
      const agent = await requestAdminJSON(mode === 'create' ? '/api/v1/agents' : agentPath, { method: mode === 'create' ? 'POST' : 'PATCH', body: JSON.stringify(body) });
      if (mode === 'create') { window.location.assign(`/admin/agents/${encodeURIComponent(agent.id)}`); }
      else { fillAgent(agent); status('资料已保存'); }
    } catch (error) { errorMessage(error); status('未保存。当前输入已保留；若提示版本冲突，请重新加载。'); }
    finally { state.saving = false; $('adminFields').disabled = false; updateDetailAvailability(); }
  });
  loadForm();
}
