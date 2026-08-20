import assert from "node:assert/strict";
import test from "node:test";

import { isVisibleChatMessage } from "./chat-visibility.js";

test("hides empty assistant messages returned after server-side filtering", function () {
  assert.equal(isVisibleChatMessage({ role: "assistant", content: [], tool_calls: [] }), false);
  assert.equal(isVisibleChatMessage({
    role: "assistant",
    content: [{ type: "text", text: "" }],
    tool_calls: [],
  }), false);
});

test("keeps assistant text and visible tool calls", function () {
  assert.equal(isVisibleChatMessage({
    role: "assistant",
    content: [{ type: "text", text: "继续处理" }],
    tool_calls: [],
  }), true);
  assert.equal(isVisibleChatMessage({
    role: "assistant",
    content: [],
    tool_calls: [{ id: "call-1", name: "read", arguments: { path: "README.md" } }],
  }), true);
});

test("keeps user and non-read tool messages", function () {
  assert.equal(isVisibleChatMessage({
    role: "user",
    content: [{ type: "text", text: "hello" }],
  }), true);
  assert.equal(isVisibleChatMessage({
    role: "tool",
    tool_name: "get_weather",
    content: [{ type: "text", text: "sunny" }],
  }), true);
});
