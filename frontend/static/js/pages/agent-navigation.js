function malformedEncoding(value) {
  return /%(?![0-9a-fA-F]{2})/.test(String(value || ""));
}

export function resolveChatTarget(search) {
  if (malformedEncoding(search)) throw new Error("聊天目标参数格式错误");
  const query = new URLSearchParams(search);
  if (query.getAll("agent_id").length > 1 || query.getAll("conversation_id").length > 1) {
    throw new Error("聊天目标参数重复");
  }
  const agentID = query.get("agent_id") || "";
  const conversationID = query.get("conversation_id") || "";
  if (!agentID && !conversationID) return { kind: "directory" };
  return { kind: conversationID ? "history" : "new", agentID, conversationID };
}

export function assertConversationAgent(conversation, selectedAgentID) {
  if (!conversation || !conversation.agent_id || (selectedAgentID && conversation.agent_id !== selectedAgentID)) {
    throw new Error("历史会话与所选 Agent 不一致");
  }
}

export function createAgentCard(agent, documentOverride) {
  const doc = documentOverride || globalThis.document;
  const card = doc.createElement("article");
  card.className = "agent-card";

  const icon = doc.createElement("span");
  icon.className = "agent-card__icon";
  icon.setAttribute("aria-hidden", "true");
  icon.textContent = agent.icon || String(agent.name || "A").slice(0, 1) || "A";

  const copy = doc.createElement("div");
  copy.className = "agent-card__copy";
  const title = doc.createElement("h2");
  title.textContent = agent.name || "未命名 Agent";
  const description = doc.createElement("p");
  description.textContent = agent.description || "暂无用途说明";
  copy.append(title, description);

  const link = doc.createElement("a");
  link.className = "agent-card__action";
  link.textContent = "开始聊天";
  link.href = `/chat?agent_id=${encodeURIComponent(agent.id)}`;
  if (!agent.selectable) {
    link.removeAttribute("href");
    link.setAttribute("aria-disabled", "true");
    link.textContent = "暂不可用";
  }
  card.append(icon, copy, link);
  return card;
}

export async function sendSelectedAgentMessage(state, content, imageURLs, io) {
  if (state.running) return;
  if (!String(content || "").trim() && imageURLs.length === 0) return;
  if (!state.selectedAgentID) throw new Error("请先选择 Agent");
  if (state.readOnly) throw new Error("此会话只能查看历史");
  state.running = true;
  try {
    if (!state.currentConversationID) {
      const conversation = await io.requestJSON("/api/v1/conversations", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ agent_id: state.selectedAgentID }),
      });
      assertConversationAgent(conversation, state.selectedAgentID);
      state.currentConversationID = conversation.id;
      io.replaceURL(`/chat?conversation_id=${encodeURIComponent(conversation.id)}`);
    }
    await io.startRun(state.currentConversationID, content, imageURLs);
  } catch (error) {
    io.showError(error instanceof Error ? error.message : "消息发送失败");
  } finally {
    state.running = false;
  }
}

export async function recoverTrainingView(trainingID, io, signal) {
  const prefix = `/api/v1/training-sessions/${encodeURIComponent(trainingID)}`;
  while (!signal.aborted) {
    const session = await io.requestJSON(prefix, { signal });
    const [messages, diff] = await Promise.all([
      io.loadAuthorizedTrainingMessages(session.conversation_id, signal),
      io.requestJSON(`${prefix}/diff`, { signal }),
    ]);
    io.renderSnapshot(session, messages, diff);
    if (session.operation?.state !== "running") return session;
    await io.wait(1000, signal);
  }
  return undefined;
}

export async function publishTraining(session, summary, confirmed, io) {
  if (!confirmed) throw new Error("请先确认本版本未经自动质量评测");
  if (session.status !== "ready" || session.operation?.state === "running") {
    throw new Error("当前训练不能发布");
  }
  return io.requestJSON(`/api/v1/training-sessions/${encodeURIComponent(session.id)}/publish`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      request_id: io.newRequestID(),
      expected_row_version: session.row_version,
      change_summary: summary,
      confirm_manual_review: true,
    }),
  });
}
