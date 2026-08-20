export function isVisibleChatMessage(message) {
  if (!message) return false;
  if (message.role !== "assistant") return true;
  const hasText = Array.isArray(message.content) && message.content.some(function (block) {
    return block && typeof block.text === "string" && block.text.length > 0;
  });
  const hasToolCalls = Array.isArray(message.tool_calls) && message.tool_calls.length > 0;
  return hasText || hasToolCalls;
}
