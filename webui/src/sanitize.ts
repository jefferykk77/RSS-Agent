import DOMPurify from "dompurify";

const allowedTags = ["h1", "h2", "h3", "h4", "p", "br", "ul", "ol", "li", "blockquote", "pre", "code", "strong", "em", "a", "img", "figure", "figcaption", "table", "thead", "tbody", "tr", "th", "td", "hr"];

export function safeArticleHTML(content: string, fallback: string): string {
  const source = (content || fallback || "订阅源没有提供可显示的正文。").trim();
  if (!/<(?:h[1-6]|p|div|section|article|ul|ol|li|blockquote|pre|code|strong|em|a|img|figure|table|hr)\b/i.test(source)) {
    return source.split(/\n{2,}/).map((paragraph) => `<p>${escapeHTML(paragraph)}</p>`).join("");
  }
  const clean = DOMPurify.sanitize(source, {
    ALLOWED_TAGS: allowedTags,
    ALLOWED_ATTR: ["href", "src", "alt", "title", "width", "height"],
    ALLOW_DATA_ATTR: false,
  });
  const documentNode = new DOMParser().parseFromString(clean, "text/html");
  documentNode.querySelectorAll("a").forEach((link) => {
    link.target = "_blank";
    link.rel = "noreferrer noopener";
  });
  documentNode.querySelectorAll("img").forEach((image) => {
    image.loading = "lazy";
    image.referrerPolicy = "no-referrer";
  });
  return documentNode.body.innerHTML || `<p>${escapeHTML(fallback)}</p>`;
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[character] || character);
}
