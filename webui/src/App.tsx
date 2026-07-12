import { useEffect, useMemo, useRef, useState } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive, Ban, Bookmark, Check, ChevronDown, Clock3, Command, ExternalLink, Inbox, Layers3,
  Link2, Menu, MessageSquareText, PanelLeftClose, Play, Plus, Search, Sparkles, ThumbsDown,
  ThumbsUp, X,
} from "lucide-react";
import { api } from "./api";
import { safeArticleHTML } from "./sanitize";
import type { CollectionFilter, DigestItem, ViewMode } from "./types";

const topics: Record<string, string[]> = {
  codex: ["codex", "openai/codex"], skills: ["skill", "skills", "mcp", "model context protocol"],
  model: ["model", "模型", "inference", "推理", "context", "上下文", "eval"],
  frontend: ["frontend", "前端", "ui", "ux", "design system", "react", "next.js"],
};

function App() {
  const queryClient = useQueryClient();
  const [profile, setProfile] = useState("default");
  const [view, setView] = useState<ViewMode>("all");
  const [collection, setCollection] = useState<CollectionFilter>("all");
  const [source, setSource] = useState("");
  const [edition, setEdition] = useState("");
  const [topic, setTopic] = useState("");
  const [search, setSearch] = useState("");
  const [selectedID, setSelectedID] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [toast, setToast] = useState<{ message: string; error?: boolean } | null>(null);
  const [commandSearch, setCommandSearch] = useState("");
  const ingestDialog = useRef<HTMLDialogElement>(null);
  const commandDialog = useRef<HTMLDialogElement>(null);
  const searchInput = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const previousAnalyzed = useRef(0);

  const bootstrap = useQuery({ queryKey: ["bootstrap", profile], queryFn: () => api.bootstrap(profile) });
  const health = useQuery({ queryKey: ["health"], queryFn: api.health, refetchInterval: 30_000 });
  const editions = useQuery({ queryKey: ["editions", profile], queryFn: () => api.editions(profile) });
  const runState = useQuery({
    queryKey: ["run-state", profile], queryFn: () => api.runState(profile), refetchInterval: 3000,
  });

  const digest = useInfiniteQuery({
    queryKey: ["digest", profile, view, source, edition],
    initialPageParam: "",
    queryFn: ({ pageParam }) => {
      const params = new URLSearchParams({ profile, limit: "30", order: view === "recommended" ? "recommended" : "hybrid" });
      if (pageParam) params.set("cursor", pageParam);
      if (source) params.set("source", source);
      if (edition) params.set("edition", edition);
      return api.digest(params);
    },
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  });

  const allItems = useMemo(() => digest.data?.pages.flatMap((page) => page.items) || [], [digest.data]);
  const visibleItems = useMemo(() => allItems.filter((item) => {
    if (collection === "saved" && !item.feedback.includes("save")) return false;
    if (collection === "later" && !item.feedback.includes("later")) return false;
    if (view === "delivered" && !item.pushed) return false;
    const haystack = [item.analysis_title, item.title, item.feed_name, item.summary, item.tags.join(" ")].join(" ").toLowerCase();
    if (search.trim() && !haystack.includes(search.trim().toLowerCase())) return false;
    if (topic && !(topics[topic] || []).some((term) => haystack.includes(term))) return false;
    return true;
  }), [allItems, collection, search, topic, view]);
  const selected = allItems.find((item) => item.id === selectedID) || visibleItems[0];
  const total = digest.data?.pages[0]?.total || visibleItems.length;

  useEffect(() => {
    if (selected && selected.id !== selectedID) setSelectedID(selected.id);
  }, [selected, selectedID]);
  useEffect(() => {
    const analyzed = runState.data?.analyzed || 0;
    if (analyzed > previousAnalyzed.current) queryClient.invalidateQueries({ queryKey: ["digest", profile] });
    previousAnalyzed.current = analyzed;
  }, [profile, queryClient, runState.data?.analyzed]);
  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 3600);
    return () => window.clearTimeout(timer);
  }, [toast]);

  const feedbackMutation = useMutation({
    mutationFn: ({ item, action }: { item: DigestItem; action: string }) => api.feedback(profile, item.id, action, item.feedback.includes(action)),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["digest", profile] }),
    onError: (error) => notify(error.message, true),
  });
  const analyzeMutation = useMutation({
    mutationFn: (itemID: string) => api.analyze(profile, itemID),
    onSuccess: async () => { notify("分析完成"); await queryClient.invalidateQueries({ queryKey: ["digest", profile] }); },
    onError: (error) => notify(error.message, true),
  });
  const runMutation = useMutation({
    mutationFn: () => api.run(profile),
    onSuccess: async (result) => {
      notify(`完成：抓取 ${result.fetched}，分析 ${result.analyzed}，后台排队 ${result.queued}`);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["digest", profile] }),
        queryClient.invalidateQueries({ queryKey: ["run-state", profile] }),
        queryClient.invalidateQueries({ queryKey: ["health"] }),
      ]);
    },
    onError: (error) => notify(error.message, true),
  });
  const ingestMutation = useMutation({
    mutationFn: (body: { url: string; title: string; content: string; tags: string[] }) => api.ingest({ profile, ...body }),
    onSuccess: async (result) => {
      ingestDialog.current?.close(); notify("已加入情报库"); setSelectedID(result.item_id);
      await queryClient.invalidateQueries({ queryKey: ["digest", profile] });
    },
    onError: (error) => notify(error.message, true),
  });

  function notify(message: string, error = false) { setToast({ message, error }); }
  function resetLibrary(next: Partial<{ view: ViewMode; collection: CollectionFilter; source: string; edition: string }>) {
    if (next.view !== undefined) setView(next.view);
    if (next.collection !== undefined) setCollection(next.collection);
    if (next.source !== undefined) setSource(next.source);
    if (next.edition !== undefined) setEdition(next.edition);
    setSelectedID("");
  }
  function toggleFeedback(item: DigestItem, action: string) { feedbackMutation.mutate({ item, action }); }
  function loadMore(event: React.UIEvent<HTMLDivElement>) {
    const node = event.currentTarget;
    if (node.scrollTop + node.clientHeight >= node.scrollHeight - 140 && digest.hasNextPage && !digest.isFetchingNextPage) digest.fetchNextPage();
  }

  useEffect(() => {
    const listener = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      const input = ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName);
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") { event.preventDefault(); commandDialog.current?.showModal(); return; }
      if (input || commandDialog.current?.open || ingestDialog.current?.open) return;
      if (event.key === "/") { event.preventDefault(); searchInput.current?.focus(); }
      if ((event.key === "ArrowDown" || event.key === "ArrowUp") && visibleItems.length) {
        event.preventDefault(); const current = visibleItems.findIndex((item) => item.id === selectedID);
        const delta = event.key === "ArrowDown" ? 1 : -1;
        setSelectedID(visibleItems[(Math.max(current, 0) + delta + visibleItems.length) % visibleItems.length].id);
      }
      if (event.key.toLowerCase() === "s" && selected) toggleFeedback(selected, "save");
      if (event.key.toLowerCase() === "l" && selected) toggleFeedback(selected, "later");
    };
    document.addEventListener("keydown", listener);
    return () => document.removeEventListener("keydown", listener);
  }, [selected, selectedID, visibleItems]);

  const failedSources = (health.data || []).filter((entry) => entry.fail_count > 0).length;
  const run = runState.data;
  const pending = (run?.pending || 0) + (run?.running || 0) + (run?.retrying || 0);
  const isRunning = runMutation.isPending || !["completed", "partial_failed", "idle", undefined].includes(run?.status);
  const runLabel = isRunning ? `分析中 ${run?.analyzed || 0} / ${(run?.total || 0)}` : run?.status === "partial_failed" ? "部分完成" : "分析完成";

  return <>
    <a className="skip-link" href="#reader">跳到文章正文</a>
    <header className="topbar">
      <div className="brand-cluster">
        <button className="icon-button" type="button" aria-label="打开来源与历史" title="来源与历史" onClick={() => setDrawerOpen(true)}><Menu /></button>
        <span className="brand-mark" aria-hidden="true">R</span><strong>RSS Agent</strong><span className="edition-label">今日情报</span>
      </div>
      <div className="run-state" role="status" aria-live="polite"><span className={`status-dot${isRunning ? " is-running" : ""}`} /><span>{runLabel}{pending ? ` · 待处理 ${pending}` : ""}</span></div>
      <div className="top-actions">
        <button className="secondary-button" type="button" aria-label="投喂内容" onClick={() => ingestDialog.current?.showModal()}><Link2 /><span>投喂</span></button>
        <button className="secondary-button" type="button" aria-label="打开命令面板" onClick={() => commandDialog.current?.showModal()}><Command /><span>命令</span><kbd>Ctrl K</kbd></button>
        <button className="primary-button" type="button" aria-label="立即运行" disabled={runMutation.isPending} onClick={() => runMutation.mutate()}><Play /><span>立即运行</span></button>
      </div>
    </header>

    <main className="workspace">
      <section className="queue" aria-label="文章队列">
        <header className="queue-header"><div><p className="eyebrow">TODAY'S INTELLIGENCE</p><h1>{collection === "saved" ? "已收藏" : collection === "later" ? "稍后阅读" : "值得读的内容"}</h1></div><span className="count">{total} 篇</span></header>
        <div className="queue-tools">
          <label className="search-field"><Search /><span className="sr-only">搜索文章</span><input ref={searchInput} value={search} onChange={(event) => setSearch(event.target.value)} type="search" placeholder="搜索标题、来源和标签"/><kbd>/</kbd></label>
          <div className="filters-row">
            <div className="tabs" role="tablist" aria-label="文章范围">
              {([['all','全部'],['recommended','推荐'],['delivered','已推送']] as [ViewMode,string][]).map(([value,label]) => <button key={value} className={`tab${view === value ? " is-active" : ""}`} type="button" role="tab" aria-selected={view === value} onClick={() => resetLibrary({ view: value, collection: "all" })}>{label}</button>)}
            </div>
            <label className="compact-select"><span className="sr-only">主题</span><select value={topic} onChange={(event) => setTopic(event.target.value)}><option value="">全部主题</option><option value="codex">Codex</option><option value="skills">Skills / MCP</option><option value="model">模型与基础设施</option><option value="frontend">前端实践</option></select><ChevronDown /></label>
          </div>
        </div>
        <div className="article-list" ref={listRef} onScroll={loadMore} role="listbox" aria-busy={digest.isLoading}>
          {digest.isLoading ? <ListSkeleton /> : digest.isError ? <EmptyState message={(digest.error as Error).message} /> : visibleItems.length ? visibleItems.map((item) => <ArticleRow key={item.id} item={item} selected={item.id === selected?.id} onSelect={() => setSelectedID(item.id)} onFeedback={(action) => toggleFeedback(item, action)} />) : <EmptyState message="没有匹配的内容" />}
          {digest.isFetchingNextPage && <div className="load-more">正在加载更多内容</div>}
        </div>
      </section>
      <Reader item={selected} analyzePending={analyzeMutation.isPending} onAnalyze={() => selected && analyzeMutation.mutate(selected.id)} onFeedback={(action) => selected && toggleFeedback(selected, action)} />
    </main>

    {drawerOpen && <div className="drawer-scrim" onClick={() => setDrawerOpen(false)} />}
    <aside className={`library-drawer${drawerOpen ? " is-open" : ""}`} aria-hidden={!drawerOpen}>
      <header className="drawer-header"><div><p className="eyebrow">LIBRARY</p><h2>来源与历史</h2></div><button className="icon-button icon-button-dark" type="button" aria-label="关闭来源与历史" onClick={() => setDrawerOpen(false)}><X /></button></header>
      <label className="drawer-field">Profile<select value={profile} onChange={(event) => { setProfile(event.target.value); setSelectedID(""); }}>{(bootstrap.data?.profiles || [profile]).map((name) => <option key={name}>{name}</option>)}</select></label>
      <nav className="drawer-nav" aria-label="内容集合">
        <DrawerLink icon={<Inbox />} label="今日 Digest" count={total} active={collection === "all"} onClick={() => { resetLibrary({ collection: "all", view: "all", source: "", edition: "" }); setDrawerOpen(false); }} />
        <DrawerLink icon={<Bookmark />} label="已收藏" active={collection === "saved"} onClick={() => { resetLibrary({ collection: "saved", view: "all", source: "", edition: "" }); setDrawerOpen(false); }} />
        <DrawerLink icon={<Clock3 />} label="稍后阅读" active={collection === "later"} onClick={() => { resetLibrary({ collection: "later", view: "all", source: "", edition: "" }); setDrawerOpen(false); }} />
      </nav>
      <div className="drawer-section-title"><span>订阅源</span><span>{failedSources ? `${failedSources} 个异常` : `${bootstrap.data?.feeds.length || 0} 个启用`}</span></div>
      <div className="source-list">
        <button type="button" className={!source ? "is-active" : ""} onClick={() => { resetLibrary({ source: "", collection: "all", edition: "" }); setDrawerOpen(false); }}><Layers3 /><span>全部来源</span></button>
        {(bootstrap.data?.feeds || []).map((feed) => {
          const state = health.data?.find((entry) => entry.url === feed.url);
          const title = state?.state === "rate_limited" ? `限流至 ${new Date(state.next_retry_at).toLocaleString("zh-CN", { hour12: false })}` : state?.last_error || feed.name;
          return <button key={feed.url} type="button" disabled={feed.disabled} title={title} className={source === feed.url ? "is-active" : ""} onClick={() => { resetLibrary({ source: feed.url, collection: "all", edition: "" }); setDrawerOpen(false); }}><span className={`health-dot ${state?.state || "unknown"}`} /><span>{feed.name}</span></button>;
        })}
      </div>
      <div className="drawer-history"><div className="drawer-section-title"><span>历史 Digest</span></div>
        <button type="button" className={!edition ? "is-active" : ""} onClick={() => { resetLibrary({ edition: "" }); setDrawerOpen(false); }}><span>当前情报库</span></button>
        {(editions.data || []).map((entry) => <button key={entry.id} type="button" className={edition === String(entry.id) ? "is-active" : ""} onClick={() => { resetLibrary({ edition: String(entry.id), source: "" }); setDrawerOpen(false); }}><span>{formatEdition(entry.slot, entry.created_at)}</span><small>{entry.item_ids.length} 篇</small></button>)}
      </div>
    </aside>

    <IngestDialog dialogRef={ingestDialog} pending={ingestMutation.isPending} onSubmit={(body) => ingestMutation.mutate(body)} />
    <CommandDialog dialogRef={commandDialog} query={commandSearch} setQuery={setCommandSearch} commands={[
      { label: "立即运行当前 Profile", icon: <Play />, action: () => runMutation.mutate() },
      { label: "聚焦 Digest 搜索", icon: <Search />, action: () => searchInput.current?.focus() },
      { label: "显示全部条目", icon: <Inbox />, action: () => resetLibrary({ collection: "all", view: "all", source: "" }) },
      { label: "显示已收藏条目", icon: <Bookmark />, action: () => resetLibrary({ collection: "saved", view: "all", source: "" }) },
    ]} />
    {toast && <div className={`toast${toast.error ? " is-error" : ""}`} role="status">{toast.error ? <Ban /> : <Check />}{toast.message}</div>}
  </>;
}

function ArticleRow({ item, selected, onSelect, onFeedback }: { item: DigestItem; selected: boolean; onSelect: () => void; onFeedback: (action: string) => void }) {
  return <button className={`article-row${selected ? " is-selected" : ""}`} type="button" role="option" aria-selected={selected} onClick={onSelect}>
    <span className="row-score">{item.analysis_status === "completed" ? item.score : "…"}</span>
    <span className="row-copy"><strong className="row-title">{displayTitle(item)}</strong><span className="row-summary">{item.analysis_status === "completed" ? firstText(item.summary, item.source_summary, item.why) : "等待分析，原文已保存"}</span><span className="row-meta"><span>{item.feed_name}</span><span>·</span><span>{relativeTime(item.published_at)}</span>{item.should_push && <span className="recommended">· 推荐</span>}</span></span>
    <span className="row-actions"><span role="button" tabIndex={0} aria-label="收藏" title="收藏" className={item.feedback.includes("save") ? "is-active" : ""} onClick={(event) => { event.stopPropagation(); onFeedback("save"); }}><Bookmark /></span><span role="button" tabIndex={0} aria-label="稍后阅读" title="稍后阅读" onClick={(event) => { event.stopPropagation(); onFeedback("later"); }}><Clock3 /></span><span role="button" tabIndex={0} aria-label="屏蔽" title="屏蔽" onClick={(event) => { event.stopPropagation(); onFeedback("block"); }}><Ban /></span></span>
  </button>;
}

function Reader({ item, analyzePending, onAnalyze, onFeedback }: { item?: DigestItem; analyzePending: boolean; onAnalyze: () => void; onFeedback: (action: string) => void }) {
  if (!item) return <article className="reader empty-reader"><Archive /><p>从左侧选择一篇内容</p></article>;
  const complete = item.analysis_status === "completed";
  return <article className="reader" id="reader" tabIndex={-1}><div className="reader-inner">
    <header className="article-header"><div className="source-line"><span className="source-avatar">{item.feed_name.slice(0, 1).toUpperCase()}</span><span>{item.feed_name}</span><span>·</span><time>{relativeTime(item.published_at)}</time>{item.author && <><span>·</span><span>{item.author}</span></>}</div><h2>{displayTitle(item)}</h2><p className="dek">{firstText(item.summary, item.source_summary, "原始订阅源没有提供摘要。")}</p>
      <div className="article-toolbar"><FeedbackButton icon={<ThumbsUp />} label="有用" active={item.feedback.includes("like")} onClick={() => onFeedback("like")} /><FeedbackButton icon={<ThumbsDown />} label="不喜欢" active={item.feedback.includes("dislike")} onClick={() => onFeedback("dislike")} /><FeedbackButton icon={<Bookmark />} label="收藏" active={item.feedback.includes("save")} onClick={() => onFeedback("save")} /><FeedbackButton icon={<Clock3 />} label="稍后" active={item.feedback.includes("later")} onClick={() => onFeedback("later")} /><span className="toolbar-spacer" />{item.link && <a className="text-button" href={safeURL(item.link)} target="_blank" rel="noreferrer"><ExternalLink />打开原文</a>}</div>
    </header>
    <section className="analysis-band"><div className="analysis-heading"><Sparkles /><div><p className="eyebrow">AGENT BRIEF</p><h3>为什么值得读</h3></div><span className="score-badge">{complete ? item.score : "…"}<small>/10</small></span></div>
      {complete ? <><p className="why">{firstText(item.why, "该条目已分析，但没有推荐说明。")}</p><div className="analysis-grid"><div><h4>一句话结论</h4><p>{firstText(item.summary, item.source_summary)}</p></div><div><h4>模型判断</h4><p>{item.should_push ? "建议进入推荐列表，值得优先阅读。" : "保留在情报库中，不进入推荐列表。"}</p></div></div>{item.key_points.length > 0 && <ul className="key-points">{item.key_points.map((point) => <li key={point}>{point}</li>)}</ul>}<div className="tag-list">{item.tags.map((tag) => <span className="tag" key={tag}>{tag}</span>)}</div></> : <div className="pending-analysis"><MessageSquareText /><div><strong>原文已保存，正在等待分析</strong><p>可以将此条提升到队列前面。</p></div><button className="primary-button" type="button" disabled={analyzePending} onClick={onAnalyze}><Sparkles />分析此条</button></div>}
    </section>
    <div className="quick-feedback"><span>调整后续推荐</span>{[["more-like-this","希望看同类"],["too-shallow","太浅"],["too-theoretical","太理论"],["too-marketing","太营销"],["unusable","无法使用"]].map(([action,label]) => <button key={action} type="button" className={item.feedback.includes(action) ? "is-active" : ""} onClick={() => onFeedback(action)}>{label}</button>)}</div>
    <section className="article-body"><div className="section-heading"><p className="eyebrow">SOURCE</p><h3>原文</h3></div><div className="rich-content" dangerouslySetInnerHTML={{ __html: safeArticleHTML(item.content, item.source_summary) }} /></section>
    <section className="analysis-metadata"><h3>分析信息</h3><dl><Meta label="评分" value={complete ? `${item.score}/10` : "未分析"}/><Meta label="建议推送" value={item.should_push ? "是" : "否"}/><Meta label="模型" value={firstText(item.model_label, item.model_name, "未分析")}/><Meta label="分析时间" value={relativeTime(item.analyzed_at)}/><Meta label="状态" value={item.seen ? "已处理" : "待处理"}/></dl></section>
  </div></article>;
}

function FeedbackButton({ icon, label, active, onClick }: { icon: React.ReactNode; label: string; active: boolean; onClick: () => void }) { return <button className={`feedback-button${active ? " is-active" : ""}`} type="button" aria-pressed={active} onClick={onClick}>{icon}<span>{label}</span></button>; }
function Meta({ label, value }: { label: string; value: string }) { return <div><dt>{label}</dt><dd>{value}</dd></div>; }
function DrawerLink({ icon, label, count, active, onClick }: { icon: React.ReactNode; label: string; count?: number; active: boolean; onClick: () => void }) { return <button className={`drawer-link${active ? " is-active" : ""}`} type="button" onClick={onClick}>{icon}<span>{label}</span>{count !== undefined && <b>{count}</b>}</button>; }

function IngestDialog({ dialogRef, pending, onSubmit }: { dialogRef: React.RefObject<HTMLDialogElement | null>; pending: boolean; onSubmit: (body: { url: string; title: string; content: string; tags: string[] }) => void }) {
  return <dialog ref={dialogRef}><form className="dialog-surface" onSubmit={(event) => { event.preventDefault(); const data = new FormData(event.currentTarget); onSubmit({ url: String(data.get("url") || ""), title: String(data.get("title") || ""), content: String(data.get("content") || ""), tags: String(data.get("tags") || "").split(",").map((tag) => tag.trim()).filter(Boolean) }); }}><header><div><p className="eyebrow">INTEREST SAMPLE</p><h2>投喂一条内容</h2></div><button className="icon-button" type="button" aria-label="关闭" onClick={() => dialogRef.current?.close()}><X /></button></header><label>文章或 X 链接<input name="url" type="url" required placeholder="https://..." /></label><label>标题（可选）<input name="title" /></label><label>补充正文（可选）<textarea name="content" rows={4} /></label><label>标签（逗号分隔）<input name="tags" placeholder="codex, applied-ai" /></label><footer><button className="secondary-button" type="button" onClick={() => dialogRef.current?.close()}>取消</button><button className="primary-button" disabled={pending} type="submit">加入兴趣样本</button></footer></form></dialog>;
}
function CommandDialog({ dialogRef, query, setQuery, commands }: { dialogRef: React.RefObject<HTMLDialogElement | null>; query: string; setQuery: (value: string) => void; commands: { label: string; icon: React.ReactNode; action: () => void }[] }) {
  const filtered = commands.filter((command) => command.label.includes(query));
  return <dialog ref={dialogRef} className="command-dialog"><div className="command-surface"><label><Search /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索命令" autoFocus /></label><div className="command-list">{filtered.map((command) => <button key={command.label} type="button" onClick={() => { dialogRef.current?.close(); command.action(); }}>{command.icon}<span>{command.label}</span></button>)}</div></div></dialog>;
}
function ListSkeleton() { return <div className="skeleton-list">{Array.from({ length: 5 }, (_, index) => <div key={index}><span /><p /><small /></div>)}</div>; }
function EmptyState({ message }: { message: string }) { return <div className="empty-state"><Archive /><p>{message}</p></div>; }
function displayTitle(item: DigestItem) { return firstText(item.analysis_title, item.title, "未命名条目"); }
function firstText(...values: string[]) { return values.find((value) => typeof value === "string" && value.trim()) || ""; }
function safeURL(value: string) { try { const url = new URL(value); return ["http:", "https:"].includes(url.protocol) ? url.href : ""; } catch { return ""; } }
function relativeTime(value: string) { if (!value) return "时间未知"; const date = new Date(value); if (Number.isNaN(date.getTime())) return "时间未知"; const seconds = Math.round((date.getTime() - Date.now()) / 1000); const formatter = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" }); const ranges: [number, Intl.RelativeTimeFormatUnit][] = [[60,"second"],[60,"minute"],[24,"hour"],[7,"day"],[4.34524,"week"],[12,"month"],[Infinity,"year"]]; let amount = seconds; for (const [limit,unit] of ranges) { if (Math.abs(amount) < limit) return formatter.format(Math.round(amount), unit); amount /= limit; } return date.toLocaleDateString("zh-CN"); }
function formatEdition(slot: string, time: string) { const label = ({ morning: "早报", evening: "晚报", manual: "手动" } as Record<string,string>)[slot] || slot; return `${new Date(time).toLocaleDateString("zh-CN")} · ${label}`; }

export default App;
