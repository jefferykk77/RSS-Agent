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
	 topic: "",
  sourceStates: [],
	 editions: [],
	 edition: "",
  loading: false,
  loadingMore: false,
  nextCursor: "",
  total: 0,
  run: { status: "idle", analyzed: 0, pending: 0, running: 0, retrying: 0, rate_limited: 0, failed: 0 },
  pollingActive: true,
  lastBackgroundPoll: 0,
  operationStatus: "",
  operationUntil: 0,
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
	 topicSelect: document.querySelector("#topic-select"),
	 ingestTrigger: document.querySelector("#ingest-trigger"),
	 ingestDialog: document.querySelector("#ingest-dialog"),
  ingestForm: document.querySelector("#ingest-form"),
	 editionSelect: document.querySelector("#edition-select"),
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
	if (state.edition) query.set("edition", state.edition);
	if (state.sourceFilter) query.set("source",state.sourceFilter);
    const [bootstrap, digest, sourceStates, editions, run] = await Promise.all([
      request(`/api/bootstrap?${query}`),
	  request(`/api/digest?${query}&limit=30&order=${state.view === "recommended" ? "recommended" : "hybrid"}`),
	  request("/api/sources/health"),
	  request(`/api/editions?profile=${encodeURIComponent(profile)}`),
	  request(`/api/analysis-runs/current?profile=${encodeURIComponent(profile)}`),
    ]);
    state.profile = bootstrap.profile;
    state.profiles = bootstrap.profiles;
    state.feeds = bootstrap.feeds;
    state.items = Array.isArray(digest.items) ? digest.items.map(normalizeItem) : [];
	state.sourceStates = Array.isArray(sourceStates) ? sourceStates : [];
	state.editions = Array.isArray(editions) ? editions : [];
	state.run = run || state.run;
	state.pollingActive = !["completed", "partial_failed", "idle"].includes(state.run.status);
	state.nextCursor = digest.next_cursor || "";
	state.total = Number(digest.total || state.items.length);
    state.selectedID = state.items.some((item) => item.id === selectedID) ? selectedID : state.items[0]?.id || "";
    renderWorkspace();
  } catch (error) {
    elements.digestList.replaceChildren(emptyState(error.message));
    elements.runStatus.textContent = "加载失败";
    showToast(error.message, true);
  } finally {
    state.loading = false;
  }
}

async function loadMoreItems() {
  if (state.loading || state.loadingMore || !state.nextCursor || state.edition) return;
  state.loadingMore = true;
  try {
	const query = new URLSearchParams({ profile: state.profile, limit: "30", cursor: state.nextCursor, order: state.view === "recommended" ? "recommended" : "hybrid" });
	if(state.sourceFilter)query.set("source",state.sourceFilter);
    const digest = await request(`/api/digest?${query}`);
    const incoming = (digest.items || []).map(normalizeItem);
    const known = new Set(state.items.map((item) => item.id));
    state.items.push(...incoming.filter((item) => !known.has(item.id)));
    state.nextCursor = digest.next_cursor || "";
	state.total = Number(digest.total || state.total);
    renderWorkspace();
    const pending = incoming.filter((item) => item.analysis_status !== "completed").map((item) => item.id);
    if (pending.length) {
      await request(`/api/analysis-queue?profile=${encodeURIComponent(state.profile)}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ item_ids: pending }) });
	  state.pollingActive = true;
    }
  } catch (error) { showToast(error.message, true); }
  finally { state.loadingMore = false; }
}

async function refreshAnalysis() {
  if (!state.pollingActive) return;
  if (document.hidden && Date.now() - state.lastBackgroundPoll < 15000) return;
  state.lastBackgroundPoll = Date.now();
  try {
    const rows = [...elements.digestList.querySelectorAll(".digest-row[data-item-id]")];
    const visible = rows.filter((row) => row.offsetTop + row.offsetHeight >= elements.digestList.scrollTop && row.offsetTop <= elements.digestList.scrollTop + elements.digestList.clientHeight).map((row) => row.dataset.itemId);
    if (state.selectedID && !visible.includes(state.selectedID)) visible.push(state.selectedID);
    const query = new URLSearchParams({ profile: state.profile });
    for (const id of visible.slice(0, 100)) query.append("item_id", id);
    const previousAnalyzed=state.run.analyzed||0;
	const [run, updates] = await Promise.all([request(`/api/analysis-runs/current?profile=${encodeURIComponent(state.profile)}`), visible.length ? request(`/api/digest/updates?${query}`) : Promise.resolve({ items: [] })]);
    state.run = run;
    const byID = new Map((updates.items || []).map((item) => [item.id, normalizeItem(item)]));
    let changed = false;
    state.items = state.items.map((item) => { const update = byID.get(item.id); if (!update) return item; if (update.analyzed_at !== item.analyzed_at || update.analysis_status !== item.analysis_status) { changed = true; return update; } return item; });
	if ((run.analyzed||0)>previousAnalyzed){await reloadSortedItems();changed=false}
	else if (changed) { const top = elements.digestList.scrollTop; renderDigest(); elements.digestList.scrollTop = top; renderDetail(); }
    state.pollingActive = !["completed", "partial_failed", "idle"].includes(run.status);
    renderNavigation();
  } catch (_) {}
}

async function reloadSortedItems(){const anchor=state.selectedID;const query=new URLSearchParams({profile:state.profile,limit:"30",order:state.view==="recommended"?"recommended":"hybrid"});if(state.sourceFilter)query.set("source",state.sourceFilter);const digest=await request(`/api/digest?${query}`);state.items=(digest.items||[]).map(normalizeItem);state.nextCursor=digest.next_cursor||"";state.total=Number(digest.total||state.items.length);if(anchor&&state.items.some((item)=>item.id===anchor))state.selectedID=anchor;renderDigest();renderDetail();document.querySelector(`[data-item-id="${cssEscape(state.selectedID)}"]`)?.scrollIntoView({block:"nearest"});}

function renderWorkspace() {
  renderProfiles();
	renderEditions();
  renderSources();
  renderNavigation();
  renderDigest();
  renderDetail();
  refreshIcons();
}

function renderEditions() {
	elements.editionSelect.replaceChildren();
	const current = document.createElement("option");
	current.value = "";
	current.textContent = "当前情报库";
	elements.editionSelect.append(current);
	for (const edition of state.editions) {
		const option = document.createElement("option");
		option.value = String(edition.id);
		const slot = { morning: "早报", evening: "晚报", manual: "手动" }[edition.slot] || edition.slot;
		option.textContent = `${new Date(edition.created_at).toLocaleDateString("zh-CN")} ${slot} · ${edition.item_ids.length} 条`;
		option.selected = option.value === state.edition;
		elements.editionSelect.append(option);
	}
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
	const health = state.sourceStates.find((entry) => entry.url === feed.url);
    const button = makeSourceButton(feed.url, feed.name, feed.disabled ? "circle-off" : health?.fail_count ? "circle-alert" : "rss");
    const sourceButton = button.querySelector(".source-item");
	if (health?.state === "rate_limited") {
	  const retryAt = health.next_retry_at ? new Date(health.next_retry_at).toLocaleString("zh-CN", { hour12: false }) : "稍后";
	  sourceButton.title = `最近抓取被限流；${retryAt} 后可在下次立即运行时重试`;
	} else if (health?.fail_count) {
	  sourceButton.title = `最近抓取失败：${health.last_error || "未知错误"}`;
	}
    sourceButton.classList.toggle("is-active", feed.url === state.sourceFilter);
    sourceButton.disabled = feed.disabled;
    elements.sourceList.append(button);
  }
  const enabled = state.feeds.filter((feed) => !feed.disabled).length;
	const failed = state.sourceStates.filter((entry) => entry.fail_count > 0).length;
  elements.sourceHealth.replaceChildren(icon(failed ? "circle-alert" : "radio"), document.createTextNode(` ${enabled} 个启用来源${failed ? ` · ${failed} 个异常` : ""}`));
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
  elements.digestCount.textContent = String(state.total || filtered.length);
	const pending = (state.run.pending || 0) + (state.run.running || 0) + (state.run.retrying || 0);
	const progress = `已分析 ${state.run.analyzed || 0} / 待分析 ${pending}${state.run.rate_limited ? ` / 等待额度 ${state.run.rate_limited}` : ""}${state.run.failed ? ` / 失败 ${state.run.failed}` : ""}`;
  elements.digestMeta.textContent = state.items.length ? `${state.items.length} 条已加载 · ${progress}` : "等待首次运行";
	const labels = { initial: "首批分析中", background: "后台分析中", rate_limited: "等待模型额度", completed: "运行完成", partial_failed: "部分完成", idle: "本地工作台" };
	if (!state.loading) elements.runStatus.textContent = state.operationStatus&&Date.now()<state.operationUntil?state.operationStatus:(labels[state.run.status] || "本地工作台");
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
  row.dataset.itemId = item.id;
  row.setAttribute("aria-selected", String(item.id === state.selectedID));
  row.classList.toggle("is-selected", item.id === state.selectedID);

  const score = document.createElement("span");
  score.className = "score";
  score.textContent = item.analysis_status === "completed" ? String(item.score) : "…";
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
  summary.textContent = item.analysis_status === "completed" ? firstText(item.summary, item.source_summary, item.why, "尚未生成摘要") : "等待分析，原文已保存";
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
	if (elements.detailPane.dataset.renderedItemId !== item.id) {
	  elements.detailPane.scrollTop = 0;
	  elements.detailPane.dataset.renderedItemId = item.id;
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
	const preferenceFeedback = document.createElement("div");
	preferenceFeedback.className = "preference-feedback";
	for (const [action, label] of [["more-like-this", "希望看同类"], ["too-shallow", "太浅"], ["too-theoretical", "太理论"], ["too-marketing", "太营销"], ["unusable", "无法使用"]]) {
		const button = document.createElement("button");
		button.type = "button";
		button.className = "signal-button";
		button.dataset.action = action;
		button.dataset.itemId = item.id;
		button.textContent = label;
		button.classList.toggle("is-active", item.feedback.includes(action));
		preferenceFeedback.append(button);
	}
  actions.append(feedback);
	actions.append(preferenceFeedback);
  if (!item.analyzed_at) {
    const analyze = document.createElement("button");
    analyze.type = "button";
    analyze.className = "text-action analyze-action";
    analyze.dataset.analyzeItemId = item.id;
    analyze.append(icon("sparkles"), document.createTextNode("分析此条"));
    actions.append(analyze);
  }
  const blockFeed = document.createElement("button");
  blockFeed.type = "button";
  blockFeed.className = "text-action";
  blockFeed.dataset.action = "block-feed";
  blockFeed.dataset.itemId = item.id;
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
  const paragraphs = readableParagraphs(item.content, item.source_summary);
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
  button.dataset.itemId = item.id;
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
	if (state.topic && !matchesTopic(item, state.topic)) {
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

function matchesTopic(item, topic) {
	const text = [item.title, item.analysis_title, item.summary, item.why, ...item.tags].filter(Boolean).join(" ").toLowerCase();
	const terms = {
	  paradigm: ["loop engineering", "harness engineering", "context engineering", "agent evaluation", "范式"],
	  codex: ["codex", "openai/codex"],
	  skills: ["skill", "skills", "mcp", "model context protocol"],
	  model: ["model", "模型", "inference", "推理", "context", "上下文", "eval"],
	  frontend: ["frontend", "前端", "ui", "ux", "design system", "react", "next.js"],
	};
	return (terms[topic] || []).some((term) => text.includes(term));
}

function selectedItem() {
  return state.items.find((item) => item.id === state.selectedID);
}

function normalizeItem(item) {
  return {
    ...item,
    feedback: Array.isArray(item.feedback) ? item.feedback : [],
    key_points: Array.isArray(item.key_points) ? item.key_points : [],
    tags: Array.isArray(item.tags) ? item.tags : [],
  };
}

function readableParagraphs(content, fallback) {
  const source = firstText(content, fallback, "订阅源没有提供可显示的正文。");
  if (!/<[a-z][\s\S]*>/i.test(source)) {
    return source.split(/\n{2,}/);
  }
  const documentNode = new DOMParser().parseFromString(source, "text/html");
  documentNode.querySelectorAll("script, style, noscript, template, iframe, svg").forEach((node) => node.remove());
  const blocks = [];
  for (const node of documentNode.body.querySelectorAll("h1, h2, h3, h4, p, li, blockquote, pre")) {
    const text = normalizeReadableText(node.textContent);
    if (text && blocks[blocks.length - 1] !== text) {
      blocks.push(text);
    }
  }
  if (blocks.length) {
    return blocks;
  }
  return [normalizeReadableText(documentNode.body.textContent) || fallback];
}

function normalizeReadableText(value) {
  return String(value || "").replace(/\u00a0/g, " ").replace(/\n{3,}/g, "\n\n").trim();
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
  if (state.loading) {
    return;
  }
  elements.runButton.disabled = true;
  elements.runStatus.textContent = "正在运行";
	state.pollingActive = true;
  try {
    const query = new URLSearchParams({ profile: state.profile });
    const result = await request(`/api/run?${query}`, { method: "POST" });
    showToast(`完成：抓取 ${result.fetched}，缓存 ${result.cached}，首轮分析 ${result.analyzed}，后台排队 ${result.queued}`);
    await loadWorkspace(state.profile, state.selectedID);
  } catch (error) {
    showToast(error.message, true);
    elements.runStatus.textContent = "运行失败";
  } finally {
    elements.runButton.disabled = false;
  }
}

async function analyzeItem(itemID) {
  const item = state.items.find((entry) => entry.id === itemID);
  if (!item) {
    return;
  }
	const buttons=[...document.querySelectorAll(`[data-analyze-item-id="${cssEscape(itemID)}"]`)];buttons.forEach((button)=>button.disabled=true);
	state.operationStatus="分析此条";state.operationUntil=Date.now()+3600000;elements.runStatus.textContent="分析此条";
  try {
    const query = new URLSearchParams({ profile: state.profile, item_id: itemID });
    const result = await request(`/api/analyze?${query}`, { method: "POST" });
	state.operationStatus="分析完成";state.operationUntil=Date.now()+3000;elements.runStatus.textContent="分析完成";
	showToast(result.cached ? "已使用现有分析结果" : "分析完成");
    await loadWorkspace(state.profile, itemID);
	document.querySelector(`[data-item-id="${cssEscape(itemID)}"]`)?.scrollIntoView({block:"start"});
  } catch (error) {
    showToast(error.message, true);
	state.operationStatus="分析失败";state.operationUntil=Date.now()+4000;elements.runStatus.textContent="分析失败";
	}finally{buttons.forEach((button)=>button.disabled=false)}
}

function openIngestDialog() {
	elements.ingestForm.reset();
	elements.ingestDialog.showModal();
	elements.ingestForm.elements.url.focus();
}

async function submitIngest(event) {
	event.preventDefault();
	const form = new FormData(elements.ingestForm);
	const submit = elements.ingestForm.querySelector('[type="submit"]');
	submit.disabled = true;
	try {
		const result = await request("/api/ingest", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				profile: state.profile,
				url: String(form.get("url") || "").trim(),
				title: String(form.get("title") || "").trim(),
				content: String(form.get("content") || "").trim(),
				tags: String(form.get("tags") || "").split(",").map((tag) => tag.trim()).filter(Boolean),
			}),
		});
		elements.ingestDialog.close();
		showToast("已加入情报库，可以继续分析此条");
		await loadWorkspace(state.profile, result.item_id);
	} catch (error) {
		showToast(error.message, true);
	} finally {
		submit.disabled = false;
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

elements.editionSelect.addEventListener("change", () => {
	state.edition = elements.editionSelect.value;
	loadWorkspace(state.profile, "");
});

elements.digestSearch.addEventListener("input", () => {
  state.search = elements.digestSearch.value;
  renderNavigation();
  renderDigest();
});

document.addEventListener("click", (event) => {
  const analyze = event.target.closest("[data-analyze-item-id]");
  if (analyze) {
    event.preventDefault();
    event.stopPropagation();
    analyzeItem(analyze.dataset.analyzeItemId);
    return;
  }
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
    loadWorkspace(state.profile, "");
    return;
  }
  const source = event.target.closest(".source-item[data-source]");
  if (source) {
    state.collectionFilter = "all";
    state.view = "all";
    state.sourceFilter = source.dataset.source;
	state.nextCursor="";loadWorkspace(state.profile,"");
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

elements.digestList.addEventListener("scroll", () => {
  if (elements.digestList.scrollTop + elements.digestList.clientHeight >= elements.digestList.scrollHeight - 120) loadMoreItems();
});

window.setInterval(refreshAnalysis, 3000);

elements.runButton.addEventListener("click", runNow);
elements.ingestTrigger.addEventListener("click", openIngestDialog);
elements.ingestForm.addEventListener("submit", submitIngest);
for (const close of document.querySelectorAll("[data-close-ingest]")) {
	close.addEventListener("click", () => elements.ingestDialog.close());
}
elements.topicSelect.addEventListener("change", () => {
	state.topic = elements.topicSelect.value;
	renderNavigation();
	renderDigest();
});
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
