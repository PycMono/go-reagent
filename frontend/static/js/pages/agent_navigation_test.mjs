import assert from "node:assert/strict";
import test from "node:test";

import {
  assertConversationAgent,
  createAgentCard,
  publishTraining,
  recoverTrainingView,
  resolveChatTarget,
  sendSelectedAgentMessage,
} from "./agent-navigation.js";

class FakeElement {
  constructor(tagName) {
    this.tagName = tagName;
    this.children = [];
    this.attributes = new Map();
    this.className = "";
    this.textContent = "";
    this.href = "";
  }

  append(...children) { this.children.push(...children); }
  setAttribute(name, value) { this.attributes.set(name, value); }
  removeAttribute(name) { this.attributes.delete(name); this[name] = ""; }
}

const fakeDocument = { createElement: (tagName) => new FakeElement(tagName) };

test("chat target requires one well-formed explicit target", function () {
  assert.deepEqual(resolveChatTarget(""), { kind: "directory" });
  assert.deepEqual(resolveChatTarget("?agent_id=a%2F1"), { kind: "new", agentID: "a/1", conversationID: "" });
  assert.deepEqual(resolveChatTarget("?conversation_id=c1"), { kind: "history", agentID: "", conversationID: "c1" });
  assert.throws(() => resolveChatTarget("?agent_id=a&agent_id=b"), /重复/);
  assert.throws(() => resolveChatTarget("?agent_id=%ZZ"), /参数/);
});

test("conversation must remain bound to the selected Agent", function () {
  assert.doesNotThrow(() => assertConversationAgent({ agent_id: "a1" }, "a1"));
  assert.throws(() => assertConversationAgent({ agent_id: "a2" }, "a1"), /不一致/);
  assert.throws(() => assertConversationAgent({}, ""), /不一致/);
});

test("agent card uses text content and a stable encoded ID without creating data", function () {
  const card = createAgentCard({
    id: "agent/一", name: "<b>分析助手</b>", description: "查资料 & 总结", icon: "研", selectable: true,
  }, fakeDocument);
  assert.equal(card.children[1].children[0].textContent, "<b>分析助手</b>");
  assert.equal(card.children[1].children[1].textContent, "查资料 & 总结");
  assert.equal(card.children[2].href, "/chat?agent_id=agent%2F%E4%B8%80");

  const disabled = createAgentCard({ id: "a2", name: "暂不可用", description: "", selectable: false }, fakeDocument);
  assert.equal(disabled.children[2].attributes.get("aria-disabled"), "true");
  assert.equal(disabled.children[2].href, "");
});

test("concurrent first send creates one conversation and reuses it", async function () {
  const state = { running: false, selectedAgentID: "a1", currentConversationID: "", readOnly: false };
  let releaseCreate;
  let creates = 0;
  const runs = [];
  const io = {
    requestJSON: async (_path, options) => {
      creates += 1;
      assert.deepEqual(JSON.parse(options.body), { agent_id: "a1" });
      await new Promise((resolve) => { releaseCreate = resolve; });
      return { id: "c1", agent_id: "a1" };
    },
    replaceURL: () => {},
    startRun: async (...args) => { runs.push(args); },
    showError: () => assert.fail("unexpected error"),
  };
  const first = sendSelectedAgentMessage(state, "first", [], io);
  const second = sendSelectedAgentMessage(state, "second", [], io);
  assert.equal(creates, 1);
  releaseCreate();
  await Promise.all([first, second]);
  assert.deepEqual(runs, [["c1", "first", []]]);
  await sendSelectedAgentMessage(state, "third", [], io);
  assert.equal(creates, 1);
  assert.deepEqual(runs[1], ["c1", "third", []]);
});

test("failed first send preserves draft state and does not retry POST", async function () {
  const state = {
    running: false, selectedAgentID: "a1", currentConversationID: "", readOnly: false,
    draft: "keep me", imageURLs: ["https://example.test/image.png"],
  };
  let creates = 0;
  let shown = "";
  const io = {
    requestJSON: async () => { creates += 1; throw new Error("创建失败"); },
    replaceURL: () => assert.fail("URL must not change"),
    startRun: () => assert.fail("run must not start"),
    showError: (message) => { shown = message; },
  };
  await sendSelectedAgentMessage(state, state.draft, state.imageURLs, io);
  assert.equal(creates, 1);
  assert.equal(shown, "创建失败");
  assert.equal(state.currentConversationID, "");
  assert.equal(state.draft, "keep me");
  assert.deepEqual(state.imageURLs, ["https://example.test/image.png"]);
  assert.equal(state.running, false);
});

test("training reconnect fetches a snapshot and never posts", async function () {
  const requests = [];
  const renders = [];
  const signal = new AbortController().signal;
  const io = {
    requestJSON: async (path, options = {}) => {
      requests.push([path, options.method || "GET"]);
      return path.endsWith("/diff")
        ? { text: "diff" }
        : { id: "t1", conversation_id: "c1", operation: { state: "completed" } };
    },
    loadAuthorizedTrainingMessages: async (id) => [{ id }],
    renderSnapshot: (...args) => renders.push(args),
    wait: async () => assert.fail("completed operation must not poll"),
  };
  const session = await recoverTrainingView("t1", io, signal);
  assert.equal(session.id, "t1");
  assert.equal(renders.length, 1);
  assert.ok(requests.every(([, method]) => method === "GET"));
});

test("publishing requires ready state and explicit manual confirmation", async function () {
  const session = { id: "t/1", status: "ready", row_version: 4, operation: { state: "completed" } };
  const io = {
    newRequestID: () => "request-1",
    requestJSON: async (path, options) => ({ path, body: JSON.parse(options.body) }),
  };
  await assert.rejects(publishTraining(session, "summary", false, io), /确认/);
  const result = await publishTraining(session, "summary", true, io);
  assert.equal(result.path, "/api/v1/training-sessions/t%2F1/publish");
  assert.deepEqual(result.body, {
    request_id: "request-1", expected_row_version: 4, change_summary: "summary", confirm_manual_review: true,
  });
});
