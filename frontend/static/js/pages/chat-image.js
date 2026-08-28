// createImageBlock 渲染用户图片：no-referrer 避免把当前页面地址通过
// Referer 发给图片服务；加载失败回退为占位框。
export function createImageBlock(url, doc) {
  const document = doc || globalThis.document;
  const wrapper = document.createElement("div");
  wrapper.className = "qb-chat__message-image";

  const image = document.createElement("img");
  image.loading = "lazy";
  image.referrerPolicy = "no-referrer";
  image.alt = "用户图片";
  image.src = url;

  const fallback = document.createElement("div");
  fallback.className = "qb-chat__image-fallback";
  fallback.textContent = "图片无法加载";
  fallback.hidden = true;

  image.addEventListener("error", function () {
    image.hidden = true;
    fallback.hidden = false;
  });

  wrapper.append(image, fallback);
  return wrapper;
}