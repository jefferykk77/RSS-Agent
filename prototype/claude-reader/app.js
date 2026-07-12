"use strict";

const articles = [
  {
    id: "eval", score: 9, recommended: true, source: "OpenAI News", avatar: "O", age: "3 小时前",
    title: "编码评测的信号与噪声：SWE-Bench Pro 暴露了什么",
    dek: "一个看似基准测试的问题，实际会改变我们判断 Coding Agent 能力的方式。",
    rowSummary: "审计基准数据，比继续追逐排行榜上的一位小数更重要。",
    why: "这篇一手研究揭示了编码基准中的可靠性缺口。它不是又一条模型发布新闻，而是会直接影响 Codex 选型、评估和上线判断的方法论。",
    summary: "先审计评测数据，再相信排行榜；Agent 的真实能力不能只由单一通过率代表。",
    action: "为现有 Coding Agent 增加一组来自真实仓库、可复查失败原因的内部评测。",
    points: ["基准样本本身可能包含噪声和不可复现条件", "通过率无法解释 Agent 在哪里失败以及失败是否可修复", "内部评测应同时记录成功率、成本、耗时与人工接管次数"],
    tags: ["agent-evaluation", "codex", "applied-ai"],
    body: ["越来越多团队把公开编码基准当作模型采购和 Agent 上线的唯一证据。但当样本环境、依赖版本或验收条件并不稳定时，排行榜会把数据问题伪装成能力差异。", "更可靠的做法不是放弃基准，而是为每一个失败保留可审计上下文：仓库快照、工具轨迹、测试输出和最终补丁。这样团队才能分清模型能力不足、工具链失败和任务定义错误。", "对个人 Codex 工作流而言，这意味着先选十到二十个真实任务建立自己的小型回归集。每次调整模型、Prompt 或 Skill 后重跑，观察变化，而不是被单次演示说服。"]
  },
  {
    id: "context", score: 8, recommended: true, source: "Reddit · Claude Engineering", avatar: "R", age: "5 小时前",
    title: "Passation：让 Codex 与 Claude Code 在长任务中交接上下文",
    dek: "把决策、风险和下一步写成可执行的交接件，而不是把整个聊天记录塞给下一个 Agent。",
    rowSummary: "一套轻量交接格式，解决长任务中上下文越来越重的问题。",
    why: "这是来自真实开发流程的 context engineering 实践，给出了明确的交接结构和失败经验，方法可以直接迁移到 Codex。",
    summary: "高质量交接不是摘要聊天，而是压缩当前状态、未决决策、验证证据和下一步动作。",
    action: "在仓库中增加 HANDOFF.md 模板，并要求每个长任务结束前更新验证状态。",
    points: ["只保留影响下一步决策的上下文", "把已验证事实与推测明确分开", "记录失败命令和恢复入口，避免重复试错"],
    tags: ["context-engineering", "workflow", "codex"],
    body: ["长任务的主要损耗并不是模型忘记了所有内容，而是重要决策散落在大量过程文本里。下一位 Agent 能看到记录，却很难判断哪些已经验证、哪些只是尝试。", "Passation 的核心是把交接视为一个可测试的工程产物。它必须说明当前工作树状态、完成的验收、已知风险和唯一明确的下一步。", "这种格式尤其适合 Codex 的任务切换：用户不必重新解释背景，Agent 也不需要重读全部对话才能开始有效工作。"]
  },
  {
    id: "skill", score: 7, recommended: true, source: "GitHub · Skills & MCP", avatar: "G", age: "8 小时前",
    title: "Stitch Skills：让 Coding Agent 消费可见的界面规范",
    dek: "设计参考只有能变成 Token、组件约束和截图验收，才真正进入 Agent 的实现循环。",
    rowSummary: "Google Labs Code 发布可供 Agent 直接读取的界面 Skill。",
    why: "项目同时提供结构化规范与可见示例，符合‘用户能看到效果、Codex 能直接消费’的筛选标准。",
    summary: "好的前端 Skill 应连接设计意图、代码约束和截图验证，而不只是罗列审美形容词。",
    action: "选一个现有页面试用 Skill，比较生成前后的桌面截图与交互状态覆盖率。",
    points: ["设计 Token 可直接映射到 CSS 变量", "组件规则同时描述默认、焦点、禁用与错误状态", "截图验收把抽象审美变成可比较结果"],
    tags: ["skills", "frontend", "visual-validation"],
    body: ["多数设计提示词的问题，是它们只能描述风格，却无法约束实现。Agent 仍然要猜测间距、层级、交互状态和响应式行为。", "结构化 Skill 把这些决定变成可检索的规则，再配合真实截图形成反馈闭环。它不是替代设计判断，而是减少每次重新发明同一套基础规范。", "使用时仍应从产品任务出发。运营工作台、沉浸式阅读器和营销首页需要不同的信息密度，不能因为同一个 Skill 提供某种风格就把所有页面做成同一种样子。"]
  },
  {
    id: "release", score: 5, recommended: false, source: "Claude Code Changelog", avatar: "C", age: "昨天",
    title: "Claude Code 更新了会话恢复与工具权限提示",
    dek: "值得记录，但对当前 Codex 工作流没有立即可迁移的新方法。",
    rowSummary: "一次偏产品维护性质的更新，暂时放在持续观察。",
    why: "变化真实但技术实质有限，保留用于趋势观察，不进入本期推荐。",
    summary: "会话恢复更加稳定，权限提示更清楚，但尚未形成新的工程范式。",
    action: "无需立即操作，等待类似能力进入 Codex 或出现完整实践复盘。",
    points: ["恢复信息更明确", "权限范围展示更细", "缺少真实项目效果数据"],
    tags: ["claude-code", "changelog"],
    body: ["本次更新主要改善了中断后的会话恢复，并调整工具权限提示的可读性。对于正在使用 Claude Code 的团队，这是一次有用的体验修补。", "但从 Codex 用户角度看，它暂时没有带来可以立刻复用的工作流变化，因此更适合作为持续观察项，而不是必读内容。"]
  }
];

let selectedID = articles[0].id;
let activeView = "all";
const saved = new Set();

const list = document.querySelector("#article-list");
const count = document.querySelector("#article-count");
const search = document.querySelector("#search");

function visibleArticles() {
  const query = search.value.trim().toLowerCase();
  return articles.filter((article) => {
    if (activeView === "recommended" && !article.recommended) return false;
    if (activeView === "saved" && !saved.has(article.id)) return false;
    return !query || [article.title, article.source, article.tags.join(" ")].join(" ").toLowerCase().includes(query);
  });
}

function renderList() {
  const visible = visibleArticles();
  count.textContent = `${visible.length} 篇`;
  list.replaceChildren(...visible.map((article) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `article-row${article.id === selectedID ? " is-selected" : ""}`;
    button.dataset.articleId = article.id;
    button.setAttribute("role", "option");
    button.setAttribute("aria-selected", String(article.id === selectedID));
    button.innerHTML = `<span class="row-score">${article.score}</span><span class="row-copy"><strong class="row-title"></strong><span class="row-summary"></span><span class="row-meta"><span>${article.source}</span><span>·</span><span>${article.age}</span>${article.recommended ? '<span class="recommended">· 推荐</span>' : ""}</span></span>`;
    button.querySelector(".row-title").textContent = article.title;
    button.querySelector(".row-summary").textContent = article.rowSummary;
    return button;
  }));
}

function renderArticle() {
  const article = articles.find((entry) => entry.id === selectedID) || articles[0];
  document.querySelector("#source-avatar").textContent = article.avatar;
  document.querySelector("#source-name").textContent = article.source;
  document.querySelector("#article-age").textContent = article.age;
  document.querySelector("#article-title").textContent = article.title;
  document.querySelector("#article-dek").textContent = article.dek;
  document.querySelector("#article-why").textContent = article.why;
  document.querySelector("#article-summary").textContent = article.summary;
  document.querySelector("#article-action").textContent = article.action;
  document.querySelector("#score").textContent = article.score;
  document.querySelector("#key-points").replaceChildren(...article.points.map((text) => Object.assign(document.createElement("li"), { textContent: text })));
  document.querySelector("#tag-list").replaceChildren(...article.tags.map((text) => Object.assign(document.createElement("span"), { className: "tag", textContent: text })));
  document.querySelector("#article-content").replaceChildren(...article.body.map((text) => Object.assign(document.createElement("p"), { textContent: text })));
  for (const button of document.querySelectorAll("[data-feedback]")) {
    const active = button.dataset.feedback === "save" && saved.has(article.id);
    button.classList.toggle("is-active", active);
    button.setAttribute("aria-pressed", String(active));
  }
}

function openDrawer() {
  document.querySelector("#library-drawer").classList.add("is-open");
  document.querySelector("#library-drawer").setAttribute("aria-hidden", "false");
  document.querySelector("#drawer-scrim").hidden = false;
  document.querySelector("#drawer-close").focus();
}

function closeDrawer() {
  document.querySelector("#library-drawer").classList.remove("is-open");
  document.querySelector("#library-drawer").setAttribute("aria-hidden", "true");
  document.querySelector("#drawer-scrim").hidden = true;
  document.querySelector("#drawer-open").focus();
}

list.addEventListener("click", (event) => {
  const row = event.target.closest("[data-article-id]");
  if (!row) return;
  selectedID = row.dataset.articleId;
  renderList();
  renderArticle();
  document.querySelector("#reader").scrollTop = 0;
});

search.addEventListener("input", renderList);
document.querySelectorAll("[data-view]").forEach((button) => button.addEventListener("click", () => {
  activeView = button.dataset.view;
  document.querySelectorAll("[data-view]").forEach((tab) => {
    const active = tab === button;
    tab.classList.toggle("is-active", active);
    tab.setAttribute("aria-selected", String(active));
  });
  renderList();
}));

document.querySelectorAll("[data-feedback]").forEach((button) => button.addEventListener("click", () => {
  if (button.dataset.feedback === "save") {
    saved.has(selectedID) ? saved.delete(selectedID) : saved.add(selectedID);
  }
  button.classList.toggle("is-active");
  button.setAttribute("aria-pressed", String(button.classList.contains("is-active")));
}));

document.querySelector("#drawer-open").addEventListener("click", openDrawer);
document.querySelector("#drawer-close").addEventListener("click", closeDrawer);
document.querySelector("#drawer-scrim").addEventListener("click", closeDrawer);
document.querySelector("#feed-button").addEventListener("click", () => document.querySelector("#feed-dialog").showModal());
document.querySelector("#run-button").addEventListener("click", async (event) => {
  const button = event.currentTarget;
  const state = document.querySelector("#run-state");
  button.disabled = true;
  state.querySelector(".status-dot").classList.add("is-running");
  for (const message of ["正在拉取 12 个来源", "正在分析 3 / 14", "正在分析 9 / 14", "分析完成 · 14 条"]) {
    state.lastElementChild.textContent = message;
    await new Promise((resolve) => setTimeout(resolve, 650));
  }
  state.querySelector(".status-dot").classList.remove("is-running");
  button.disabled = false;
});

document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && document.querySelector("#library-drawer").classList.contains("is-open")) closeDrawer();
  if (event.key === "/" && document.activeElement !== search) { event.preventDefault(); search.focus(); }
});

renderList();
renderArticle();
window.addEventListener("load", () => window.lucide?.createIcons());
