import { createAgentCard } from "./agent-navigation.js";

const API_PATH = "/api/v1/agents";
const PAGE_SIZE = 20;

const state = {
  items: [],
  keyword: "",
  nextCursor: "",
  loading: false,
  requestSequence: 0,
  controller: null,
  searchTimer: 0,
};

const ui = {
  form: document.getElementById("agentSearchForm"),
  search: document.getElementById("agentSearch"),
  clearSearch: document.getElementById("clearAgentSearch"),
  count: document.getElementById("agentCount"),
  loading: document.getElementById("agentLoading"),
  error: document.getElementById("agentError"),
  errorMessage: document.getElementById("agentErrorMessage"),
  retry: document.getElementById("retryAgents"),
  empty: document.getElementById("agentEmpty"),
  emptyTitle: document.getElementById("agentEmptyTitle"),
  emptyMessage: document.getElementById("agentEmptyMessage"),
  list: document.getElementById("agentList"),
  loadMore: document.getElementById("loadMoreAgents"),
};

async function requestJSON(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: { Accept: "application/json", ...(options.headers || {}) },
  });
  let envelope;
  try {
    envelope = await response.json();
  } catch (_error) {
    throw new Error("服务返回了无法识别的内容");
  }
  if (!response.ok || !envelope || envelope.code !== 0) {
    throw new Error(envelope?.msg || "Agent 列表请求失败");
  }
  if (!envelope.data || !Array.isArray(envelope.data.items)) {
    throw new Error("Agent 列表格式错误");
  }
  return envelope.data;
}

function render() {
  ui.list.replaceChildren(...state.items.map((agent) => createAgentCard(agent)));
  ui.count.textContent = state.items.length ? `已显示 ${state.items.length} 个` : "";
  ui.loadMore.hidden = !state.nextCursor || state.loading;
  ui.loadMore.disabled = state.loading;
  ui.clearSearch.hidden = !state.keyword;

  const empty = !state.loading && state.items.length === 0 && ui.error.hidden;
  ui.empty.hidden = !empty;
  if (empty && state.keyword) {
    ui.emptyTitle.textContent = "没有匹配的 Agent";
    ui.emptyMessage.textContent = "换一个名称或用途关键词再试。";
  } else if (empty) {
    ui.emptyTitle.textContent = "还没有可用的 Agent";
    ui.emptyMessage.textContent = "管理员发布 Agent 后会显示在这里。";
  }
}

async function loadAgents(reset) {
  if (state.loading && !reset) return;
  if (reset && state.controller) state.controller.abort();
  const controller = new AbortController();
  const sequence = ++state.requestSequence;
  state.controller = controller;
  state.loading = true;
  ui.error.hidden = true;
  ui.empty.hidden = true;
  if (reset) {
    state.items = [];
    state.nextCursor = "";
    ui.list.replaceChildren();
    ui.loading.hidden = false;
  }
  ui.loadMore.disabled = true;

  const query = new URLSearchParams({ keyword: state.keyword, cursor: reset ? "" : state.nextCursor, limit: String(PAGE_SIZE) });
  try {
    const result = await requestJSON(`${API_PATH}?${query.toString()}`, { signal: controller.signal });
    if (sequence !== state.requestSequence) return;
    state.items = reset ? result.items.slice() : state.items.concat(result.items);
    state.nextCursor = result.next_cursor || "";
  } catch (error) {
    if (controller.signal.aborted || sequence !== state.requestSequence) return;
    ui.errorMessage.textContent = error instanceof Error ? error.message : "请检查连接后重试。";
    ui.error.hidden = false;
  } finally {
    if (sequence === state.requestSequence) {
      state.loading = false;
      ui.loading.hidden = true;
      render();
    }
  }
}

function searchNow() {
  window.clearTimeout(state.searchTimer);
  state.keyword = ui.search.value.trim();
  loadAgents(true);
}

ui.form.addEventListener("submit", (event) => {
  event.preventDefault();
  searchNow();
});
ui.search.addEventListener("input", () => {
  window.clearTimeout(state.searchTimer);
  state.searchTimer = window.setTimeout(searchNow, 260);
});
ui.clearSearch.addEventListener("click", () => {
  ui.search.value = "";
  searchNow();
  ui.search.focus();
});
ui.retry.addEventListener("click", () => loadAgents(true));
ui.loadMore.addEventListener("click", () => loadAgents(false));

loadAgents(true);
