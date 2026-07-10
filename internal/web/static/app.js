"use strict";

const state = {
  profile: "default",
  profiles: [],
  feeds: [],
  items: [],
  selectedID: "",
  search: "",
  collectionFilter: "all",
  view: "all",
  sourceFilter: "",
  loading: false,
};

const elements = {
  navPane: document.querySelector("#nav-pane"),
  navToggle: document.querySelector("#nav-toggle"),
  profileSelect: document.querySelector("#profile-select"),
  sourceList: document.querySelector("#source-list"),
  sourceHealth: document.querySelector("#source-health"),
  digestCount: document.querySelector("#digest-count"),
  digestMeta: document.querySelector("#digest-meta"),
  digestSearch: document.querySelector("#digest-search"),
  digestList: document.querySelector("#digest-list"),
  detailPane: document.querySelector("#detail-pane"),
  detailEmpty: document.querySelector("#detail-empty"),
  detailContent: document.querySelector("#detail-content"),
  runButton: document.querySelector("#run-button"),
  runStatus: document.querySelector("#run-status"),
  commandTrigger: document.querySelector("#command-trigger"),
  commandDialog: document.querySelector("#command-dialog"),
  commandSearch: document.querySelector("#command-search"),
  commandList: document.querySelector("#command-list"),
  toast: document.querySelector("#toast"),
};

let toastTimer;

async function request(url, options = {}) {
  const response = await fetch(url, {
    headers: { Accept: "application/json", ...options.headers },
    ...options,
  });
  const contentType = response.headers.get("content-type") || "";
  const payload = contentType.includes("application/json") ? await response.json() : {};
  if (!response.ok) {
    throw new Error(payload.error || `请求失败 (${response.status})`);
  }
  return payload;
}

async function loadWorkspace(profile = state.profile, selectedID = state.selectedID) {
  state.loading = true;
  elements.digestList.replaceChildren(loadingState());
  try {
    const query = new URLSearchParams({ profile });
    const [bootstrap, digest] = await Promise.all([
      request(`/api/bootstrap?${query}`),
      request(`/api/digest?${query}&limit=100`),
    ]);
    state.profile = bootstrap.profile;
    state.profiles = bootstrap.profiles;
    state.feeds = bootstrap.feeds;
    state.items = digest.items;
    state.selectedID = state.items.some((item) => item.id === selectedID) ? selectedID : state.items[0]?.id || "";
    renderWorkspace();
    elements.runStatus.textContent = "本地工作台";
  } catch (error) {
    elements.digestList.replaceChildren(emptyState(error.message));
    elements.runStatus.textContent = "连接失败";
    showToast(error.message, true);
  } finally {
    state.loading = false;
  }
}

function renderWorkspace() {
  renderProfiles();
  renderSources();
  renderNavigation();
  renderDigest();
  renderDetail();
  refreshIcons();
}

function renderProfiles() {
  elements.profileSelect.replaceChildren();
  for (const profile of state.profiles) {
    const option = document.createElement("option");
    option.value = profile;
    option.textContent = profile;
    option.selected = profile === state.profile;
    elements.profileSelect.append(option);
  }
}

function renderSources() {
  elements.sourceList.replaceChildren();
  const allSources = makeSourceButton("", "全部来源", "layers");
  allSources.querySelector(".source-item").classList.toggle("is-active", !state.sourceFilter);
  elements.sourceList.append(allSources);
  for (const feed of state.feeds) {
    const button = makeSourceButton(feed.url, feed.name, feed.disabled ? "circle-off" : "rss");
    const sourceButton = button.querySelector(".source-item");
    sourceButton.classList.toggle("is-active", feed.url === state.sourceFilter);
    sourceButton.disabled = feed.disabled;
    elements.sourceList.append(button);
  }
  const enabled = state.feeds.filter((feed) => !feed.disabled).length;
  elements.sourceHealth.replaceChildren(icon("radio"), document.createTextNode(` ${enabled} 个启用来源`));
}

function makeSourceButton(url, name, iconName) {
  const item = document.createElement("li");
  const button = document.createElement("button");
  button.type = "button";
  button.className = "source-item";
  button.dataset.source = url;
  button.append(icon(iconName));
  const label = document.createElement("span");
  label.className = "source-item__name";
  label.textContent = name;
  button.append(label);
  item.append(button);
  return item;
}

function renderNavigation() {
  const filtered = filteredItems();
  elements.digestCount.textContent = String(filtered.length);
  elements.digestMeta.textContent = state.items.length ? `${state.items.length} 条已保存条目` : "等待首次运行";
  for (const button of document.querySelectorAll(".nav-item[data-filter]")) {
    button.classList.toggle("is-active", button.dataset.filter === state.collectionFilter);
  }
  for (const button of document.querySelectorAll(".tab[data-view]")) {
    const active = button.dataset.view === state.view;
    button.classList.toggle("is-active", active);
    button.setAttribute("aria-selected", String(active));
  }
}

function renderDigest() {
  const items = filteredItems();
  elements.digestList.replaceChildren();
  if (!items.length) {
    elements.digestList.append(emptyState(state.items.length ? "没有匹配的条目" : "还没有已保存条目"));
    return;
  }
  for (const item of items) {
    elements.digestList.append(makeDigestRow(item));
  }
  refreshIcons(elements.digestList);
}

function makeDigestRow(item) {
  const row = document.createElement("button");
  row.type = "button";
  row.className = "digest-row";
  row.setAttribute("role", "option");
  row.dataset.itemID = item.id;
  row.setAttribute("aria-selected", String(item.id === state.selectedID));
  row.classList.toggle("is-selected", item.id === state.selectedID);

  const score = document.createElement("span");
  score.className = "score";
  score.textContent = item.score ? String(item.score) : "-";
  score.classList.toggle("is-recommended", item.should_push);
  row.append(score);

  const body = document.createElement("span");
  body.className = "digest-row__body";
  const topLine = document.createElement("span");
  topLine.className = "digest-row__topline";
  const title = document.createElement("strong");
  title.className = "digest-row__title";
  title.textContent = displayTitle(item);
  topLine.append(title);
  if (item.pushed) {
    const delivered = document.createElement("span");
    delivered.className = "row-state";
    delivered.title = "至少一个渠道已成功投递";
    delivered.append(icon("send"));
    topLine.append(delivered);
  }
  body.append(topLine);

  const summary = document.createElement("span");
  summary.className = "digest-row__summary";
  summary.textContent = firstText(item.summary, item.source_summary, item.why, "尚未生成摘要");
  body.append(summary);

  const meta = document.createElement("span");
  meta.className = "digest-row__meta";
  meta.append(sourceMark(item), textNode(item.feed_name), textNode(relativeTime(item.published_at)));
  body.append(meta);

  if (item.why) {
    const footer = document.createElement("span");
    footer.className = "digest-row__footer";
    const why = document.createElement("span");
    why.className = "why-line";
    why.textContent = item.why;
    footer.append(why);
    body.append(footer);
  }
  row.append(body);

  const actions = document.createElement("span");
  actions.className = "digest-row__actions";
  actions.append(
    feedbackIconButton(item, "save", "bookmark", "收藏"),
    feedbackIconButton(item, "later", "clock-3", "稍后阅读"),
    feedbackIconButton(item, "block", "ban", "屏蔽这篇"),
  );
  row.append(actions);
  return row;
}

function renderDetail() {
  const item = selectedItem();
  const hasItem = Boolean(item);
  elements.detailEmpty.hidden = hasItem;
  elements.detailContent.hidden = !hasItem;
  elements.detailPane.classList.toggle("is-open", hasItem);
  if (!item) {
    return;
  }

  const article = elements.detailContent;
  article.replaceChildren();
  const header = document.createElement("header");
  header.className = "detail-header";
  const topLine = document.createElement("div");
  topLine.className = "detail-topline";
  const source = document.createElement("span");
  source.className = "detail-source";
  source.append(sourceMark(item));
  const sourceName = document.createElement("span");
  sourceName.className = "detail-source__name";
  sourceName.textContent = item.feed_name;
  source.append(sourceName);
  topLine.append(source);
  if (window.matchMedia("(max-width: 680px)").matches) {
    const close = document.createElement("button");
    close.type = "button";
    close.className = "icon-button";
    close.setAttribute("aria-label", "返回 Digest");
    close.dataset.tooltip = "返回 Digest";
    close.append(icon("x"));
    topLine.append(close);
  }
  header.append(topLine);
  const title = document.createElement("h1");
  title.className = "detail-title";
  title.textContent = displayTitle(item);
  header.append(title);
  const meta = document.createElement("div");
  meta.className = "detail-meta";
  meta.append(textNode(relativeTime(item.published_at)));
  if (item.author) {
    meta.append(textNode(item.author));
  }
  const link = safeURL(item.link);
  if (link) {
    const openLink = document.createElement("a");
    openLink.className = "detail-link";
    openLink.href = link;
    openLink.target = "_blank";
    openLink.rel = "noreferrer";
    openLink.append(icon("external-link"), document.createTextNode("打开原文"));
    meta.append(openLink);
  }
  header.append(meta);
  article.append(header);

  const actions = document.createElement("section");
  actions.className = "detail-actions";
  const feedback = document.createElement("div");
  feedback.className = "feedback-actions";
  feedback.append(
    feedbackIconButton(item, "like", "thumbs-up", "喜欢"),
    feedbackIconButton(item, "dislike", "thumbs-down", "不喜欢"),
    feedbackIconButton(item, "save", "bookmark", "收藏"),
    feedbackIconButton(item, "later", "clock-3", "稍后阅读"),
    feedbackIconButton(item, "block", "ban", "屏蔽这篇"),
  );
  actions.append(feedback);
  const blockFeed = document.createElement("button");
  blockFeed.type = "button";
  blockFeed.className = "text-action";
  blockFeed.dataset.action = "block-feed";
  blockFeed.dataset.itemID = item.id;
  blockFeed.textContent = item.feedback.includes("block-feed") ? "已屏蔽来源" : "屏蔽来源";
  actions.append(blockFeed);
  article.append(actions);

  const explanation = section("为什么推送");
  const why = document.createElement("div");
  why.className = "why-block";
  const whyText = document.createElement("p");
  whyText.textContent = firstText(item.why, "该条目已保存，但还没有分析说明。");
  why.append(whyText);
  explanation.append(why);
  article.append(explanation);

  const summary = section("摘要");
  const summaryText = document.createElement("p");
  summaryText.className = "summary-text";
  summaryText.textContent = firstText(item.summary, item.source_summary, "原始订阅源没有提供摘要。");
  summary.append(summaryText);
  article.append(summary);

  if (item.key_points.length) {
    const points = section("关键要点");
    const list = document.createElement("ul");
    list.className = "keypoint-list";
    for (const point of item.key_points) {
      const entry = document.createElement("li");
      entry.textContent = point;
      list.append(entry);
    }
    points.append(list);
    article.append(points);
  }

  if (item.tags.length) {
    const tags = section("标签");
    tags.append(tagList(item.tags));
    article.append(tags);
  }

  const body = section("原文");
  const bodyText = document.createElement("div");
  bodyText.className = "article-body";
  const paragraphs = firstText(item.content, item.source_summary, "订阅源没有提供可显示的正文。").split(/\n{2,}/);
  for (const paragraph of paragraphs) {
    const content = paragraph.trim();
    if (!content) {
      continue;
    }
    const paragraphElement = document.createElement("p");
    paragraphElement.textContent = content;
    bodyText.append(paragraphElement);
  }
  body.append(bodyText);
  article.append(body);

  const metadata = section("分析信息");
  const grid = document.createElement("dl");
  grid.className = "metadata-grid";
  addMetadata(grid, "评分", item.score ? `${item.score}/10` : "未分析");
  addMetadata(grid, "建议推送", item.should_push ? "是" : "否");
  addMetadata(grid, "模型", firstText(item.model_label, item.model_name, "未分析"));
  addMetadata(grid, "分析时间", relativeTime(item.analyzed_at));
  addMetadata(grid, "投递", item.pushed ? "至少一个渠道成功" : "尚未投递");
  addMetadata(grid, "状态", item.seen ? "已处理" : "待处理");
  metadata.append(grid);
  article.append(metadata);
  refreshIcons(article);
}

function section(title) {
  const sectionElement = document.createElement("section");
  sectionElement.className = "detail-section";
  const heading = document.createElement("h2");
  heading.textContent = title;
  sectionElement.append(heading);
  return sectionElement;
}

function addMetadata(grid, label, value) {
  const wrapper = document.createElement("div");
  const key = document.createElement("dt");
  key.textContent = label;
  const detail = document.createElement("dd");
  detail.textContent = value;
  wrapper.append(key, detail);
  grid.append(wrapper);
}

function tagList(tags) {
  const list = document.createElement("div");
  list.className = "tag-list";
  for (const tag of tags) {
    const token = document.createElement("span");
    token.className = "tag";
    token.textContent = tag;
    list.append(token);
  }
  return list;
}

function feedbackIconButton(item, action, iconName, label) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "feedback-button";
  button.dataset.action = action;
  button.dataset.itemID = item.id;
  button.setAttribute("aria-label", label);
  button.dataset.tooltip = label;
  button.classList.toggle("is-active", item.feedback.includes(action));
  button.classList.toggle("is-danger", action === "block");
  button.append(icon(iconName));
  return button;
}

function filteredItems() {
  const search = state.search.trim().toLowerCase();
  return state.items.filter((item) => {
    if (state.collectionFilter === "saved" && !item.feedback.includes("save")) {
      return false;
    }
    if (state.collectionFilter === "later" && !item.feedback.includes("later")) {
      return false;
    }
    if (state.view === "recommended" && !item.should_push) {
      return false;
    }
    if (state.view === "delivered" && !item.pushed) {
      return false;
    }
    if (state.sourceFilter && item.feed_url !== state.sourceFilter) {
      return false;
    }
    if (!search) {
      return true;
    }
    return [item.title, item.analysis_title, item.feed_name, item.summary, item.why, ...item.tags]
      .filter(Boolean)
      .join(" ")
      .toLowerCase()
      .includes(search);
  });
}

function selectedItem() {
  return state.items.find((item) => item.id === state.selectedID);
}

function displayTitle(item) {
  return firstText(item.analysis_title, item.title, "未命名条目");
}

function firstText(...values) {
  return values.find((value) => typeof value === "string" && value.trim()) || "";
}

function textNode(value) {
  const text = document.createElement("span");
  text.textContent = value;
  return text;
}

function icon(name) {
  const element = document.createElement("i");
  element.dataset.lucide = name;
  element.setAttribute("aria-hidden", "true");
  return element;
}

function sourceMark(item) {
  const mark = document.createElement("span");
  mark.className = "source-mark";
  mark.textContent = firstText(item.feed_name, "?").slice(0, 1).toUpperCase();
  const faviconURL = faviconFor(item.feed_url);
  if (faviconURL) {
    const image = new Image();
    image.loading = "lazy";
    image.alt = "";
    image.src = faviconURL;
    image.addEventListener("load", () => mark.replaceChildren(image), { once: true });
  }
  return mark;
}

function faviconFor(rawURL) {
  try {
    const url = new URL(rawURL);
    if (url.protocol === "https:" || url.protocol === "http:") {
      return `${url.origin}/favicon.ico`;
    }
  } catch {
    return "";
  }
  return "";
}

function safeURL(rawURL) {
  try {
    const url = new URL(rawURL);
    return url.protocol === "https:" || url.protocol === "http:" ? url.href : "";
  } catch {
    return "";
  }
}

function relativeTime(rawTime) {
  if (!rawTime) {
    return "时间未知";
  }
  const date = new Date(rawTime);
  if (Number.isNaN(date.getTime())) {
    return "时间未知";
  }
  const seconds = Math.round((date.getTime() - Date.now()) / 1000);
  const formatter = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" });
  const intervals = [
    [60, "second"],
    [60, "minute"],
    [24, "hour"],
    [7, "day"],
    [4.34524, "week"],
    [12, "month"],
    [Infinity, "year"],
  ];
  let value = seconds;
  for (const [amount, unit] of intervals) {
    if (Math.abs(value) < amount) {
      return formatter.format(Math.round(value), unit);
    }
    value /= amount;
  }
  return date.toLocaleDateString("zh-CN");
}

function emptyState(message) {
  const empty = document.createElement("div");
  empty.className = "empty-list";
  const paragraph = document.createElement("p");
  paragraph.textContent = message;
  empty.append(paragraph);
  return empty;
}

function loadingState() {
  const loading = document.createElement("div");
  loading.className = "loading-state";
  loading.textContent = "正在加载 Digest";
  return loading;
}

function refreshIcons(root = document) {
  if (window.lucide) {
    window.lucide.createIcons({ root, attrs: { "stroke-width": 1.8 } });
  }
}

async function updateFeedback(itemID, action) {
  const item = state.items.find((entry) => entry.id === itemID);
  if (!item) {
    return;
  }
  const active = item.feedback.includes(action);
  try {
    if (active) {
      const query = new URLSearchParams({ profile: state.profile, item_id: itemID, action });
      await request(`/api/feedback?${query}`, { method: "DELETE" });
      showToast("已撤销反馈");
    } else {
      await request("/api/feedback", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ profile: state.profile, item_id: itemID, action }),
      });
      showToast(action === "block-feed" ? "已屏蔽这个来源" : "反馈已记录");
    }
    await loadWorkspace(state.profile, itemID);
  } catch (error) {
    showToast(error.message, true);
  }
}

async function runNow() {
  if (state.loading || !window.confirm(`立即运行 Profile “${state.profile}”？这会抓取订阅并调用已配置的模型。`)) {
    return;
  }
  elements.runButton.disabled = true;
  elements.runStatus.textContent = "正在运行";
  try {
    const query = new URLSearchParams({ profile: state.profile });
    const result = await request(`/api/run?${query}`, { method: "POST" });
    showToast(`完成：抓取 ${result.fetched}，分析 ${result.analyzed}，推送 ${result.pushed}`);
    await loadWorkspace(state.profile, state.selectedID);
  } catch (error) {
    showToast(error.message, true);
    elements.runStatus.textContent = "运行失败";
  } finally {
    elements.runButton.disabled = false;
  }
}

const commands = [
  { id: "run", label: "立即运行当前 Profile", shortcut: "R", icon: "play", run: () => runNow() },
  { id: "search", label: "聚焦 Digest 搜索", shortcut: "/", icon: "search", run: () => elements.digestSearch.focus() },
  { id: "all", label: "显示全部条目", shortcut: "", icon: "inbox", run: () => setFilters("all", "all") },
  { id: "saved", label: "显示已收藏条目", shortcut: "", icon: "bookmark", run: () => setFilters("saved", "all") },
  { id: "later", label: "显示稍后阅读", shortcut: "", icon: "clock-3", run: () => setFilters("later", "all") },
];

function openCommandPalette() {
  if (elements.commandDialog.open) {
    elements.commandSearch.focus();
    return;
  }
  elements.commandSearch.value = "";
  renderCommands();
  elements.commandDialog.showModal();
  elements.commandSearch.focus();
  refreshIcons(elements.commandDialog);
}

function renderCommands() {
  const query = elements.commandSearch.value.trim().toLowerCase();
  elements.commandList.replaceChildren();
  const matches = commands.filter((command) => command.label.toLowerCase().includes(query));
  for (const command of matches) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "command-item";
    button.dataset.command = command.id;
    button.append(icon(command.icon));
    const label = document.createElement("span");
    label.textContent = command.label;
    button.append(label);
    const shortcut = document.createElement("small");
    shortcut.textContent = command.shortcut;
    button.append(shortcut);
    elements.commandList.append(button);
  }
  if (!matches.length) {
    elements.commandList.append(emptyState("没有匹配命令"));
  }
  refreshIcons(elements.commandList);
}

function setFilters(collectionFilter, view) {
  state.collectionFilter = collectionFilter;
  state.view = view;
  state.sourceFilter = "";
  renderNavigation();
  renderDigest();
  if (elements.commandDialog.open) {
    elements.commandDialog.close();
  }
}

function moveSelection(delta) {
  const items = filteredItems();
  if (!items.length) {
    return;
  }
  const current = items.findIndex((item) => item.id === state.selectedID);
  const next = current < 0 ? 0 : (current + delta + items.length) % items.length;
  state.selectedID = items[next].id;
  renderDigest();
  renderDetail();
  document.querySelector(`[data-item-id="${cssEscape(state.selectedID)}"]`)?.scrollIntoView({ block: "nearest" });
}

function cssEscape(value) {
  return window.CSS?.escape ? window.CSS.escape(value) : value.replace(/[^a-zA-Z0-9_-]/g, "\\$&");
}

function showToast(message, isError = false) {
  window.clearTimeout(toastTimer);
  elements.toast.textContent = message;
  elements.toast.classList.toggle("is-error", isError);
  elements.toast.hidden = false;
  toastTimer = window.setTimeout(() => {
    elements.toast.hidden = true;
  }, 3600);
}

elements.profileSelect.addEventListener("change", () => {
  state.collectionFilter = "all";
  state.view = "all";
  state.sourceFilter = "";
  state.search = "";
  elements.digestSearch.value = "";
  loadWorkspace(elements.profileSelect.value, "");
});

elements.digestSearch.addEventListener("input", () => {
  state.search = elements.digestSearch.value;
  renderNavigation();
  renderDigest();
});

document.addEventListener("click", (event) => {
  const feedback = event.target.closest("[data-action][data-item-id]");
  if (feedback) {
    event.preventDefault();
    event.stopPropagation();
    updateFeedback(feedback.dataset.itemId, feedback.dataset.action);
    return;
  }
  const row = event.target.closest(".digest-row[data-item-id]");
  if (row) {
    state.selectedID = row.dataset.itemId;
    renderDigest();
    renderDetail();
    return;
  }
  const navItem = event.target.closest(".nav-item[data-filter]");
  if (navItem) {
    state.collectionFilter = navItem.dataset.filter;
    state.view = "all";
    state.sourceFilter = "";
    renderNavigation();
    renderDigest();
    return;
  }
  const tab = event.target.closest(".tab[data-view]");
  if (tab) {
    state.collectionFilter = "all";
    state.view = tab.dataset.view;
    state.sourceFilter = "";
    renderNavigation();
    renderDigest();
    return;
  }
  const source = event.target.closest(".source-item[data-source]");
  if (source) {
    state.collectionFilter = "all";
    state.view = "all";
    state.sourceFilter = source.dataset.source;
    renderNavigation();
    renderSources();
    renderDigest();
    return;
  }
  const command = event.target.closest("[data-command]");
  if (command) {
    const selected = commands.find((entry) => entry.id === command.dataset.command);
    if (selected) {
      elements.commandDialog.close();
      selected.run();
    }
    return;
  }
  if (event.target.closest(".detail-topline .icon-button")) {
    elements.detailPane.classList.remove("is-open");
  }
});

elements.runButton.addEventListener("click", runNow);
elements.commandTrigger.addEventListener("click", openCommandPalette);
elements.commandSearch.addEventListener("input", renderCommands);
elements.navToggle.addEventListener("click", () => elements.navPane.classList.toggle("is-open"));

document.addEventListener("keydown", (event) => {
  const target = event.target;
  const inInput = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement;
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    openCommandPalette();
    return;
  }
  if (elements.commandDialog.open || inInput || event.altKey || event.ctrlKey || event.metaKey) {
    return;
  }
  if (event.key === "/") {
    event.preventDefault();
    elements.digestSearch.focus();
  } else if (event.key === "ArrowDown") {
    event.preventDefault();
    moveSelection(1);
  } else if (event.key === "ArrowUp") {
    event.preventDefault();
    moveSelection(-1);
  } else if (event.key.toLowerCase() === "s" && selectedItem()) {
    updateFeedback(state.selectedID, "save");
  } else if (event.key.toLowerCase() === "l" && selectedItem()) {
    updateFeedback(state.selectedID, "later");
  } else if (event.key.toLowerCase() === "b" && selectedItem()) {
    updateFeedback(state.selectedID, "block");
  }
});

loadWorkspace();
