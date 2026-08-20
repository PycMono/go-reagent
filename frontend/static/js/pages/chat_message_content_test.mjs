import assert from "node:assert/strict";
import test from "node:test";

import { parseMessageContent, renderMessageContent } from "./chat-message-content.js";

class FakeElement {
  constructor(tagName) {
    this.tagName = tagName.toUpperCase();
    this.children = [];
    this.attributes = new Map();
    this.className = "";
    this.dataset = {};
    this.listeners = new Map();
    this.textContent = "";
    this.type = "";
  }

  append(...children) {
    this.children.push(...children);
  }

  replaceChildren(...children) {
    this.children = children;
  }

  setAttribute(name, value) {
    this.attributes.set(name, String(value));
  }

  addEventListener(name, listener) {
    this.listeners.set(name, listener);
  }
}

const fakeDocument = {
  createElement(tagName) {
    return new FakeElement(tagName);
  },
};

test("keeps ordinary message text unchanged", function () {
  assert.deepEqual(parseMessageContent("第一行\n第二行"), [
    { type: "text", text: "第一行\n第二行" },
  ]);
});

test("parses fenced code blocks and surrounding text", function () {
  assert.deepEqual(parseMessageContent("说明\n```text\n标题\n价格：[待填写]\n```\n结束"), [
    { type: "text", text: "说明" },
    { type: "code", language: "text", text: "标题\n价格：[待填写]" },
    { type: "text", text: "结束" },
  ]);
});

test("supports multiple blocks and normalizes language labels", function () {
  assert.deepEqual(parseMessageContent("``` Go \nfmt.Println(1)\n```\n```\nplain\n```"), [
    { type: "code", language: "go", text: "fmt.Println(1)" },
    { type: "code", language: "text", text: "plain" },
  ]);
});

test("leaves an unclosed fence visible as ordinary text", function () {
  const source = "前文\n```js\nalert(1)";
  assert.deepEqual(parseMessageContent(source), [{ type: "text", text: source }]);
});

test("renders code through textContent and creates an accessible copy control", async function () {
  const container = new FakeElement("div");
  const malicious = '<img src=x onerror="alert(1)">';
  let copied = "";
  let resetCopyState = null;
  renderMessageContent(container, "```html\n" + malicious + "\n```", {
    document: fakeDocument,
    copyText: async function (value) { copied = value; },
    setTimeout: function (callback) { resetCopyState = callback; },
  });

  assert.equal(container.children.length, 1);
  const block = container.children[0];
  assert.equal(block.tagName, "DIV");
  assert.equal(block.className, "qb-chat__code-block");
  assert.equal(block.dataset.language, "html");

  const header = block.children[0];
  const button = header.children[1];
  const code = block.children[1].children[0];
  assert.equal(button.tagName, "BUTTON");
  assert.equal(button.type, "button");
  assert.equal(button.attributes.get("aria-label"), "复制代码块");
  assert.equal(button.textContent, "复制");
  assert.equal(code.textContent, malicious);
  assert.equal(code.children.length, 0);

  await button.listeners.get("click")();
  assert.equal(copied, malicious);
  assert.equal(button.textContent, "已复制");
  resetCopyState();
  assert.equal(button.textContent, "复制");
});
