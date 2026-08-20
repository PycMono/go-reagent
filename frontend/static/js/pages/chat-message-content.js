function normalizeLanguage(info) {
  const value = String(info || "").trim().split(/\s+/, 1)[0].toLowerCase();
  if (!value) return "text";
  return value.replace(/[^a-z0-9_+.#-]/g, "").slice(0, 32) || "text";
}

function openingFence(line) {
  const match = /^[ \t]{0,3}(`{3,})([^`]*)$/.exec(line);
  if (!match) return null;
  return { width: match[1].length, language: normalizeLanguage(match[2]) };
}

function closingFence(line, minimumWidth) {
  const match = /^[ \t]{0,3}(`{3,})[ \t]*$/.exec(line);
  return Boolean(match && match[1].length >= minimumWidth);
}

export function parseMessageContent(value) {
  const source = String(value || "").replace(/\r\n?/g, "\n");
  if (!source) return [];

  const lines = source.split("\n");
  const tokens = [];
  let plainLines = [];

  function flushPlainText() {
    if (plainLines.length === 0) return;
    const text = plainLines.join("\n");
    if (text) tokens.push({ type: "text", text: text });
    plainLines = [];
  }

  for (let index = 0; index < lines.length;) {
    const fence = openingFence(lines[index]);
    if (!fence) {
      plainLines.push(lines[index]);
      index += 1;
      continue;
    }

    let closingIndex = -1;
    for (let candidate = index + 1; candidate < lines.length; candidate += 1) {
      if (closingFence(lines[candidate], fence.width)) {
        closingIndex = candidate;
        break;
      }
    }
    if (closingIndex < 0) {
      plainLines.push(...lines.slice(index));
      break;
    }

    flushPlainText();
    tokens.push({
      type: "code",
      language: fence.language,
      text: lines.slice(index + 1, closingIndex).join("\n"),
    });
    index = closingIndex + 1;
  }

  flushPlainText();
  return tokens;
}

function fallbackCopyText(value, doc) {
  const textarea = doc.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  doc.body.appendChild(textarea);
  textarea.select();
  const copied = doc.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("copy command failed");
}

function browserCopyText(value, doc) {
  if (globalThis.navigator && globalThis.navigator.clipboard && globalThis.navigator.clipboard.writeText) {
    return globalThis.navigator.clipboard.writeText(value);
  }
  return Promise.resolve(fallbackCopyText(value, doc));
}

function createCodeBlock(token, options) {
  const doc = options.document;
  const block = doc.createElement("div");
  block.className = "qb-chat__code-block";
  block.dataset.language = token.language;

  const header = doc.createElement("div");
  header.className = "qb-chat__code-header";
  const language = doc.createElement("span");
  language.className = "qb-chat__code-language";
  language.textContent = token.language;
  const copy = doc.createElement("button");
  copy.className = "qb-chat__code-copy";
  copy.type = "button";
  copy.setAttribute("aria-label", "复制代码块");
  copy.setAttribute("title", "复制代码块");
  copy.textContent = "复制";
  copy.addEventListener("click", async function () {
    if (copy.dataset.copying === "true") return;
    copy.dataset.copying = "true";
    copy.disabled = true;
    try {
      await options.copyText(token.text);
      copy.textContent = "已复制";
    } catch (_error) {
      copy.textContent = "复制失败";
    }
    options.setTimeout(function () {
      copy.textContent = "复制";
      copy.disabled = false;
      delete copy.dataset.copying;
    }, 1600);
  });
  header.append(language, copy);

  const pre = doc.createElement("pre");
  pre.setAttribute("tabindex", "0");
  const code = doc.createElement("code");
  code.textContent = token.text;
  pre.append(code);
  block.append(header, pre);
  return block;
}

export function renderMessageContent(container, value, settings) {
  const options = settings || {};
  const doc = options.document || container.ownerDocument || globalThis.document;
  const runtimeOptions = {
    document: doc,
    copyText: options.copyText || function (text) { return browserCopyText(text, doc); },
    setTimeout: options.setTimeout || globalThis.setTimeout.bind(globalThis),
  };

  const nodes = parseMessageContent(value).map(function (token) {
    if (token.type === "code") return createCodeBlock(token, runtimeOptions);
    const text = doc.createElement("div");
    text.className = "qb-chat__message-text";
    text.textContent = token.text;
    return text;
  });
  container.replaceChildren(...nodes);
}
