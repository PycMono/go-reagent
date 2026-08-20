import assert from "node:assert/strict";
import test from "node:test";

import { isVisibleChatMessage } from "./chat-visibility.js";
import { createChatStream } from "./chat-stream.js";

class FakeElement {
  constructor(tagName) {
    this.tagName = tagName.toUpperCase();
    this.children = [];
    this.dataset = {};
    this.textContent = "";
    this.hidden = false;
    this.parentNode = null;
    this.scrollTop = 0;
    this.scrollHeight = 0;
    this.bodyElement = null;
  }

  appendChild(child) {
    child.parentNode = this;
    this.children.push(child);
  }

  remove() {
    if (!this.parentNode) return;
    const index = this.parentNode.children.indexOf(this);
    if (index >= 0) this.parentNode.children.splice(index, 1);
    this.parentNode = null;
  }

  replaceWith(other) {
    if (!this.parentNode) return;
    const index = this.parentNode.children.indexOf(this);
    if (index >= 0) {
      this.parentNode.children[index] = other;
      other.parentNode = this.parentNode;
    }
    this.parentNode = null;
  }

  querySelector() {
    return this.bodyElement;
  }
}

function createMessageElement(message) {
  const element = new FakeElement("article");
  element.message = message;
  element.bodyElement = new FakeElement("div");
  return element;
}

function createFixture() {
  const state = { streamingMessage: null };
  const ui = {
    messages: new FakeElement("div"),
    welcome: new FakeElement("div"),
  };
  const stream = createChatStream({
    state,
    ui,
    createMessageElement,
    isVisibleChatMessage,
  });
  return { state, ui, stream };
}

test("run.failed before completion discards the provisional assistant message", function () {
  const { state, ui, stream } = createFixture();
  stream.start();
  stream.appendDelta({ type: "text", text: "partial" });
  assert.equal(ui.messages.children.length, 1);
  assert.equal(state.streamingMessage.body.textContent, "partial");

  stream.discard();
  assert.equal(ui.messages.children.length, 0);
  assert.equal(state.streamingMessage, null);
});

test("run.failed after completion keeps the completed assistant message", function () {
  const { state, ui, stream } = createFixture();
  stream.start();
  stream.appendDelta({ type: "text", text: "partial" });
  stream.complete({ role: "assistant", content: [{ type: "text", text: "done" }] });
  assert.equal(state.streamingMessage, null);
  assert.equal(ui.messages.children.length, 1);
  assert.equal(ui.messages.children[0].message.content[0].text, "done");

  stream.discard();
  assert.equal(ui.messages.children.length, 1, "completed message must survive discard");
});

test("a delta without an explicit start opens the provisional message", function () {
  const { state, ui, stream } = createFixture();
  stream.appendDelta({ type: "text", text: "hello" });
  assert.ok(state.streamingMessage);
  assert.equal(ui.messages.children.length, 1);
  assert.equal(state.streamingMessage.body.textContent, "hello");
});

test("discard without a streaming message is a no-op", function () {
  const { stream } = createFixture();
  stream.discard();
});

test("completion with an invisible message drops the provisional message", function () {
  const { state, ui, stream } = createFixture();
  stream.start();
  stream.appendDelta({ type: "text", text: "partial" });
  stream.complete({ role: "assistant", content: [] });
  assert.equal(state.streamingMessage, null);
  assert.equal(ui.messages.children.length, 0);
});

test("non-text deltas are ignored", function () {
  const { state, ui, stream } = createFixture();
  stream.appendDelta({ type: "image", text: "ignored" });
  stream.appendDelta(null);
  assert.equal(state.streamingMessage, null);
  assert.equal(ui.messages.children.length, 0);
});
