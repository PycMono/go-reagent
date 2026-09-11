import { isVisibleChatMessage } from "./chat-visibility.js";
import { renderMessageContent } from "./chat-message-content.js";
import { createChatStream } from "./chat-stream.js";
import { createImageBlock } from "./chat-image.js";
import { assertConversationAgent, clearChatDraft, resolveChatTarget, sendSelectedAgentMessage, updateChatAvailability } from "./agent-navigation.js";

const API_ROOT = "/api/v1/conversations";
const AGENT_API = "/api/v1/agents";

const state = {
  conversations: [],
  selectedAgentID: "",
  selectedAgent: null,
  readOnly: false,
  conversationCursor: "",
  messageCursor: "",
  currentConversationId: "",
  activeConversation: null,
  running: false,
  runId: "",
  runAbort: null,
  streamingMessage: null,
  activityItems: new Map(),
  searchTimer: 0,
};

const ui = {
  body: document.body,
  sidebar: document.getElementById("conversationSidebar"),
  sidebarOpen: document.getElementById("sidebarOpen"),
  sidebarClose: document.getElementById("sidebarClose"),
  sidebarScrim: document.getElementById("sidebarScrim"),
  newChat: document.getElementById("newChatBtn"),
  search: document.getElementById("conversationSearch"),
  agentFilter: document.getElementById("agentFilter"),
  conversationList: document.getElementById("conversationList"),
  conversationCount: document.getElementById("conversationCount"),
  loadMoreConversations: document.getElementById("loadMoreConversations"),
  title: document.getElementById("sessionTitle"),
  sessionProfile: document.getElementById("sessionProfile"),
  messages: document.getElementById("chatMessages"),
  welcome: document.getElementById("chatWelcome"),
  welcomeAgentIcon: document.getElementById("welcomeAgentIcon"),
  agentWelcomeTitle: document.getElementById("agentWelcomeTitle"),
  agentWelcomeDescription: document.getElementById("agentWelcomeDescription"),
  agentLoadError: document.getElementById("agentLoadError"),
  agentStarters: document.getElementById("agentStarters"),
  loadOlderMessages: document.getElementById("loadOlderMessages"),
  readOnlyNotice: document.getElementById("readOnlyNotice"),
  runStatus: document.getElementById("runStatus"),
  composer: document.getElementById("chatComposer"),
  input: document.getElementById("chatInput"),
  imageURL: document.getElementById("chatImageURL"),
  send: document.getElementById("sendBtn"),
  toast: document.getElementById("toast"),
};

async function requestJSON(path, options) {
  const response = await fetch(path, Object.assign({
    headers: { "Accept": "application/json" },
  }, options || {}));
  let body = null;
  try {
    body = await response.json();
  } catch (_error) {
    throw new Error("服务返回了无法识别的内容");
  }
  if (!response.ok || !body || body.code !== 0) {
    throw new Error(body && body.msg ? body.msg : "请求失败");
  }
  return body.data;
}

function showToast(message) {
  ui.toast.textContent = message;
  ui.toast.hidden = false;
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(function () {
    ui.toast.hidden = true;
  }, 3600);
}

function formatTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit",
  }).format(date);
}

function agentSymbol(agent) {
  return agent && agent.icon ? agent.icon : "A";
}

function currentConversation() {
  return state.conversations.find(function (item) { return item.id === state.currentConversationId; }) || state.activeConversation;
}

function updateSendAvailability() {
  updateChatAvailability(state, ui);
}

function renderAgentFilter() {
  ui.agentFilter.replaceChildren();
  const all = document.createElement("option");
  all.value = "";
  all.textContent = "全部 Agent";
  ui.agentFilter.appendChild(all);
  if (state.selectedAgent) {
    const option = document.createElement("option");
    option.value = state.selectedAgent.id;
    option.textContent = agentSymbol(state.selectedAgent) + " " + state.selectedAgent.name;
    ui.agentFilter.appendChild(option);
  }
  ui.agentFilter.value = "";
}

function renderAgentStarters(agent) {
  ui.agentStarters.replaceChildren();
  if (state.currentConversationId || !agent || !Array.isArray(agent.starters) || agent.starters.length === 0) {
    ui.agentStarters.hidden = true;
    return;
  }
  ui.agentStarters.hidden = false;
  agent.starters.forEach(function (starter) {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = starter.title;
    button.addEventListener("click", function () {
      ui.input.value = starter.prompt || "";
      resizeComposer();
      ui.input.focus();
      ui.input.setSelectionRange(ui.input.value.length, ui.input.value.length);
    });
    ui.agentStarters.appendChild(button);
  });
}

function renderSessionProfile() {
  if (!state.selectedAgent) {
    ui.sessionProfile.hidden = true;
    ui.sessionProfile.textContent = "";
    return;
  }
  ui.sessionProfile.textContent = agentSymbol(state.selectedAgent) + " " + state.selectedAgent.name;
  ui.sessionProfile.hidden = false;
}

function renderWelcome() {
  const conversation = currentConversation();
  const agent = state.selectedAgent;
  ui.agentLoadError.hidden = Boolean(agent);
  if (agent) {
    ui.welcomeAgentIcon.textContent = agentSymbol(agent);
    ui.agentWelcomeTitle.textContent = agent.welcome || (conversation ? "继续与 " + agent.name + " 对话" : "开始与 " + agent.name + " 对话");
    ui.agentWelcomeDescription.textContent = state.readOnly
      ? "这个历史会话可以查看，但当前 Agent 已归档或停用。"
      : (agent.description || "此对话将固定使用该 Agent。");
  } else {
    ui.welcomeAgentIcon.textContent = "!";
    ui.agentWelcomeTitle.textContent = "Agent 加载失败";
    ui.agentWelcomeDescription.textContent = "返回 Agent 目录后重新选择。";
  }
  renderAgentStarters(agent);
  updateSendAvailability();
}

function agentFromConversation(conversation) {
  return {
    id: conversation.agent_id,
    name: conversation.agent_name || "Agent",
    icon: conversation.agent_icon || "A",
    status: conversation.agent_status || "archived",
    selectable: conversation.agent_status === "enabled",
    description: "此历史会话固定使用 " + (conversation.agent_name || "该 Agent") + "。",
    welcome: "继续与 " + (conversation.agent_name || "Agent") + " 对话",
    starters: [],
  };
}

async function loadNewAgent(agentID) {
  const agent = await requestJSON(AGENT_API + "/" + encodeURIComponent(agentID));
  if (!agent || agent.id !== agentID) throw new Error("Agent 响应与所选目标不一致");
  state.selectedAgentID = agent.id;
  state.selectedAgent = agent;
  state.readOnly = !agent.selectable || agent.status !== "enabled";
  ui.title.textContent = agent.name;
  renderAgentFilter();
  renderSessionProfile();
  renderMessages([]);
}

function closeSidebar() {
  ui.body.classList.remove("sidebar-open");
}

function renderConversationList() {
  ui.conversationList.replaceChildren();
  ui.conversationCount.textContent = String(state.conversations.length);
  if (state.conversations.length === 0) {
    const empty = document.createElement("p");
    empty.className = "qb-chat__history-empty";
    empty.textContent = ui.search.value.trim() || ui.agentFilter.value ? "没有找到匹配的对话" : "发出第一条消息后，对话会出现在这里。";
    ui.conversationList.appendChild(empty);
    return;
  }
  state.conversations.forEach(function (conversation) {
    const item = document.createElement("div");
    item.className = "qb-chat__history-item";
    if (conversation.id === state.currentConversationId) item.classList.add("is-active");

    const main = document.createElement("button");
    main.type = "button";
    main.className = "qb-chat__conversation-main";
    main.setAttribute("aria-label", "打开对话 " + conversation.name);
    const title = document.createElement("span");
    title.className = "qb-chat__conversation-title";
    title.textContent = conversation.name || "未命名对话";
    const meta = document.createElement("span");
    meta.className = "qb-chat__conversation-meta";
    const agentLabel = (conversation.agent_icon || "A") + " " + (conversation.agent_name || "Agent");
    meta.textContent = agentLabel + " · " + String(conversation.message_total || 0) + " 条 · " + formatTime(conversation.updated_at);
    main.append(title, meta);
    main.addEventListener("click", function () {
      selectConversation(conversation.id).catch(function (error) {
        showToast("会话加载失败：" + error.message);
      });
    });

    const menu = document.createElement("button");
    menu.type = "button";
    menu.className = "qb-chat__conversation-menu";
    menu.setAttribute("aria-label", "管理 " + conversation.name);
    menu.textContent = "⋯";
    menu.addEventListener("click", function () {
      manageConversation(conversation);
    });

    item.append(main, menu);
    ui.conversationList.appendChild(item);
  });
}

async function loadConversations(reset) {
  if (reset) {
    state.conversationCursor = "";
    state.conversations = [];
  }
  const params = new URLSearchParams({ limit: "20" });
  const keyword = ui.search.value.trim();
  if (keyword) params.set("keyword", keyword);
  if (ui.agentFilter.value) params.set("agent_id", ui.agentFilter.value);
  if (state.conversationCursor) params.set("cursor", state.conversationCursor);
  try {
    const page = await requestJSON(API_ROOT + "?" + params.toString());
    state.conversations = reset ? page.items : state.conversations.concat(page.items);
    state.conversationCursor = page.next_cursor || "";
    ui.loadMoreConversations.hidden = !state.conversationCursor;
    renderConversationList();
    const current = state.conversations.find(function (item) { return item.id === state.currentConversationId; });
    if (current) {
      state.activeConversation = current;
      ui.title.textContent = current.name;
    }
    renderSessionProfile();
  } catch (error) {
    showToast("会话列表加载失败：" + error.message);
  }
}

async function selectConversation(id, expectedAgentID) {
  if (!id || id === state.currentConversationId && ui.messages.dataset.loaded === "true") {
    closeSidebar();
    return;
  }
  if (state.running) {
    showToast("请先停止当前回复");
    return;
  }
  const conversation = await requestJSON(API_ROOT + "/" + encodeURIComponent(id));
  assertConversationAgent(conversation, expectedAgentID || "");
  clearChatDraft(ui);
  resizeComposer();
  state.currentConversationId = conversation.id;
  state.activeConversation = conversation;
  state.selectedAgentID = conversation.agent_id;
  state.selectedAgent = agentFromConversation(conversation);
  state.readOnly = conversation.agent_status !== "enabled";
  updateSendAvailability();
  ui.title.textContent = conversation.name || "对话";
  window.history.replaceState(null, "", "/chat?conversation_id=" + encodeURIComponent(conversation.id));
  renderAgentFilter();
  renderSessionProfile();
  renderConversationList();
  closeSidebar();
  await loadMessages(true);
}

async function manageConversation(conversation) {
  const value = window.prompt("输入新的会话名称；输入 DELETE 可删除此会话", conversation.name);
  if (value === null) return;
  const name = value.trim();
  if (name === "DELETE") {
    if (!window.confirm("删除“" + conversation.name + "”及其全部聊天记录？此操作无法撤销。")) return;
    try {
      if (state.running && state.currentConversationId === conversation.id) await cancelCurrentRun();
      await requestJSON(API_ROOT + "/" + encodeURIComponent(conversation.id), { method: "DELETE" });
      state.conversations = state.conversations.filter(function (item) { return item.id !== conversation.id; });
      if (state.currentConversationId === conversation.id) {
        state.currentConversationId = "";
        state.activeConversation = null;
        state.messageCursor = "";
        ui.title.textContent = "新对话";
        window.history.replaceState(null, "", "/chat?agent_id=" + encodeURIComponent(state.selectedAgentID));
        renderSessionProfile();
        renderMessages([]);
      }
      renderConversationList();
      showToast("会话已删除");
    } catch (error) {
      showToast("删除失败：" + error.message);
    }
    return;
  }
  if (!name || name === conversation.name) return;
  try {
    await requestJSON(API_ROOT + "/" + encodeURIComponent(conversation.id), {
      method: "PATCH",
      headers: { "Accept": "application/json", "Content-Type": "application/json" },
      body: JSON.stringify({ name: name }),
    });
    conversation.name = name;
    if (state.activeConversation && state.activeConversation.id === conversation.id) state.activeConversation.name = name;
    if (state.currentConversationId === conversation.id) ui.title.textContent = name;
    renderConversationList();
    showToast("会话已重命名");
  } catch (error) {
    showToast("重命名失败：" + error.message);
  }
}

function renderMessages(items) {
  state.streamingMessage = null;
  ui.messages.replaceChildren();
  ui.messages.dataset.loaded = "true";
  const visibleItems = (items || []).filter(isVisibleChatMessage);
  if (visibleItems.length === 0) {
    ui.welcome.hidden = false;
    renderWelcome();
    ui.messages.appendChild(ui.welcome);
    return;
  }
  ui.welcome.hidden = true;
  const fragment = document.createDocumentFragment();
  visibleItems.forEach(function (message) {
    fragment.appendChild(createMessageElement(message));
  });
  ui.messages.appendChild(fragment);
  ui.messages.scrollTop = ui.messages.scrollHeight;
}

function appendMessage(message, provisional) {
  if (ui.welcome.parentNode === ui.messages) ui.welcome.remove();
  ui.welcome.hidden = true;
  const element = createMessageElement(message);
  if (provisional) element.dataset.provisional = "true";
  ui.messages.appendChild(element);
  ui.messages.scrollTop = ui.messages.scrollHeight;
}

const chatStream = createChatStream({ state, ui, createMessageElement, isVisibleChatMessage });

function createMessageElement(message) {
  const role = message.role || "assistant";
  const article = document.createElement("article");
  article.className = "qb-chat__message qb-chat__message--" + role;

  const inner = document.createElement("div");
  inner.className = "qb-chat__message-inner";
  const avatar = document.createElement("div");
  avatar.className = "qb-chat__message-avatar";
  avatar.setAttribute("aria-hidden", "true");
  avatar.textContent = role === "user" ? "YOU" : role === "tool" ? "TL" : "R";

  const content = document.createElement("div");
  content.className = "qb-chat__message-content";
  const head = document.createElement("div");
  head.className = "qb-chat__message-head";
  const roleLabel = document.createElement("strong");
  roleLabel.className = "qb-chat__message-role";
  roleLabel.textContent = role === "user" ? "你" : role === "tool" ? (message.tool_name || "工具") : "Reagent";
  const time = document.createElement("time");
  time.className = "qb-chat__message-time";
  time.textContent = formatTime(message.created_at);
  head.append(roleLabel, time);
  content.appendChild(head);

  const blocks = Array.isArray(message.content) ? message.content : [];
  if (role !== "tool") {
    blocks.forEach(function (block) {
      if (block.type === "image" && block.image && block.image.url) {
        content.appendChild(createImageBlock(block.image.url));
        return;
      }
      const text = document.createElement("div");
      text.className = "qb-chat__message-body";
      renderMessageContent(text, block.text || "");
      content.appendChild(text);
    });
  }

  const calls = Array.isArray(message.tool_calls) ? message.tool_calls : [];
  calls.forEach(function (call) {
    content.appendChild(createToolRecord("调用 " + (call.name || "工具"), call.arguments, false, "qb-chat__tool-call"));
  });
  if (role === "tool") {
    const resultText = blocks.map(function (block) { return block.text || ""; }).join("\n");
    content.appendChild(createToolRecord(
      (message.tool_name || "工具") + (message.tool_call_id ? " · " + message.tool_call_id : ""),
      resultText,
      Boolean(message.is_error),
      "qb-chat__tool-record"
    ));
  }
  if (blocks.length === 0 && calls.length === 0 && role !== "tool") {
    const empty = document.createElement("div");
    empty.className = "qb-chat__message-body";
    empty.textContent = "（空消息）";
    content.appendChild(empty);
  }
  inner.append(avatar, content);
  article.appendChild(inner);
  return article;
}

// 用户图片渲染（lazy / no-referrer / 加载失败占位）见 chat-image.js。

function createToolRecord(label, value, isError, className) {
  const record = document.createElement("div");
  record.className = className + (isError ? " is-error" : "");
  const head = document.createElement("div");
  head.className = "qb-chat__tool-head";
  head.textContent = label;
  const output = document.createElement("pre");
  output.textContent = prettyValue(value);
  record.append(head, output);
  return record;
}

function prettyValue(value) {
  if (value === undefined || value === null || value === "") return "暂无输出";
  if (typeof value === "string") {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch (_error) {
      return value;
    }
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch (_error) {
    return String(value);
  }
}

async function loadMessages(reset) {
  if (!state.currentConversationId) {
    renderMessages([]);
    return;
  }
  if (reset) state.messageCursor = "";
  const params = new URLSearchParams({ limit: "50" });
  if (state.messageCursor) params.set("cursor", state.messageCursor);
  try {
    const page = await requestJSON(
      API_ROOT + "/" + encodeURIComponent(state.currentConversationId) + "/messages?" + params.toString()
    );
    if (reset) {
      renderMessages(page.items || []);
    } else {
      const previousHeight = ui.messages.scrollHeight;
      const previousTop = ui.messages.scrollTop;
      const fragment = document.createDocumentFragment();
      (page.items || []).forEach(function (message) {
        if (isVisibleChatMessage(message)) fragment.appendChild(createMessageElement(message));
      });
      ui.messages.insertBefore(fragment, ui.messages.firstChild);
      ui.messages.scrollTop = ui.messages.scrollHeight - previousHeight + previousTop;
    }
    state.messageCursor = page.next_cursor || "";
    ui.loadOlderMessages.hidden = !state.messageCursor;
  } catch (error) {
    showToast("聊天记录加载失败：" + error.message);
  }
}

function resizeComposer() {
  ui.input.style.height = "auto";
  ui.input.style.height = Math.min(ui.input.scrollHeight, 180) + "px";
}

function setRunning(running) {
  state.running = running;
  ui.send.classList.toggle("is-running", running);
  ui.send.setAttribute("aria-label", running ? "停止回复" : "发送消息");
  ui.input.disabled = running;
  updateSendAvailability();
  if (!running) {
    state.runId = "";
    state.runAbort = null;
    ui.input.focus();
  }
}

function resetActivity() {
  state.activityItems.clear();
  ui.runStatus.replaceChildren();
  ui.runStatus.hidden = false;
  const title = document.createElement("div");
  title.className = "qb-chat__activity-title";
  const pulse = document.createElement("span");
  pulse.className = "activity-pulse";
  pulse.setAttribute("aria-hidden", "true");
  const label = document.createElement("span");
  label.textContent = "Agent activity";
  title.append(pulse, label);
  ui.runStatus.appendChild(title);
}

function addActivity(key, label, details) {
  let item = state.activityItems.get(key);
  if (!item) {
    item = document.createElement("div");
    item.className = "qb-chat__activity-item";
    const heading = document.createElement("strong");
    item.appendChild(heading);
    ui.runStatus.appendChild(item);
    state.activityItems.set(key, item);
  }
  item.querySelector("strong").textContent = label;
  let output = item.querySelector("pre");
  if (details !== undefined && details !== null && details !== "") {
    if (!output) {
      output = document.createElement("pre");
      item.appendChild(output);
    }
    output.textContent = prettyValue(details);
  }
  ui.runStatus.scrollTop = ui.runStatus.scrollHeight;
}

async function startRun(content, imageURLs, onAccepted) {
  chatStream.discard();
  resetActivity();
  setRunning(true);
  state.runAbort = new AbortController();
  let terminal = false;
  const payload = { content: content };
  if (imageURLs && imageURLs.length > 0) payload.image_urls = imageURLs;
  try {
    const response = await fetch(
      API_ROOT + "/" + encodeURIComponent(state.currentConversationId) + "/runs",
      {
        method: "POST",
        headers: { "Accept": "text/event-stream", "Content-Type": "application/json" },
        body: JSON.stringify(payload),
        signal: state.runAbort.signal,
      }
    );
    if (!response.ok || !response.body) {
      let message = "回复启动失败";
      try {
        const body = await response.json();
        if (body && body.msg) message = body.msg;
      } catch (_error) {
        // Keep the safe fallback.
      }
      throw new Error(message);
    }
    if (onAccepted) onAccepted();
    await readSSE(response.body, async function (eventName, data) {
      if (eventName === "run.started") {
        state.runId = data.run_id || "";
        addActivity("run", "正在分析你的请求");
      } else if (eventName === "agent.thinking") {
        addActivity("thinking", "模型正在思考");
      } else if (eventName === "tool.started") {
        const tool = data.tool || {};
        addActivity("tool:" + tool.id, "调用工具 · " + (tool.name || "unknown"), tool.arguments);
      } else if (eventName === "tool.updated") {
        const tool = data.tool || {};
        addActivity("tool:" + tool.id, "工具运行中 · " + (tool.name || "unknown"), tool.content || tool.details);
      } else if (eventName === "tool.completed") {
        const tool = data.tool || {};
        addActivity("tool:" + tool.id, (tool.is_error ? "工具失败 · " : "工具完成 · ") + (tool.name || "unknown"), tool.content || tool.details);
      } else if (eventName === "message.started") {
        chatStream.start();
      } else if (eventName === "message.delta") {
        chatStream.appendDelta(data.delta);
      } else if (eventName === "message.completed") {
        chatStream.complete(data.message);
        if (isVisibleChatMessage(data.message)) addActivity("message", "回复已生成，正在保存");
      } else if (eventName === "run.failed") {
        terminal = true;
        chatStream.discard();
        addActivity("terminal", "本轮未完成", data.error && data.error.message);
        showToast(data.error && data.error.message ? data.error.message : "本轮回复失败");
      } else if (eventName === "run.completed") {
        terminal = true;
        addActivity("terminal", "回复与工具记录已保存");
      }
    });
    if (!terminal) throw new Error("连接提前结束，请重新发送");
  } catch (error) {
    chatStream.discard();
    if (error.name !== "AbortError") showToast(error.message || "回复失败");
  } finally {
    setRunning(false);
    await loadMessages(true);
    await loadConversations(true);
    window.setTimeout(function () {
      if (!state.running) ui.runStatus.hidden = true;
    }, 1600);
  }
}

async function readSSE(stream, onEvent) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const part = await reader.read();
    buffer += decoder.decode(part.value || new Uint8Array(), { stream: !part.done });
    buffer = buffer.replace(/\r\n/g, "\n");
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const frame = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      const parsed = parseSSEFrame(frame);
      if (parsed) await onEvent(parsed.name, parsed.data);
      boundary = buffer.indexOf("\n\n");
    }
    if (part.done) break;
  }
}

function parseSSEFrame(frame) {
  let name = "message";
  const dataLines = [];
  frame.split("\n").forEach(function (line) {
    if (line.startsWith("event:")) name = line.slice(6).trim();
    if (line.startsWith("data:")) dataLines.push(line.slice(5).trimStart());
  });
  if (dataLines.length === 0) return null;
  try {
    return { name: name, data: JSON.parse(dataLines.join("\n")) };
  } catch (_error) {
    return null;
  }
}

async function cancelCurrentRun() {
  const abort = state.runAbort;
  try {
    if (state.currentConversationId && state.runId) {
      await requestJSON(
        API_ROOT + "/" + encodeURIComponent(state.currentConversationId) +
        "/runs/" + encodeURIComponent(state.runId) + "/cancel",
        { method: "POST" }
      );
    }
  } catch (error) {
    showToast("停止失败：" + error.message);
  } finally {
    if (abort) abort.abort();
  }
}

ui.composer.addEventListener("submit", async function (event) {
  event.preventDefault();
  if (state.running) {
    await cancelCurrentRun();
    return;
  }
  const content = ui.input.value;
  const imageURLs = [];
  if (ui.imageURL) {
    const raw = ui.imageURL.value.trim();
    if (raw) imageURLs.push(raw);
  }
  const sendState = {};
  Object.defineProperties(sendState, {
    running: { get: () => state.running, set: (value) => setRunning(value) },
    selectedAgentID: { get: () => state.selectedAgentID },
    currentConversationID: {
      get: () => state.currentConversationId,
      set: (value) => { state.currentConversationId = value; },
    },
    readOnly: { get: () => state.readOnly },
  });
  try {
    await sendSelectedAgentMessage(sendState, content, imageURLs, {
      requestJSON: async function (path, options) {
        const conversation = await requestJSON(path, options);
        assertConversationAgent(conversation, state.selectedAgentID);
        state.activeConversation = conversation;
        state.messageCursor = "";
        if (!state.conversations.some(function (item) { return item.id === conversation.id; })) {
          state.conversations.unshift(conversation);
        }
        ui.title.textContent = conversation.name || "新对话";
        renderConversationList();
        closeSidebar();
        return conversation;
      },
      replaceURL: function (path) {
        window.history.replaceState(null, "", path);
        renderConversationList();
      },
      startRun: function (id, text, images) {
        if (id !== state.currentConversationId) throw new Error("会话状态已变化");
        return startRun(text, images, function () {
          const userBlocks = [{ type: "text", text: text.trim() }];
          images.forEach(function (url) { userBlocks.push({ type: "image", image: { url: url } }); });
          appendMessage({ role: "user", content: userBlocks, created_at: new Date().toISOString() }, true);
          ui.input.value = "";
          if (ui.imageURL) ui.imageURL.value = "";
          resizeComposer();
        });
      },
      showError: function (message) { showToast("消息发送失败：" + message); },
    });
  } catch (error) {
    showToast(error.message || "消息发送失败");
  }
});

ui.input.addEventListener("input", resizeComposer);
ui.input.addEventListener("keydown", function (event) {
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
    event.preventDefault();
    ui.composer.requestSubmit();
  }
});
ui.newChat.addEventListener("click", async function () {
  if (state.running) {
    showToast("请先停止当前回复");
    return;
  }
  state.currentConversationId = "";
  state.activeConversation = null;
  state.messageCursor = "";
  clearChatDraft(ui);
  resizeComposer();
  ui.title.textContent = "新对话";
  window.history.replaceState(null, "", "/chat?agent_id=" + encodeURIComponent(state.selectedAgentID));
  renderSessionProfile();
  renderConversationList();
  renderMessages([]);
  closeSidebar();
  ui.input.focus();
});
ui.search.addEventListener("input", function () {
  window.clearTimeout(state.searchTimer);
  state.searchTimer = window.setTimeout(function () { loadConversations(true); }, 240);
});
ui.agentFilter.addEventListener("change", function () {
  loadConversations(true);
});
ui.loadMoreConversations.addEventListener("click", function () { loadConversations(false); });
ui.loadOlderMessages.addEventListener("click", function () { loadMessages(false); });
ui.sidebarOpen.addEventListener("click", function () { ui.body.classList.add("sidebar-open"); });
ui.sidebarClose.addEventListener("click", closeSidebar);
ui.sidebarScrim.addEventListener("click", closeSidebar);
document.addEventListener("keydown", function (event) {
  if (event.key === "Escape") closeSidebar();
});

async function initializeChat() {
  let target;
  try {
    target = resolveChatTarget(window.location.search);
  } catch (error) {
    showToast(error.message);
    window.location.replace("/agents");
    return;
  }
  if (target.kind === "directory") {
    window.location.replace("/agents");
    return;
  }
  try {
    if (target.kind === "new") {
      await loadNewAgent(target.agentID);
    } else {
      await selectConversation(target.conversationID, target.agentID);
    }
    await loadConversations(true);
  } catch (error) {
    showToast("聊天加载失败：" + error.message);
    ui.agentLoadError.hidden = false;
    updateSendAvailability();
  }
}

resizeComposer();
renderMessages([]);
initializeChat();
