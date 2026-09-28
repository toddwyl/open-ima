import { Children, cloneElement, Fragment, isValidElement, memo, useCallback, useEffect, useRef, useState, type ChangeEvent, type ElementType, type FormEvent, type ReactNode } from "react";
import {
  AlertCircle, ArrowUp, BookOpen, Check, ChevronLeft, ChevronRight, CircleDashed, FileText, FolderOpen, Globe,
  Link, LoaderCircle, Menu, MessageSquareText, Plus, RefreshCw,
  Database, ExternalLink, Eye, EyeOff, Files, Save, Search, Settings, Sparkles, Trash2, Upload, X, Copy,
} from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { api, streamChat } from "./api";
import type { AgentStep, AppSettings, ChatModel, Citation, Conversation, Media, MediaContent, KnowledgeBase, Message, SearchResult } from "./types";

type Tab = "medias" | "chat" | "search";
type ReadFn = (mediaBizID: string, chunkBizID?: string) => void;

const ALLOWED_EXTENSIONS = new Set([".pdf", ".docx", ".pptx", ".md", ".txt", ".html", ".htm"]);
const ACCEPT_ATTRIBUTE = Array.from(ALLOWED_EXTENSIONS).join(",");
const TYPE_HINT = "PDF、Word、PPT、Markdown、文本或 HTML";

function extensionOf(name: string) {
  const dot = name.lastIndexOf(".");
  return dot < 0 ? "" : name.slice(dot).toLowerCase();
}

async function collectDroppedFiles(dataTransfer: DataTransfer): Promise<File[]> {
  const items = Array.from(dataTransfer.items ?? []).filter((item) => item.kind === "file");
  const entries = items
    .map((item) => (typeof item.webkitGetAsEntry === "function" ? item.webkitGetAsEntry() : null))
    .filter((entry): entry is FileSystemEntry => entry !== null);
  if (!entries.length) return Array.from(dataTransfer.files ?? []);
  const files: File[] = [];
  const walk = (entry: FileSystemEntry): Promise<void> =>
    new Promise((resolve) => {
      if (entry.isFile) {
        (entry as FileSystemFileEntry).file(
          (file) => { files.push(file); resolve(); },
          () => resolve(),
        );
      } else if (entry.isDirectory) {
        const reader = (entry as FileSystemDirectoryEntry).createReader();
        const readBatch = () =>
          reader.readEntries(async (children) => {
            if (!children.length) { resolve(); return; }
            for (const child of children) await walk(child);
            readBatch();
          }, () => resolve());
        readBatch();
      } else {
        resolve();
      }
    });
  for (const entry of entries) await walk(entry);
  return files;
}

export default function App() {
  const [knowledgeBases, setKnowledgeBases] = useState<KnowledgeBase[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("chat");
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [highlightMediaID, setHighlightMediaID] = useState<string | null>(null);
  const [reader, setReader] = useState<{ mediaBizID: string; chunkBizID?: string } | null>(null);

  const refreshKBs = useCallback(async () => {
    try {
      const list = (await api.listKBs()) || [];
      setKnowledgeBases(list);
      setSelectedID((current) => current && list.some((item) => item.biz_id === current) ? current : list[0]?.biz_id || null);
      setError("");
    } catch (cause) {
      setError(messageOf(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void refreshKBs(); }, [refreshKBs]);
  const selected = knowledgeBases.find((item) => item.biz_id === selectedID) || null;

  const chooseKB = (id: string) => {
    setSelectedID(id);
    setSidebarOpen(false);
    setTab("chat");
    setSettingsOpen(false);
    setHighlightMediaID(null);
    setReader(null);
  };

  return (
    <div className="app-shell">
      <aside className={`sidebar ${sidebarOpen ? "sidebar-open" : ""}`}>
        <div className="brand-row">
          <div className="brand-mark"><img src="/open-ima-mascot.png" alt="" /></div>
          <div><strong>Open IMA</strong><span>个人知识工作台</span></div>
          <button className="icon-button mobile-only" onClick={() => setSidebarOpen(false)} aria-label="关闭导航"><X size={18} /></button>
        </div>
        <div className="side-heading"><span>知识库</span><button className="icon-button" onClick={() => setCreateOpen(true)} aria-label="新建知识库" title="新建知识库"><Plus size={17} /></button></div>
        <nav className="kb-list" aria-label="知识库列表">
          {knowledgeBases.map((kb, index) => (
            <button key={kb.biz_id} className={`kb-item ${kb.biz_id === selectedID ? "active" : ""}`} onClick={() => chooseKB(kb.biz_id)} style={{ animationDelay: `${index * 45}ms` }}>
              <span className="kb-glyph">{kb.name.slice(0, 1).toUpperCase()}</span>
              <span className="kb-copy"><strong>{kb.name}</strong><small>{kb.media_count} 份文档</small></span>
              <ChevronRight size={15} />
            </button>
          ))}
          {!loading && knowledgeBases.length === 0 && <p className="side-empty">还没有知识库</p>}
        </nav>
        <div className="sidebar-foot"><span className="status-dot" /><span>本地工作区</span><button className={`icon-button ${settingsOpen ? "active" : ""}`} onClick={() => { setSettingsOpen(true); setSidebarOpen(false); }} aria-label="配置中心" title="配置中心"><Settings size={17} /></button></div>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <button className="icon-button mobile-only" onClick={() => setSidebarOpen(true)} aria-label="打开导航"><Menu size={19} /></button>
          <div className="title-block">
            <span className="eyebrow">{settingsOpen ? "本地系统" : "知识库"}</span>
            <h1>{settingsOpen ? "配置中心" : selected?.name || "选择一个知识库"}</h1>
            {settingsOpen ? <p>模型服务与本地检索引擎</p> : selected?.description && <p>{selected.description}</p>}
          </div>
          {!settingsOpen && selected && <button className="icon-button danger-ghost" title="删除知识库" aria-label="删除知识库" onClick={async () => {
            if (!window.confirm(`删除“${selected.name}”及其所有文档？`)) return;
            try { await api.deleteKB(selected.biz_id); await refreshKBs(); } catch (cause) { setError(messageOf(cause)); }
          }}><Trash2 size={17} /></button>}
        </header>

        {error && <div className="global-error"><AlertCircle size={17} /><span>{error}</span><button onClick={() => setError("")} aria-label="关闭错误"><X size={15} /></button></div>}

        {settingsOpen ? <section className="tab-content"><SettingsView onError={setError} /></section> : !selected ? (
          <EmptyWorkspace loading={loading} onCreate={() => setCreateOpen(true)} />
        ) : (
          <>
            <WorkspaceBar kb={selected} onOpenChat={() => setTab("chat")} onOpenSearch={() => setTab("search")} onOpenMedias={() => setTab("medias")} />
            <div className="tabs" role="tablist">
              <TabButton active={tab === "chat"} onClick={() => setTab("chat")} icon={<MessageSquareText size={16} />} label="问答" />
              <TabButton active={tab === "medias"} onClick={() => setTab("medias")} icon={<FileText size={16} />} label="文档" />
              <TabButton active={tab === "search"} onClick={() => setTab("search")} icon={<Search size={16} />} label="搜索" />
            </div>
            <section className="tab-content">
              {tab === "medias" && <MediasView kb={selected} onError={setError} onCountChange={refreshKBs} highlightBizID={highlightMediaID} onReadMedia={(mediaBizID) => setReader({ mediaBizID })} />}
              {tab === "chat" && <ChatView kb={selected} onError={setError} onLocateMedia={(mediaBizID) => { setHighlightMediaID(mediaBizID); setTab("medias"); }} onReadMedia={(mediaBizID, chunkBizID) => setReader({ mediaBizID, chunkBizID })} />}
              {tab === "search" && <SearchView kb={selected} onError={setError} />}
            </section>
          </>
        )}
      </main>

      {sidebarOpen && <button className="scrim mobile-only" onClick={() => setSidebarOpen(false)} aria-label="关闭导航遮罩" />}
      {createOpen && <CreateDialog onClose={() => setCreateOpen(false)} onCreated={async (kb) => { await refreshKBs(); setSelectedID(kb.biz_id); setCreateOpen(false); }} />}
      {reader && <MediaReader mediaBizID={reader.mediaBizID} focusChunkBizID={reader.chunkBizID} onClose={() => setReader(null)} />}
    </div>
  );
}

function TabButton({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: ReactNode; label: string }) {
  return <button role="tab" aria-selected={active} className={active ? "active" : ""} onClick={onClick}>{icon}<span>{label}</span></button>;
}

function EmptyWorkspace({ loading, onCreate }: { loading: boolean; onCreate: () => void }) {
  return <div className="empty-workspace">
    <div className="empty-symbol">{loading ? <LoaderCircle className="spin" /> : <BookOpen />}</div>
    <h2>{loading ? "正在打开工作区" : "从一个知识库开始"}</h2>
    {!loading && <><p>把散落的文档、网页和想法放进同一个可检索的空间。</p><button className="primary-button" onClick={onCreate}><Plus size={17} />新建知识库</button></>}
  </div>;
}

function WorkspaceBar({ kb, onOpenChat, onOpenSearch, onOpenMedias }: { kb: KnowledgeBase; onOpenChat: () => void; onOpenSearch: () => void; onOpenMedias: () => void }) {
  return <section className="workspace-bar" aria-label="知识库操作">
    <button type="button" className="workspace-status" onClick={onOpenMedias}>
      <Files size={18} />
      <span><strong>{kb.media_count}</strong><small>已收录文档</small></span>
    </button>
    <div className="workspace-actions">
      <button type="button" onClick={onOpenChat}><MessageSquareText size={17} />问答</button>
      <button type="button" onClick={onOpenMedias}><Upload size={17} />导入资料</button>
      <button type="button" onClick={onOpenSearch}><Search size={17} />检索片段</button>
    </div>
  </section>;
}

function CreateDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (kb: KnowledgeBase) => void }) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    try { onCreated(await api.createKB(name.trim(), description.trim())); } catch (cause) { setError(messageOf(cause)); setBusy(false); }
  };
  return <div className="dialog-layer" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
    <form className="dialog" role="dialog" aria-modal="true" aria-labelledby="new-kb-title" onSubmit={submit}>
      <div className="dialog-head"><div><span className="eyebrow">新空间</span><h2 id="new-kb-title">新建知识库</h2></div><button type="button" className="icon-button" onClick={onClose} aria-label="关闭"><X size={18} /></button></div>
      <label>名称<input autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：产品研究" /></label>
      <label>描述<textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder="这个知识库收录什么？" rows={3} /></label>
      {error && <p className="field-error">{error}</p>}
      <div className="dialog-actions"><button type="button" className="text-button" onClick={onClose}>取消</button><button className="primary-button" disabled={busy || !name.trim()}>{busy ? <LoaderCircle className="spin" size={17} /> : <Plus size={17} />}创建</button></div>
    </form>
  </div>;
}

function MediasView({ kb, onError, onCountChange, highlightBizID, onReadMedia }: { kb: KnowledgeBase; onError: (value: string) => void; onCountChange: () => void; highlightBizID?: string | null; onReadMedia: ReadFn }) {
  const [medias, setMedias] = useState<Media[]>([]);
  const [url, setURL] = useState("");
  const [busy, setBusy] = useState(false);
  const [uploading, setUploading] = useState("");
  const [dragging, setDragging] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement | null>(null);
  const dragDepth = useRef(0);
  const refresh = useCallback(async () => {
    try { setMedias((await api.listMedias(kb.biz_id)) || []); } catch (cause) { onError(messageOf(cause)); }
  }, [kb.biz_id, onError]);
  useEffect(() => {
    void refresh();
    const timer = window.setInterval(refresh, 3000);
    return () => window.clearInterval(timer);
  }, [refresh]);
  const uploadFiles = async (files: File[]) => {
    if (busy) return;
    const accepted = files.filter((file) => ALLOWED_EXTENSIONS.has(extensionOf(file.name)));
    const skipped = files.length - accepted.length;
    if (!accepted.length) {
      if (skipped) onError(`已跳过 ${skipped} 个不支持的文件（仅支持 ${TYPE_HINT}）`);
      return;
    }
    setBusy(true);
    const failed: string[] = [];
    for (let index = 0; index < accepted.length; index++) {
      setUploading(`正在上传 ${index + 1}/${accepted.length}`);
      try { await api.uploadMedia(kb.biz_id, accepted[index]); }
      catch (cause) { failed.push(`${accepted[index].name}：${messageOf(cause)}`); }
    }
    setUploading("");
    try { await refresh(); await onCountChange(); }
    finally { setBusy(false); }
    if (failed.length) onError(failed.length === 1 ? failed[0] : `${failed.length} 个文件上传失败：${failed.join("；")}`);
    else if (skipped) onError(`已上传 ${accepted.length} 个文件，跳过 ${skipped} 个不支持的文件`);
  };
  const pickFiles = (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? []);
    event.target.value = "";
    if (files.length) void uploadFiles(files);
  };
  const onDragEnter = (event: React.DragEvent) => {
    if (!event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    dragDepth.current += 1;
    setDragging(true);
  };
  const onDragOver = (event: React.DragEvent) => {
    if (event.dataTransfer.types.includes("Files")) event.preventDefault();
  };
  const onDragLeave = (event: React.DragEvent) => {
    if (!event.dataTransfer.types.includes("Files")) return;
    dragDepth.current -= 1;
    if (dragDepth.current <= 0) { dragDepth.current = 0; setDragging(false); }
  };
  const onDrop = async (event: React.DragEvent) => {
    if (!event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    dragDepth.current = 0;
    setDragging(false);
    const files = await collectDroppedFiles(event.dataTransfer);
    if (files.length) await uploadFiles(files);
  };
  const ingest = async (event: FormEvent) => {
    event.preventDefault();
    if (!url.trim()) return;
    setBusy(true);
    try { await api.ingestURL(kb.biz_id, url.trim()); setURL(""); await refresh(); await onCountChange(); } catch (cause) { onError(messageOf(cause)); } finally { setBusy(false); }
  };
  return <div className="medias-view" onDragEnter={onDragEnter} onDragOver={onDragOver} onDragLeave={onDragLeave} onDrop={(event) => void onDrop(event)}>
    {dragging && <div className="drop-overlay"><Upload size={30} /><strong>松开以上传文件或文件夹</strong></div>}
    <div className="action-band">
      <button className="upload-zone" onClick={() => fileInput.current?.click()} disabled={busy}>
        <span className="action-icon"><Upload size={20} /></span><span><strong>上传文件</strong><small>{uploading || `可多选或拖拽上传，支持 ${TYPE_HINT}`}</small></span>
      </button>
      <button className="upload-zone" onClick={() => folderInput.current?.click()} disabled={busy}>
        <span className="action-icon"><FolderOpen size={20} /></span><span><strong>上传文件夹</strong><small>{uploading || "导入整个文件夹，自动收集支持的格式"}</small></span>
      </button>
      <input ref={fileInput} hidden type="file" multiple accept={ACCEPT_ATTRIBUTE} onChange={pickFiles} />
      <input
        hidden
        type="file"
        ref={(element) => { folderInput.current = element; element?.setAttribute("webkitdirectory", ""); }}
        onChange={pickFiles}
      />
      <form className="url-form" onSubmit={ingest}><Link size={18} /><input value={url} onChange={(event) => setURL(event.target.value)} placeholder="粘贴网页链接" aria-label="网页链接" /><button className="icon-button filled" disabled={busy || !url.trim()} aria-label="收录网页"><ArrowUp size={17} /></button></form>
    </div>
    <div className="section-heading"><div><h2>文档</h2><span>{medias.length}</span></div><button className="icon-button" onClick={() => void refresh()} title="刷新" aria-label="刷新文档"><RefreshCw size={16} /></button></div>
    <div className="media-table">
      {medias.map((media) => <MediaRow key={media.biz_id} media={media} refresh={refresh} onError={onError} highlighted={media.biz_id === highlightBizID} onReadMedia={onReadMedia} />)}
      {medias.length === 0 && <InlineEmpty icon={<FileText />} title="这里还很安静" copy="上传文件或收录网页，内容会自动解析并建立索引。" />}
    </div>
  </div>;
}

function MediaRow({ media, refresh, onError, highlighted, onReadMedia }: { media: Media; refresh: () => Promise<void>; onError: (value: string) => void; highlighted?: boolean; onReadMedia: ReadFn }) {
  const active = ["pending", "parsing", "chunking", "indexing", "deleting"].includes(media.status);
  const rowRef = useRef<HTMLElement>(null);
  useEffect(() => { if (highlighted) rowRef.current?.scrollIntoView({ behavior: "smooth", block: "center" }); }, [highlighted]);
  return <article ref={rowRef} className={`media-row${highlighted ? " highlight" : ""}`}>
    <div className={`file-icon type-${media.file_type}`}><FileText size={18} /></div>
    <div className="media-main"><strong>{media.title}</strong><span>{media.file_type.toUpperCase()} · {media.source_type === "url" ? "网页" : "文件"}{media.chunk_count ? ` · ${media.chunk_count} 个片段` : ""}</span>{media.error && <small className="media-error" title={media.error}>{media.error}</small>}</div>
    <div className={`status status-${media.status}`}>{active && media.status !== "deleting" ? <LoaderCircle className="spin" size={13} /> : media.status === "ready" ? <Check size={13} /> : media.status === "failed" ? <AlertCircle size={13} /> : <CircleDashed size={13} />}<span>{statusLabel(media.status)}</span></div>
    <div className="row-actions">
      <button className="icon-button" title="阅读" aria-label={`阅读 ${media.title}`} disabled={media.status !== "ready"} onClick={() => onReadMedia(media.biz_id)}><BookOpen size={16} /></button>
      {media.status === "failed" && <button className="icon-button" title="重试" aria-label={`重试 ${media.title}`} onClick={async () => { try { await api.retryMedia(media.biz_id); await refresh(); } catch (cause) { onError(messageOf(cause)); } }}><RefreshCw size={16} /></button>}
      <button className="icon-button" title="删除" aria-label={`删除 ${media.title}`} disabled={media.status === "deleting"} onClick={async () => { if (!window.confirm(`删除“${media.title}”？`)) return; try { await api.deleteMedia(media.biz_id); await refresh(); } catch (cause) { onError(messageOf(cause)); } }}><Trash2 size={16} /></button>
    </div>
  </article>;
}

function ChatView({ kb, onError, onLocateMedia, onReadMedia }: { kb: KnowledgeBase; onError: (value: string) => void; onLocateMedia: (mediaBizID: string) => void; onReadMedia: ReadFn }) {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversationID, setConversationID] = useState<string | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [query, setQuery] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [models, setModels] = useState<ChatModel[]>([]);
  const [modelBizID, setModelBizID] = useState("");
  const [mode, setMode] = useState<"quick" | "agent">("agent");
  const tokenBuffer = useRef("");
  const tokenTimer = useRef<number | null>(null);
  const refreshConversations = useCallback(async () => {
    try { setConversations((await api.listConversations(kb.biz_id)) || []); } catch (cause) { onError(messageOf(cause)); }
  }, [kb.biz_id, onError]);
  useEffect(() => { setConversationID(null); setMessages([]); setMode("agent"); void refreshConversations(); }, [kb.biz_id, refreshConversations]);
  useEffect(() => { void api.getSettings().then((value) => { setModels(value.chat_models); setModelBizID(value.default_chat_model_biz_id); }).catch((cause) => onError(messageOf(cause))); }, [onError]);
  const openConversation = async (id: string) => {
    setConversationID(id);
    const conversation = conversations.find((item) => item.biz_id === id);
    if (conversation?.mode === "quick" || conversation?.mode === "agent") setMode(conversation.mode);
    try { setMessages((await api.listMessages(id)) || []); } catch (cause) { onError(messageOf(cause)); }
  };
  // 把 thought / tool_call / tool_result 事件合入正在流式生成的助手消息的步骤树。
  const applyStep = (answerID: string, update: (steps: AgentStep[]) => AgentStep[]) => {
    setMessages((current) => current.map((item) => {
      if (item.biz_id !== answerID) return item;
      return { ...item, agent_steps: update(item.agent_steps ? [...item.agent_steps] : []) };
    }));
  };
  const send = async (event: FormEvent) => {
    event.preventDefault();
    const text = query.trim();
    if (!text || streaming) return;
    setQuery(""); setStreaming(true);
    const temporaryID = `temp-${Date.now()}`;
    const answerID = `${temporaryID}-answer`;
    // token 先攒进缓冲区、每 50ms 合入一次，避免每个 token 都触发整棵消息树的重渲染（打字机卡顿的主因）。
    const flushTokens = () => {
      if (tokenTimer.current !== null) { window.clearTimeout(tokenTimer.current); tokenTimer.current = null; }
      const chunk = tokenBuffer.current;
      tokenBuffer.current = "";
      if (!chunk) return;
      setMessages((current) => current.map((item) => item.biz_id === answerID ? { ...item, content: item.content + chunk } : item));
    };
    tokenBuffer.current = "";
    setMessages((current) => [...current, { id: 0, biz_id: temporaryID, conversation_biz_id: conversationID || "", role: "user", content: text, citations: [], created_at: new Date().toISOString() }, { id: 0, biz_id: answerID, conversation_biz_id: conversationID || "", role: "assistant", content: "", citations: [], agent_steps: [], created_at: new Date().toISOString() }]);
    try {
      await streamChat(kb.biz_id, conversationID, modelBizID, mode, text, {
        onToken: (token) => {
          tokenBuffer.current += token;
          if (tokenTimer.current === null) tokenTimer.current = window.setTimeout(flushTokens, 50);
        },
        onThought: (round, content) => applyStep(answerID, (steps) => {
          const step = steps.find((item) => item.iteration === round);
          if (step) step.thought = content; else steps.push({ iteration: round, thought: content, tool_calls: [] });
          return steps;
        }),
        onToolCall: (call) => applyStep(answerID, (steps) => {
          let step = steps.find((item) => item.iteration === call.round);
          if (!step) { step = { iteration: call.round, tool_calls: [] }; steps.push(step); }
          step.tool_calls = [...(step.tool_calls || []), { id: call.id, name: call.name, args: call.args, success: undefined }];
          return steps;
        }),
        onToolResult: (result) => applyStep(answerID, (steps) => {
          const step = steps.find((item) => item.iteration === result.round);
          const call = step?.tool_calls?.find((item) => item.id === result.id);
          if (call) { call.success = result.success; call.output = result.output; call.duration_ms = result.duration_ms; }
          return steps;
        }),
        onReferences: (citations) => setMessages((current) => current.map((item) => item.biz_id === answerID ? { ...item, citations } : item)),
        onDone: (done) => { flushTokens(); setConversationID(done.conversation_biz_id); },
      });
      flushTokens();
      await refreshConversations();
    } catch (cause) { flushTokens(); onError(messageOf(cause)); setMessages((current) => current.filter((item) => item.biz_id !== answerID || item.content || (item.agent_steps?.length ?? 0) > 0)); } finally { setStreaming(false); }
  };
  return <div className="chat-layout">
    <aside className="conversation-list"><div className="conversation-head"><span>对话</span><button className="icon-button" title="新对话" aria-label="新对话" onClick={() => { setConversationID(null); setMessages([]); setMode("agent"); }}><Plus size={16} /></button></div>{conversations.map((conversation) => <button key={conversation.biz_id} className={conversation.biz_id === conversationID ? "active" : ""} onClick={() => void openConversation(conversation.biz_id)}><MessageSquareText size={15} /><span>{conversation.title}</span></button>)}{conversations.length === 0 && <small>暂无历史对话</small>}</aside>
    <div className="chat-stage">
      <div className="messages" aria-live="polite">{messages.length === 0 ? <div className="chat-empty"><img src="/open-ima-mascot.png" alt="" /><h3>向知识库提问</h3><p>Agent 模式会自主规划检索、阅读与联网搜索，逐步给出答案。</p></div> : messages.map((message) => <ChatMessage key={message.biz_id} message={message} streaming={streaming && message === messages[messages.length - 1]} onLocateMedia={onLocateMedia} onReadMedia={onReadMedia} />)}</div>
      <form className="composer" onSubmit={send}><select aria-label="问答模式" value={mode} onChange={(event) => setMode(event.target.value as "quick" | "agent")} disabled={streaming}><option value="agent">Agent</option><option value="quick">快问</option></select><select aria-label="问答模型" value={modelBizID} onChange={(event) => setModelBizID(event.target.value)} disabled={streaming}>{models.map((model) => <option key={model.model_biz_id} value={model.model_biz_id}>{model.name}</option>)}</select><textarea rows={1} value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} placeholder="问问这个知识库…" aria-label="问题" /><button className="send-button" disabled={!query.trim() || streaming || !modelBizID} aria-label="发送问题">{streaming ? <LoaderCircle className="spin" size={18} /> : <ArrowUp size={18} />}</button></form>
    </div>
  </div>;
}

const ChatMessage = memo(function ChatMessage({ message, streaming, onLocateMedia, onReadMedia }: { message: Message; streaming: boolean; onLocateMedia: (mediaBizID: string) => void; onReadMedia: ReadFn }) {
  const [openCitations, setOpenCitations] = useState<number[]>([]);
  const [flashCitation, setFlashCitation] = useState<number | null>(null);
  const citationListRef = useRef<HTMLDivElement>(null);
  const flashTimer = useRef<number | null>(null);
  useEffect(() => () => { if (flashTimer.current) window.clearTimeout(flashTimer.current); }, []);
  const toggleCitation = (index: number) => {
    const opening = !openCitations.includes(index);
    setOpenCitations((current) => opening ? [...current, index] : current.filter((item) => item !== index));
    if (!opening) return;
    window.setTimeout(() => {
      citationListRef.current?.querySelector(`[data-citation-index="${index}"]`)?.scrollIntoView({ behavior: "smooth", block: "nearest" });
    }, 30);
    setFlashCitation(index);
    if (flashTimer.current) window.clearTimeout(flashTimer.current);
    flashTimer.current = window.setTimeout(() => setFlashCitation(null), 1400);
  };
  const components = buildMarkdownComponents(message.citations, toggleCitation);
  const hasSteps = (message.agent_steps?.length ?? 0) > 0;
  return <div className={`message message-${message.role}`}>
    <div className={`message-label${message.role === "assistant" ? " mascot-label" : ""}${message.role === "assistant" && streaming ? " streaming" : ""}`}>{message.role === "user" ? "你" : <img src="/open-ima-mascot.png" alt="IMA" />}</div>
    <div className="message-body">
      {message.role === "assistant" && hasSteps && <StepTree steps={message.agent_steps ?? []} streaming={streaming && !message.content} />}
      {message.role === "assistant" ? streaming && !message.content ? <div className="thinking"><img src="/open-ima-mascot.png" alt="" /><span className="thinking-dots" aria-hidden="true"><i /><i /><i /></span><span className="thinking-text">正在检索知识库并酝酿回答…</span></div> : <div className="markdown-content"><ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>{message.content}</ReactMarkdown>{streaming && <span className="cursor" />}{streaming && <span className="cursor-runner" aria-hidden="true"><img src="/open-ima-mascot.png" alt="" /></span>}</div> : <p>{message.content}</p>}
      {message.citations.length > 0 && <div className="citations" ref={citationListRef}>
        {message.citations.map((citation, index) => {
          const isWeb = citation.source_type === "web";
          return <details key={index} data-citation-index={index} open={openCitations.includes(index)} className={flashCitation === index ? "citation-flash" : ""}>
            <summary onClick={(event) => { event.preventDefault(); toggleCitation(index); }}><span className="cite-no">[{index + 1}]</span>{isWeb ? <Globe size={13} className="cite-source-icon" /> : null}<span className="cite-title">{citation.title}</span>{typeof citation.score === "number" ? <span className="cite-score">相关度 {citation.score.toFixed(3)}</span> : null}</summary>
            <p>{stripTags(citation.snippet)}</p>
            {isWeb ? <div className="cite-actions">{citation.url ? <button type="button" onClick={() => window.open(citation.url, "_blank", "noreferrer")}><ExternalLink size={13} />打开网页</button> : null}</div>
              : <div className="cite-actions">{citation.media_biz_id ? <button type="button" onClick={() => onReadMedia(citation.media_biz_id ?? "", citation.chunk_biz_id)}><BookOpen size={13} />阅读全文</button> : null}{citation.media_biz_id ? <button type="button" onClick={() => onLocateMedia(citation.media_biz_id ?? "")}><FolderOpen size={13} />在文档列表中查看</button> : null}</div>}
          </details>;
        })}
      </div>}
    </div>
  </div>;
});

// StepTree 按轮次渲染 Agent 的思考与工具调用轨迹：思考文本 + 工具卡片（状态、耗时、可展开输出）。
const TOOL_ICONS: Record<string, ElementType> = { search_knowledge: Search, web_search: Globe, read_document: BookOpen, list_documents: Files };
const TOOL_LABELS: Record<string, string> = { search_knowledge: "检索知识库", web_search: "联网搜索", read_document: "阅读文档", list_documents: "列出文档" };

const StepTree = memo(function StepTree({ steps, streaming }: { steps: AgentStep[]; streaming: boolean }) {
  return <div className="agent-steps" aria-label="Agent 推理过程">
    {steps.map((step) => <div key={step.iteration} className="agent-step">
      <div className="agent-step-head"><span className="agent-step-round">第 {step.iteration} 轮</span>{step.truncated ? <span className="agent-step-flag">输出被截断</span> : null}</div>
      {step.thought ? <p className="agent-thought">{step.thought}</p> : null}
      {(step.tool_calls ?? []).map((call) => {
        const Icon = TOOL_ICONS[call.name] ?? Search;
        const state = call.success === undefined ? "running" : call.success ? "ok" : "failed";
        return <details key={call.id} className={`tool-card tool-${state}`}>
          <summary><Icon size={14} /><span className="tool-name">{TOOL_LABELS[call.name] ?? call.name}</span><span className="tool-args">{summarizeToolArgs(call.args)}</span>{state === "running" ? <LoaderCircle size={13} className="spin" /> : state === "ok" ? <Check size={13} /> : <X size={13} />}{typeof call.duration_ms === "number" ? <span className="tool-duration">{call.duration_ms}ms</span> : null}</summary>
          {call.error ? <pre className="tool-output tool-error">{call.error}</pre> : call.output ? <pre className="tool-output">{call.output}</pre> : null}
        </details>;
      })}
    </div>)}
    {streaming ? <div className="agent-step-live"><LoaderCircle size={13} className="spin" /><span>规划下一步…</span></div> : null}
  </div>;
});

// summarizeToolArgs 把工具参数压成一行摘要（优先 query/keywords/media_biz_id 等关键字段）。
function summarizeToolArgs(args: unknown): string {
  if (args == null || typeof args !== "object") return "";
  const record = args as Record<string, unknown>;
  const parts: string[] = [];
  for (const key of ["query", "keywords", "mode", "media_biz_id", "limit"]) {
    const value = record[key];
    if (value === undefined || value === null || value === "") continue;
    parts.push(Array.isArray(value) ? (value as unknown[]).join("、") : String(value));
  }
  const summary = parts.join(" · ");
  return summary.length > 60 ? `${summary.slice(0, 60)}…` : summary;
}

// buildMarkdownComponents 覆盖常见承载文本的节点，把行内的 [n] 引用标记替换成可点击的引用按钮。
function buildMarkdownComponents(citations: Citation[], onRef: (index: number) => void) {
  const wrap = (Tag: ElementType) => function TextWrapped(props: { children?: ReactNode }) {
    return <Tag>{decorateCitations(props.children, citations, onRef)}</Tag>;
  };
  return {
    p: wrap("p"), li: wrap("li"), h1: wrap("h1"), h2: wrap("h2"), h3: wrap("h3"), h4: wrap("h4"),
    strong: wrap("strong"), em: wrap("em"), td: wrap("td"), th: wrap("th"),
    a: (props: { href?: string; children?: ReactNode }) => <a href={props.href} target="_blank" rel="noreferrer">{props.children}</a>,
    pre: (props: { children?: ReactNode }) => <CodeBlock>{props.children}</CodeBlock>,
  };
}

// decorateCitations 递归遍历 React 子节点，把落在引用序号范围内的 [n] 文本替换为按钮；数字超范围的保持原样。
function decorateCitations(children: ReactNode, citations: Citation[], onRef: (index: number) => void): ReactNode {
  return Children.map(children, (child, index) => {
    if (typeof child === "string") return decorateCitationText(child, citations, onRef, index);
    if (isValidElement<{ children?: ReactNode }>(child)) {
      if (child.props.children == null) return child;
      return cloneElement(child, undefined, decorateCitations(child.props.children, citations, onRef));
    }
    return child;
  });
}

function decorateCitationText(text: string, citations: Citation[], onRef: (index: number) => void, key: number): ReactNode {
  const parts = text.split(/\[(\d{1,2})\]/);
  if (parts.length === 1) return text;
  return <Fragment key={key}>
    {parts.map((part, offset) => {
      if (offset % 2 === 1) {
        const number = Number(part);
        if (number >= 1 && number <= citations.length) {
          return <button key={offset} type="button" className="cite-ref" title={`来源：${citations[number - 1].title}`} aria-label={`引用 ${number}：${citations[number - 1].title}`} onClick={(event) => { event.preventDefault(); onRef(number - 1); }}>[{number}]</button>;
        }
        return <Fragment key={offset}>[{part}]</Fragment>;
      }
      return <Fragment key={offset}>{part}</Fragment>;
    })}
  </Fragment>;
}

function CodeBlock({ children }: { children?: ReactNode }) {
  const preRef = useRef<HTMLPreElement>(null);
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(preRef.current?.textContent ?? "");
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch { /* 剪贴板不可用（如非安全上下文）时静默 */ }
  };
  return <div className="code-block">
    <button type="button" className="code-copy" onClick={() => void copy()} aria-label="复制代码">{copied ? <Check size={12} /> : <Copy size={12} />}{copied ? "已复制" : "复制"}</button>
    <pre ref={preRef}>{children}</pre>
  </div>;
}

// MediaReader 是文档阅读抽屉：按分块拼接正文，每个片段带序号，可跳转并高亮被引用的分块。
function MediaReader({ mediaBizID, focusChunkBizID, onClose }: { mediaBizID: string; focusChunkBizID?: string; onClose: () => void }) {
  const [content, setContent] = useState<MediaContent | null>(null);
  const [error, setError] = useState("");
  const [opening, setOpening] = useState(false);
  const [currentIndex, setCurrentIndex] = useState(0);
  const [focusCleared, setFocusCleared] = useState(false);
  const bodyRef = useRef<HTMLDivElement>(null);
  const chunks = content?.chunks ?? [];
  useEffect(() => {
    let cancelled = false;
    api.getMediaContent(mediaBizID)
      .then((result) => { if (!cancelled) setContent(result); })
      .catch((cause) => { if (!cancelled) setError(messageOf(cause)); });
    return () => { cancelled = true; };
  }, [mediaBizID]);
  useEffect(() => { setFocusCleared(false); }, [mediaBizID]);
  useEffect(() => {
    if (!content) return;
    const focusIndex = focusChunkBizID ? content.chunks.findIndex((chunk) => chunk.chunk_biz_id === focusChunkBizID) : -1;
    setCurrentIndex(focusIndex >= 0 ? focusIndex : 0);
    if (focusIndex < 0) return;
    const timer = window.setTimeout(() => {
      bodyRef.current?.querySelector(`[data-chunk-id="${focusChunkBizID}"]`)?.scrollIntoView({ behavior: "smooth", block: "center" });
    }, 50);
    return () => window.clearTimeout(timer);
  }, [content, focusChunkBizID]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  const openLocal = async () => {
    setOpening(true);
    try { await api.openMedia(mediaBizID); } catch (cause) { setError(messageOf(cause)); } finally { setOpening(false); }
  };
  const jumpTo = (index: number) => {
    if (!chunks.length) return;
    const clamped = Math.max(0, Math.min(index, chunks.length - 1));
    setCurrentIndex(clamped);
    bodyRef.current?.querySelector(`[data-chunk-id="${chunks[clamped].chunk_biz_id}"]`)?.scrollIntoView({ behavior: "smooth", block: "start" });
  };
  const focusIndex = !focusCleared && focusChunkBizID ? chunks.findIndex((chunk) => chunk.chunk_biz_id === focusChunkBizID) : -1;
  return <div className="reader-layer" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
    <section className="reader-panel" role="dialog" aria-modal="true" aria-label={content?.title || "文档阅读器"}>
      <header className="reader-head">
        <div className="reader-title"><span className="eyebrow">{content ? `${content.file_type.toUpperCase()} · ${content.chunks.length} 个片段` : "文档"}</span><h2>{content?.title || "正在加载…"}</h2></div>
        <div className="reader-actions">
          {content?.source_type === "file" && <button type="button" className="secondary-button" onClick={() => void openLocal()} disabled={opening}>{opening ? <LoaderCircle className="spin" size={15} /> : <FolderOpen size={15} />}打开本地文件</button>}
          {content?.source_type === "url" && <a className="secondary-button" href={content.source_uri} target="_blank" rel="noreferrer"><ExternalLink size={15} />打开原网页</a>}
          <button type="button" className="icon-button" onClick={onClose} aria-label="关闭阅读器"><X size={17} /></button>
        </div>
      </header>
      {chunks.length > 0 && <div className="reader-nav">
        <button type="button" className="icon-button" onClick={() => jumpTo(currentIndex - 1)} disabled={currentIndex <= 0} aria-label="上一个片段" title="上一个片段"><ChevronLeft size={16} /></button>
        <select aria-label="片段导航" value={currentIndex} onChange={(event) => jumpTo(Number(event.target.value))}>
          {chunks.map((chunk, index) => <option key={chunk.chunk_biz_id} value={index}>{`片段 ${chunk.seq + 1} · ${chunkPreview(chunk.content)}`}</option>)}
        </select>
        <button type="button" className="icon-button" onClick={() => jumpTo(currentIndex + 1)} disabled={currentIndex >= chunks.length - 1} aria-label="下一个片段" title="下一个片段"><ChevronRight size={16} /></button>
        <span className="reader-nav-count">第 {chunks[currentIndex] ? chunks[currentIndex].seq + 1 : "—"} / {chunks.length} 个</span>
        {focusIndex >= 0 && <span className="reader-focus-tag">引用位置 · 片段 {chunks[focusIndex].seq + 1}<button type="button" className="icon-button" onClick={() => setFocusCleared(true)} aria-label="清除引用高亮" title="清除引用高亮"><X size={12} /></button></span>}
      </div>}
      {error && <div className="reader-error"><AlertCircle size={16} /><span>{error}</span></div>}
      <div className="reader-body" ref={bodyRef}>
        {chunks.map((chunk, index) => <div key={chunk.chunk_biz_id} data-chunk-id={chunk.chunk_biz_id} className={`reader-chunk${index === currentIndex ? " reader-current" : ""}${index === focusIndex ? " reader-focus" : ""}`}>
          <span className="chunk-seq" title={`片段 ${chunk.seq + 1}`}>{chunk.seq + 1}</span>
          <p>{chunk.content}</p>
        </div>)}
      </div>
    </section>
  </div>;
}

function chunkPreview(content: string) {
  const compact = stripTags(content).trim().replace(/\s+/g, " ");
  return compact.length > 24 ? `${compact.slice(0, 24)}…` : compact || "（空片段）";
}

function SearchView({ kb, onError }: { kb: KnowledgeBase; onError: (value: string) => void }) {
  const [query, setQuery] = useState("");
  const [mode, setMode] = useState<"hybrid" | "text">("hybrid");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [searched, setSearched] = useState(false);
  const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (!query.trim()) return; setBusy(true);
    try { setResults((await api.search(kb.biz_id, query.trim(), mode)) || []); setSearched(true); } catch (cause) { onError(messageOf(cause)); } finally { setBusy(false); }
  };
  return <div className="search-view"><form className="search-bar" onSubmit={submit}><Search size={19} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索文档内容" aria-label="搜索内容" /><div className="mode-switch"><button type="button" className={mode === "hybrid" ? "active" : ""} onClick={() => setMode("hybrid")}>混合</button><button type="button" className={mode === "text" ? "active" : ""} onClick={() => setMode("text")}>全文</button></div><button className="primary-button" disabled={busy || !query.trim()}>{busy ? <LoaderCircle className="spin" size={16} /> : "搜索"}</button></form><div className="search-results">{results.map((result, index) => <article key={result.chunk_biz_id} className="search-result"><div className="result-rank">{String(index + 1).padStart(2, "0")}</div><div><h3>{result.title}</h3><p><Highlighted text={result.snippet} /></p><small>{typeof result.score === "number" ? `相关度 ${result.score.toFixed(3)}` : ""}</small></div></article>)}{searched && results.length === 0 && <InlineEmpty icon={<Search />} title="没有找到匹配内容" copy="换个关键词，或切换搜索模式再试一次。" />}{!searched && <InlineEmpty icon={<Search />} title="在所有片段中检索" copy="混合搜索兼顾语义和关键词，全文搜索更适合精确短语。" />}</div></div>;
}

function SettingsView({ onError }: { onError: (value: string) => void }) {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [apiKeys, setAPIKeys] = useState<Record<string, string>>({});
  const [visibleKeys, setVisibleKeys] = useState<Record<string, boolean>>({});
  const [anysearchKey, setAnysearchKey] = useState("");
  const [anysearchVisible, setAnysearchVisible] = useState(false);
  const [view, setView] = useState<"models" | "search">("models");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  useEffect(() => { void api.getSettings().then(setSettings).catch((cause) => onError(messageOf(cause))); }, [onError]);
  if (!settings) return <div className="settings-loading"><LoaderCircle className="spin" /><span>正在读取本地配置</span></div>;
  const update = <K extends keyof AppSettings>(key: K, value: AppSettings[K]) => setSettings((current) => current ? { ...current, [key]: value } : current);
  const updateModel = (modelBizID: string, patch: Partial<ChatModel>) => update("chat_models", settings.chat_models.map((model) => model.model_biz_id === modelBizID ? { ...model, ...patch } : model));
  const addModel = () => {
    const modelBizID = crypto.randomUUID();
    update("chat_models", [...settings.chat_models, { model_biz_id: modelBizID, name: "新模型", protocol: "openai", base_url: "https://api.deepseek.com/v1", model: "deepseek-chat", api_key_configured: false }]);
  };
  const removeModel = (modelBizID: string) => {
    if (settings.chat_models.length === 1) return;
    const remaining = settings.chat_models.filter((model) => model.model_biz_id !== modelBizID);
    setSettings({ ...settings, chat_models: remaining, default_chat_model_biz_id: settings.default_chat_model_biz_id === modelBizID ? remaining[0].model_biz_id : settings.default_chat_model_biz_id });
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault(); setBusy(true); setSaved(false);
    try {
      const updated = await api.updateSettings({ ...settings, chat_models: settings.chat_models.map((model) => ({ ...model, api_key: apiKeys[model.model_biz_id] || undefined })), anysearch_api_key: anysearchKey || undefined });
      setSettings(updated); setAPIKeys({}); setAnysearchKey(""); setSaved(true);
      window.setTimeout(() => setSaved(false), 2500);
    } catch (cause) { onError(messageOf(cause)); } finally { setBusy(false); }
  };
  return <div className="settings-shell">
    <div className="settings-nav" role="tablist"><button role="tab" aria-selected={view === "models"} className={view === "models" ? "active" : ""} onClick={() => setView("models")}><Sparkles size={16} />模型配置</button><button role="tab" aria-selected={view === "search"} className={view === "search" ? "active" : ""} onClick={() => setView("search")}><Database size={16} />索引控制台</button></div>
    {view === "search" ? <IndexConsole onError={onError} /> : <form className="settings-view" onSubmit={submit}>
    <section className="settings-section">
      <div className="settings-section-head"><div><span>01</span><h2>问答模型</h2></div><p>每个模型独立保存协议与密钥，问答时可随时选择。</p></div>
      <div className="model-list">{settings.chat_models.map((model, index) => <article className="model-config" key={model.model_biz_id}>
        <div className="model-config-head"><label className="default-model"><input type="radio" name="default-model" checked={settings.default_chat_model_biz_id === model.model_biz_id} onChange={() => update("default_chat_model_biz_id", model.model_biz_id)} /><span>默认</span></label><strong>{String(index + 1).padStart(2, "0")}</strong><button type="button" className="icon-button danger-ghost" onClick={() => removeModel(model.model_biz_id)} disabled={settings.chat_models.length === 1} aria-label={`删除 ${model.name}`}><Trash2 size={15} /></button></div>
        <div className="settings-grid">
          <label className="field"><span>显示名称</span><input value={model.name} onChange={(event) => updateModel(model.model_biz_id, { name: event.target.value })} required /></label>
          <label className="field"><span>API 协议</span><div className="protocol-switch"><button type="button" className={model.protocol === "openai" ? "active" : ""} onClick={() => updateModel(model.model_biz_id, { protocol: "openai" })}>OpenAI 兼容</button><button type="button" className={model.protocol === "anthropic" ? "active" : ""} onClick={() => updateModel(model.model_biz_id, { protocol: "anthropic" })}>Anthropic</button></div></label>
          <label className="field"><span>模型标识</span><input value={model.model} onChange={(event) => updateModel(model.model_biz_id, { model: event.target.value })} placeholder="deepseek-chat" required /></label>
          <label className="field"><span>Base URL</span><input type="url" value={model.base_url} onChange={(event) => updateModel(model.model_biz_id, { base_url: event.target.value })} required /></label>
          <label className="field wide"><span>API Key</span><div className="secret-input"><input type={visibleKeys[model.model_biz_id] ? "text" : "password"} value={apiKeys[model.model_biz_id] || ""} onChange={(event) => setAPIKeys((current) => ({ ...current, [model.model_biz_id]: event.target.value }))} placeholder={model.api_key_configured ? "已配置，留空则保持不变" : "输入 API Key"} /><button type="button" className="icon-button" onClick={() => setVisibleKeys((current) => ({ ...current, [model.model_biz_id]: !current[model.model_biz_id] }))} aria-label={visibleKeys[model.model_biz_id] ? `隐藏 ${model.name} API Key` : `显示 ${model.name} API Key`}>{visibleKeys[model.model_biz_id] ? <EyeOff size={16} /> : <Eye size={16} />}</button></div><small className={model.api_key_configured ? "configured" : ""}>{model.api_key_configured ? "密钥已安全保存在本地" : "尚未配置密钥"}</small></label>
        </div>
      </article>)}</div>
      <button type="button" className="secondary-button add-model" onClick={addModel}><Plus size={16} />添加模型</button>
    </section>
    <section className="settings-section">
      <div className="settings-section-head"><div><span>02</span><h2>本地向量模型</h2></div><p>Meilisearch 直接调用 Ollama，保存后立即更新索引 embedder。</p></div>
      <div className="settings-grid">
        <label className="field wide"><span>Ollama Endpoint</span><input type="url" value={settings.embedder_url} onChange={(event) => update("embedder_url", event.target.value)} required /></label>
        <label className="field"><span>模型</span><input value={settings.embedder_model} onChange={(event) => update("embedder_model", event.target.value)} required /></label>
        <label className="field"><span>向量维度</span><input type="number" min={1} max={65536} value={settings.embedder_dimensions} onChange={(event) => update("embedder_dimensions", Number(event.target.value))} required /></label>
      </div>
      <div className="settings-note"><AlertCircle size={16} /><span>更换向量模型或维度后，需要执行重建索引，已有文档才会使用新模型。</span></div>
    </section>
    <section className="settings-section">
      <div className="settings-section-head"><div><span>03</span><h2>文档分块</h2></div><p>按字符切分文档，超长块按分隔符优先级递归切分。</p></div>
      <div className="settings-grid">
        <label className="field"><span>块大小（字符）</span><input type="number" min={1} max={65536} value={settings.chunk_size} onChange={(event) => update("chunk_size", Number(event.target.value))} required /></label>
        <label className="field"><span>重叠（字符）</span><input type="number" min={0} max={65535} value={settings.chunk_overlap} onChange={(event) => update("chunk_overlap", Number(event.target.value))} required /></label>
        <label className="field wide"><span>分隔符（逗号分隔，\n 换行、\s 空格）</span><input value={encodeSeparators(settings.chunk_separators)} onChange={(event) => update("chunk_separators", decodeSeparators(event.target.value))} placeholder={`\n\n, \n, 。, ?, !, ;, \s`} /></label>
      </div>
      <div className="settings-note"><AlertCircle size={16} /><span>修改后仅对新上传文档生效，存量文档需执行重建索引才会按新规则切分。</span></div>
    </section>
    <section className="settings-section">
      <div className="settings-section-head"><div><span>04</span><h2>联网搜索</h2></div><p>Agent 模式下允许模型调用联网搜索工具，与知识库检索结合作答。</p></div>
      <div className="settings-grid">
        <label className="field wide"><span className="checkbox-field"><input type="checkbox" checked={settings.web_search_enabled} onChange={(event) => update("web_search_enabled", event.target.checked)} />启用联网搜索（仅 Agent 模式生效）</span></label>
        <label className="field"><span>搜索提供方</span><select value={settings.web_search_provider} onChange={(event) => update("web_search_provider", event.target.value as AppSettings["web_search_provider"])} disabled={!settings.web_search_enabled}><option value="anysearch">AnySearch（需 API Key）</option><option value="duckduckgo">DuckDuckGo（免配置）</option><option value="searxng">SearxNG（自建）</option></select></label>
        {settings.web_search_provider === "searxng" ? <label className="field"><span>SearxNG 地址</span><input type="url" value={settings.searxng_base_url} onChange={(event) => update("searxng_base_url", event.target.value)} placeholder="http://127.0.0.1:8080" disabled={!settings.web_search_enabled} required={settings.web_search_enabled} /></label> : null}
        {settings.web_search_provider === "anysearch" ? <label className="field wide"><span>AnySearch API Key</span><div className="secret-input"><input type={anysearchVisible ? "text" : "password"} value={anysearchKey} onChange={(event) => setAnysearchKey(event.target.value)} placeholder={settings.anysearch_api_key_configured ? "已配置，留空则保持不变" : "在 anysearch.com 控制台免费创建"} disabled={!settings.web_search_enabled} /><button type="button" className="icon-button" onClick={() => setAnysearchVisible((visible) => !visible)} aria-label={anysearchVisible ? "隐藏 AnySearch API Key" : "显示 AnySearch API Key"}>{anysearchVisible ? <EyeOff size={16} /> : <Eye size={16} />}</button></div><small className={settings.anysearch_api_key_configured ? "configured" : ""}>{settings.anysearch_api_key_configured ? "密钥已安全保存在本地" : "尚未配置密钥"}</small></label> : null}
        <label className="field"><span>每次结果数</span><input type="number" min={1} max={20} value={settings.web_search_max_results} onChange={(event) => update("web_search_max_results", Number(event.target.value))} disabled={!settings.web_search_enabled} /></label>
      </div>
      <div className="settings-note"><AlertCircle size={16} /><span>推荐 AnySearch：结构化 API、结果稳定，需先在 anysearch.com 免费创建 API Key；DuckDuckGo 免密钥但匿名入口可能对部分出口 IP 长期限流；追求完全自建可用 SearxNG。</span></div>
    </section>
    <div className="settings-actions"><span className={saved ? "save-confirmation visible" : "save-confirmation"}><Check size={15} />配置已生效</span><button className="primary-button" disabled={busy}>{busy ? <LoaderCircle className="spin" size={17} /> : <Save size={17} />}{busy ? "正在应用" : "保存配置"}</button></div>
    </form>}
  </div>;
}

function IndexConsole({ onError }: { onError: (value: string) => void }) {
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const reindex = async () => {
    if (!window.confirm("将重新解析并索引全部文档，期间检索结果可能不完整。确定继续吗？")) return;
    setBusy(true); setNotice("");
    try {
      const result = await api.reindex();
      setNotice(`已加入重建队列：${result.enqueued} 个文档`);
    } catch (cause) { onError(messageOf(cause)); } finally { setBusy(false); }
  };
  return <section className="dashboard-view">
    <div className="reindex-card">
      <div><strong>重建索引</strong><p>全部文档按当前分块与向量设置重新解析、切分并生成向量，适用于修改分块参数或更换向量模型之后。后台任务依次执行，可在文档列表查看进度。</p></div>
      <div className="reindex-actions"><button type="button" className="secondary-button" onClick={() => void reindex()} disabled={busy}>{busy ? <LoaderCircle className="spin" size={16} /> : <RefreshCw size={16} />}{busy ? "正在入队" : "重建索引"}</button>{notice && <span className="save-confirmation visible"><Check size={15} />{notice}</span>}</div>
    </div>
    <div className="dashboard-head"><div><span className="status-dot" /><strong>Meilisearch mini-dashboard</strong><small>127.0.0.1:7700</small></div><a href="http://127.0.0.1:7700/" target="_blank" rel="noreferrer"><ExternalLink size={15} />新窗口打开</a></div>
    <iframe src="http://127.0.0.1:7700/" title="Meilisearch mini-dashboard" />
  </section>;
}

function Highlighted({ text }: { text: string }) {
  const parts = text.split(/(<\/?em>)/i); let highlighted = false;
  return <>{parts.map((part, index) => { if (/^<em>$/i.test(part)) { highlighted = true; return null; } if (/^<\/em>$/i.test(part)) { highlighted = false; return null; } return highlighted ? <mark key={index}>{part}</mark> : part; })}</>;
}

function InlineEmpty({ icon, title, copy }: { icon: ReactNode; title: string; copy: string }) { return <div className="inline-empty"><span>{icon}</span><h3>{title}</h3><p>{copy}</p></div>; }
function encodeSeparators(separators: string[]) { return separators.map((separator) => separator.replace(/\\/g, "\\\\").replace(/\n/g, "\\n").replace(/\t/g, "\\t").replace(/ /g, "\\s")).join(", "); }
function decodeSeparators(raw: string) { return raw.split(",").map((part) => part.trim()).filter((part) => part !== "").map((part) => part.replace(/\\n/g, "\n").replace(/\\t/g, "\t").replace(/\\s/g, " ").replace(/\\\\/g, "\\")); }
function messageOf(cause: unknown) { return cause instanceof Error ? cause.message : "操作失败，请稍后重试"; }
function stripTags(value: string) { return value.replace(/<[^>]*>/g, ""); }
function statusLabel(status: Media["status"]) { return ({ pending: "等待中", parsing: "解析中", chunking: "分块中", indexing: "索引中", ready: "可检索", failed: "失败", deleting: "删除中" })[status]; }
