import assert from "node:assert/strict";
import test from "node:test";

import { createImageBlock } from "./chat-image.js";

class FakeElement {
  constructor(tagName) {
    this.tagName = tagName.toUpperCase();
    this.children = [];
    this.attributes = new Map();
    this.className = "";
    this.dataset = {};
    this.listeners = new Map();
    this.textContent = "";
    this.hidden = false;
  }

  append(...children) {
    this.children.push(...children);
  }

  addEventListener(name, listener) {
    this.listeners.set(name, listener);
  }

  dispatch(name) {
    this.listeners.get(name)?.();
  }
}

const fakeDocument = {
  createElement(tagName) {
    return new FakeElement(tagName);
  },
};

function renderImage(url) {
  const wrapper = createImageBlock(url, fakeDocument);
  assert.equal(wrapper.className, "qb-chat__message-image");
  assert.equal(wrapper.children.length, 2);
  return { wrapper, image: wrapper.children[0], fallback: wrapper.children[1] };
}

test("image renders lazily without referrer", () => {
  const { image } = renderImage("https://example.com/a.png?sig=secret");
  assert.equal(image.tagName, "IMG");
  assert.equal(image.loading, "lazy");
  assert.equal(image.referrerPolicy, "no-referrer");
  assert.equal(image.alt, "用户图片");
  assert.equal(image.src, "https://example.com/a.png?sig=secret");
});

test("image failure falls back to placeholder", () => {
  const { image, fallback } = renderImage("https://example.com/broken.png");
  assert.equal(fallback.hidden, true);
  assert.equal(image.hidden, false);

  image.dispatch("error");

  assert.equal(image.hidden, true);
  assert.equal(fallback.hidden, false);
  assert.equal(fallback.textContent, "图片无法加载");
});