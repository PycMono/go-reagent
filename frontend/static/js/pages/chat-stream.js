// createChatStream 管理一条流式 Assistant 消息的生命周期。
// message.delta 只是临时展示：只有 complete 的消息被接受为业务消息；
// 未 complete 时被 discard（例如 run.failed）必须移除，已 complete 的消息
// 在随后的 discard 中必须保留。
export function createChatStream({ state, ui, createMessageElement, isVisibleChatMessage }) {
  function start() {
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

  function appendDelta(delta) {
    if (!delta || delta.type !== "text" || !delta.text) return;
    if (!state.streamingMessage) start();
    state.streamingMessage.body.textContent += delta.text;
    ui.messages.scrollTop = ui.messages.scrollHeight;
  }

  function complete(message) {
    if (!isVisibleChatMessage(message)) {
      discard();
      return;
    }
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

  function discard() {
    if (!state.streamingMessage) return;
    state.streamingMessage.element.remove();
    state.streamingMessage = null;
  }

  return { start, appendDelta, complete, discard };
}
