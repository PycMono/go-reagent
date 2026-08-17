const API_ROOT = "/api/v1/conversations";
const PROFILE_API = "/api/v1/agent-profiles";

const PROFILE_SYMBOLS = Object.freeze({
  "message-circle": "◎",
  "pen-line": "✎",
  "graduation-cap": "学",
  "heart-pulse": "+",
  "scale": "§",
  "car-front": "车",
  "briefcase": "公",
  "baby": "育",
});

const state = {
  conversations: [],
  profiles: [],
  defaultProfileCode: "",
  selectedProfileCode: "",
  profileFilter: "",
  profilesReady: false,
  profileLoadFailed: false,
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
  profileFilter: document.getElementById("profileFilter"),
  conversationList: document.getElementById("conversationList"),
  conversationCount: document.getElementById("conversationCount"),
  loadMoreConversations: document.getElementById("loadMoreConversations"),
  title: document.getElementById("sessionTitle"),
  sessionProfile: document.getElementById("sessionProfile"),
  messages: document.getElementById("chatMessages"),
  welcome: document.getElementById("chatWelcome"),
  welcomeProfileIcon: document.getElementById("welcomeProfileIcon"),
  profileWelcomeTitle: document.getElementById("profileWelcomeTitle"),
  profileWelcomeDescription: document.getElementById("profileWelcomeDescription"),
  profileLoadError: document.getElementById("profileLoadError"),
  profilePicker: document.getElementById("profilePicker"),
  profileStarters: document.getElementById("profileStarters"),
  loadOlderMessages: document.getElementById("loadOlderMessages"),
  runStatus: document.getElementById("runStatus"),
  composer: document.getElementById("chatComposer"),
  input: document.getElementById("chatInput"),
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

function findProfile(code) {
  return state.profiles.find(function (profile) { return profile.code === code; }) || null;
}

function profileSymbol(profile) {
  return profile && PROFILE_SYMBOLS[profile.icon] ? PROFILE_SYMBOLS[profile.icon] : "◇";
}

function currentConversation() {
  return state.conversations.find(function (item) { return item.id === state.currentConversationId; }) || state.activeConversation;
}

function updateSendAvailability() {
  ui.send.disabled = !state.running && !state.currentConversationId && !state.profilesReady;
}

function renderProfileFilter() {
  ui.profileFilter.replaceChildren();
  const all = document.createElement("option");
  all.value = "";
  all.textContent = "全部助手";
  ui.profileFilter.appendChild(all);
  state.profiles.forEach(function (profile) {
    const option = document.createElement("option");
    option.value = profile.code;
    option.textContent = profileSymbol(profile) + " " + profile.name;
    ui.profileFilter.appendChild(option);
  });
  ui.profileFilter.value = state.profileFilter;
  ui.profileFilter.disabled = !state.profilesReady;
}

function setSelectedProfile(code) {
  if (state.currentConversationId) return;
  const profile = findProfile(code);
  if (!profile || !profile.selectable) return;
  state.selectedProfileCode = profile.code;
  renderWelcome();
}

function renderProfilePicker() {
  ui.profilePicker.replaceChildren();
  if (state.currentConversationId || !state.profilesReady) {
    ui.profilePicker.hidden = true;
    return;
  }
  ui.profilePicker.hidden = false;
  state.profiles.filter(function (profile) { return profile.selectable; }).forEach(function (profile) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "qb-chat__profile-option";
    button.setAttribute("role", "option");
    const selected = profile.code === state.selectedProfileCode;
    button.setAttribute("aria-selected", selected ? "true" : "false");
    if (selected) button.classList.add("is-selected");

    const icon = document.createElement("span");
    icon.className = "qb-chat__profile-option-icon";
    icon.setAttribute("aria-hidden", "true");
    icon.textContent = profileSymbol(profile);
    const copy = document.createElement("span");
    copy.className = "qb-chat__profile-option-copy";
    const name = document.createElement("strong");
    name.textContent = profile.name;
    const description = document.createElement("small");
    description.textContent = profile.description;
    copy.append(name, description);
    button.append(icon, copy);
    button.addEventListener("click", function () { setSelectedProfile(profile.code); });
    ui.profilePicker.appendChild(button);
  });
}

function renderProfileStarters(profile) {
  ui.profileStarters.replaceChildren();
  if (state.currentConversationId || !profile || !Array.isArray(profile.starters) || profile.starters.length === 0) {
    ui.profileStarters.hidden = true;
    return;
  }
  ui.profileStarters.hidden = false;
  profile.starters.forEach(function (starter) {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = starter.title;
    button.addEventListener("click", function () {
      ui.input.value = starter.prompt || "";
      resizeComposer();
      ui.input.focus();
      ui.input.setSelectionRange(ui.input.value.length, ui.input.value.length);
    });
    ui.profileStarters.appendChild(button);
  });
}

function renderSessionProfile() {
  const conversation = currentConversation();
  if (!conversation) {
    ui.sessionProfile.hidden = true;
    ui.sessionProfile.textContent = "";
    return;
  }
  const profile = findProfile(conversation.profile_code);
  ui.sessionProfile.textContent = profile ? profileSymbol(profile) + " " + profile.name : conversation.profile_code;
  ui.sessionProfile.hidden = false;
}

function renderWelcome() {
  const conversation = currentConversation();
  const profileCode = conversation ? conversation.profile_code : state.selectedProfileCode;
  const profile = findProfile(profileCode);
  ui.profileLoadError.hidden = !state.profileLoadFailed || Boolean(conversation);
  if (profile) {
    ui.welcomeProfileIcon.textContent = profileSymbol(profile);
    ui.profileWelcomeTitle.textContent = profile.welcome;
    ui.profileWelcomeDescription.textContent = conversation
      ? "这个对话固定使用" + profile.name + "。"
      : profile.description;
  } else if (state.profileLoadFailed) {
    ui.welcomeProfileIcon.textContent = "!";
    ui.profileWelcomeTitle.textContent = conversation ? "继续当前对话" : "助手列表加载失败";
    ui.profileWelcomeDescription.textContent = conversation
      ? "仍可继续发送消息，助手身份由服务端会话记录决定。"
      : "刷新页面后再创建新对话。";
  } else {
    ui.welcomeProfileIcon.textContent = "R";
    ui.profileWelcomeTitle.textContent = "正在加载助手";
    ui.profileWelcomeDescription.textContent = "每个对话会固定使用创建时选择的助手。";
  }
  renderProfilePicker();
  renderProfileStarters(profile);
  updateSendAvailability();
}

async function loadProfiles() {
  try {
    const catalog = await requestJSON(PROFILE_API);
    const profiles = catalog && Array.isArray(catalog.items) ? catalog.items : [];
    const defaultProfile = profiles.find(function (profile) {
      return profile.code === catalog.default_profile && profile.selectable;
    });
    if (!defaultProfile) throw new Error("服务没有提供可用的默认助手");
    state.profiles = profiles;
    state.defaultProfileCode = defaultProfile.code;
    if (!findProfile(state.selectedProfileCode)) state.selectedProfileCode = defaultProfile.code;
    state.profilesReady = true;
    state.profileLoadFailed = false;
    renderProfileFilter();
    renderConversationList();
    renderSessionProfile();
    renderWelcome();
  } catch (error) {
    state.profiles = [];
    state.defaultProfileCode = "";
    state.selectedProfileCode = "";
    state.profilesReady = false;
    state.profileLoadFailed = true;
    renderProfileFilter();
    renderSessionProfile();
    renderWelcome();
    showToast("助手列表加载失败：" + error.message);
  }
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
    empty.textContent = ui.search.value.trim() || state.profileFilter ? "没有找到匹配的对话" : "发出第一条消息后，对话会出现在这里。";
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
    const profile = findProfile(conversation.profile_code);
    const profileLabel = profile ? profileSymbol(profile) + " " + profile.name : conversation.profile_code;
    meta.textContent = profileLabel + " · " + String(conversation.message_total || 0) + " 条 · " + formatTime(conversation.updated_at);
    main.append(title, meta);
    main.addEventListener("click", function () {
      selectConversation(conversation.id);
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
  if (state.profileFilter) params.set("profile_code", state.profileFilter);
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

async function createConversation() {
  const profile = findProfile(state.selectedProfileCode);
  if (!state.profilesReady || !profile || !profile.selectable) {
    throw new Error("请等待助手列表加载完成");
  }
  const conversation = await requestJSON(API_ROOT, {
  method: "POST",
  headers: { "Accept": "application/json", "Content-Type": "application/json" },
  body: JSON.stringify({ profile_code: profile.code }),
  });
  state.conversations.unshift(conversation);
  state.currentConversationId = conversation.id;
  state.activeConversation = conversation;
  state.messageCursor = "";
  ui.title.textContent = conversation.name;
  renderSessionProfile();
  renderConversationList();
  renderMessages([]);
  closeSidebar();
  return conversation;
}

async function selectConversation(id) {
  if (!id || id === state.currentConversationId && ui.messages.dataset.loaded === "true") {
    closeSidebar();
    return;
  }
  if (state.running) {
    showToast("请先停止当前回复");
    return;
  }
  state.currentConversationId = id;
  const conversation = state.conversations.find(function (item) { return item.id === id; });
  state.activeConversation = conversation || null;
  ui.title.textContent = conversation ? conversation.name : "对话";
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
    state.selectedProfileCode = state.defaultProfileCode;
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
  if (!items || items.length === 0) {
    ui.welcome.hidden = false;
  renderWelcome();
    ui.messages.appendChild(ui.welcome);
    return;
  }
  ui.welcome.hidden = true;
  const fragment = document.createDocumentFragment();
  items.forEach(function (message) {
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

function startStreamingMessage() {
  const element = createMessageElement({
    role: "assistant",
    content: [{ type: "text", text: "" }],
    created_at: new Date().toISOString(),
  });
  element.dataset.provisional = "true";
  if (ui.welcome.parentNode === ui.messages) ui.welcome.remove();
  ui.welcome.hidden = true;
  ui.messages.appendChild(element);
  state.streamingMessage = {
    element: element,
    body: element.querySelector(".qb-chat__message-body"),
  };
  ui.messages.scrollTop = ui.messages.scrollHeight;
}

function appendStreamingDelta(delta) {
  if (!delta || delta.type !== "text" || !delta.text) return;
  if (!state.streamingMessage) startStreamingMessage();
  state.streamingMessage.body.textContent += delta.text;
  ui.messages.scrollTop = ui.messages.scrollHeight;
}

function completeStreamingMessage(message) {
  if (!message) return;
  const completed = createMessageElement(message);
  completed.dataset.provisional = "true";
  if (state.streamingMessage) {
    state.streamingMessage.element.replaceWith(completed);
  } else {
    if (ui.welcome.parentNode === ui.messages) ui.welcome.remove();
    ui.welcome.hidden = true;
    ui.messages.appendChild(completed);
  }
  state.streamingMessage = null;
  ui.messages.scrollTop = ui.messages.scrollHeight;
}

function discardStreamingMessage() {
  if (!state.streamingMessage) return;
  state.streamingMessage.element.remove();
  state.streamingMessage = null;
}

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
      const text = document.createElement("div");
      text.className = "qb-chat__message-body";
      text.textContent = block.text || "";
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
        fragment.appendChild(createMessageElement(message));
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

async function startRun(content) {
  discardStreamingMessage();
  resetActivity();
  setRunning(true);
  state.runAbort = new AbortController();
  let terminal = false;
  try {
    const response = await fetch(
      API_ROOT + "/" + encodeURIComponent(state.currentConversationId) + "/runs",
      {
        method: "POST",
        headers: { "Accept": "text/event-stream", "Content-Type": "application/json" },
        body: JSON.stringify({ content: content }),
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
        startStreamingMessage();
      } else if (eventName === "message.delta") {
        appendStreamingDelta(data.delta);
      } else if (eventName === "message.completed") {
        completeStreamingMessage(data.message);
        addActivity("message", "回复已生成，正在保存");
      } else if (eventName === "run.failed") {
        terminal = true;
        discardStreamingMessage();
        addActivity("terminal", "本轮未完成", data.error && data.error.message);
        showToast(data.error && data.error.message ? data.error.message : "本轮回复失败");
      } else if (eventName === "run.completed") {
        terminal = true;
        addActivity("terminal", "回复与工具记录已保存");
      }
    });
    if (!terminal) throw new Error("连接提前结束，请重新发送");
  } catch (error) {
    discardStreamingMessage();
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
  const content = ui.input.value.trim();
  if (!content) return;
  try {
    if (!state.currentConversationId) await createConversation();
    appendMessage({
      role: "user",
      content: [{ type: "text", text: content }],
      created_at: new Date().toISOString(),
    }, true);
    ui.input.value = "";
    resizeComposer();
    await startRun(content);
  } catch (error) {
    showToast("消息发送失败：" + error.message);
    setRunning(false);
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
  state.selectedProfileCode = state.defaultProfileCode;
  ui.title.textContent = "新对话";
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
ui.profileFilter.addEventListener("change", function () {
  state.profileFilter = ui.profileFilter.value;
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

resizeComposer();
renderMessages([]);
Promise.allSettled([loadProfiles(), loadConversations(true)]);
