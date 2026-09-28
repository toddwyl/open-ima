import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { api } from "./api";

// jsdom 未实现 scrollIntoView，组件在引用定位时会调用它。
Element.prototype.scrollIntoView = vi.fn();

vi.mock("./api", () => ({
  api: {
    listKBs: vi.fn().mockResolvedValue([{ biz_id: "kb1", name: "产品研究", description: "AI 与芯片", media_count: 1, created_at: "2026-09-27" }]),
    listMedias: vi.fn().mockResolvedValue([{ biz_id: "d1", kb_biz_id: "kb1", title: "产业笔记", source_type: "file", source_uri: "key", file_type: "md", status: "ready", error: "", chunk_count: 3, created_at: "2026-09-27" }]),
    listConversations: vi.fn().mockResolvedValue([]),
    listMessages: vi.fn().mockResolvedValue([]),
    search: vi.fn().mockResolvedValue([]),
    getSettings: vi.fn().mockResolvedValue({ chat_models: [{ model_biz_id: "kimi-id", name: "Kimi", protocol: "anthropic", base_url: "https://api.kimi.com/coding", model: "kimi-for-coding", api_key_configured: true }], default_chat_model_biz_id: "kimi-id", embedder_url: "http://127.0.0.1:11434/api/embeddings", embedder_model: "bge-m3", embedder_dimensions: 1024, chunk_size: 512, chunk_overlap: 80, chunk_separators: ["\n\n", "\n", "。", "?", "!", ";", " "], web_search_enabled: true, web_search_max_results: 5, anysearch_api_key_configured: false }),
    updateSettings: vi.fn(),
    createKB: vi.fn(), deleteKB: vi.fn(), uploadMedia: vi.fn(), ingestURL: vi.fn(), retryMedia: vi.fn(), deleteMedia: vi.fn(),
    getMediaContent: vi.fn().mockResolvedValue({ media_biz_id: "d1", title: "产业笔记", source_type: "file", source_uri: "key", file_type: "md", chunks: [{ chunk_biz_id: "chunk-1", seq: 0, content: "全文第一段" }] }),
    openMedia: vi.fn().mockResolvedValue(undefined),
  },
  streamChat: vi.fn(),
}));

afterEach(() => { cleanup(); vi.clearAllMocks(); });

describe("App", () => {
  it("loads the selected knowledge base and documents", async () => {
    render(<App />);
    expect(await screen.findByRole("heading", { name: "产品研究" })).toBeInTheDocument();
    expect(await screen.findByText("向知识库提问")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("tab", { name: "文档" }));
    expect(await screen.findByText("产业笔记")).toBeInTheDocument();
    expect(screen.getByText("3 个片段", { exact: false })).toBeInTheDocument();
  });

  it("uploads multiple selected files in one go", async () => {
    render(<App />);
    await userEvent.click(await screen.findByRole("tab", { name: "文档" }));
    const input = document.querySelector<HTMLInputElement>('input[type="file"][accept]');
    expect(input).not.toBeNull();
    expect(input).toHaveAttribute("multiple");
    const folderInput = document.querySelector<HTMLInputElement>('input[type="file"]:not([accept])');
    expect(folderInput).toHaveAttribute("webkitdirectory");
    const first = new File(["# A"], "a.md", { type: "text/markdown" });
    const second = new File(["# B"], "b.md", { type: "text/markdown" });
    await userEvent.upload(input!, [first, second]);
    await waitFor(() => expect(vi.mocked(api.uploadMedia)).toHaveBeenCalledTimes(2));
    expect(vi.mocked(api.uploadMedia)).toHaveBeenNthCalledWith(1, "kb1", first);
    expect(vi.mocked(api.uploadMedia)).toHaveBeenNthCalledWith(2, "kb1", second);
  });

  it("skips unsupported files when batch uploading", async () => {
    render(<App />);
    await userEvent.click(await screen.findByRole("tab", { name: "文档" }));
    const input = document.querySelector<HTMLInputElement>('input[type="file"][accept]');
    await userEvent.upload(input!, [
      new File(["# A"], "a.md", { type: "text/markdown" }),
      new File(["MZ"], "virus.exe", { type: "application/octet-stream" }),
    ], { applyAccept: false });
    await waitFor(() => expect(vi.mocked(api.uploadMedia)).toHaveBeenCalledTimes(1));
    expect(vi.mocked(api.uploadMedia).mock.calls[0][1].name).toBe("a.md");
    expect(await screen.findByText(/跳过 1 个不支持的文件/)).toBeInTheDocument();
  });

  it("uploads files dropped onto the documents view", async () => {
    render(<App />);
    await userEvent.click(await screen.findByRole("tab", { name: "文档" }));
    const view = document.querySelector(".medias-view");
    expect(view).not.toBeNull();
    const dropped = new File(["# C"], "c.md", { type: "text/markdown" });
    fireEvent.dragEnter(view!, { dataTransfer: { types: ["Files"] } });
    expect(await screen.findByText("松开以上传文件或文件夹")).toBeInTheDocument();
    fireEvent.drop(view!, { dataTransfer: { types: ["Files"], files: [dropped], items: [] } });
    await waitFor(() => expect(vi.mocked(api.uploadMedia)).toHaveBeenCalledTimes(1));
    expect(vi.mocked(api.uploadMedia).mock.calls[0][1].name).toBe("c.md");
  });

  it("switches between chat and search workspaces", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "产品研究" });
    expect(await screen.findByText("向知识库提问")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("tab", { name: "搜索" }));
    await waitFor(() => expect(screen.getByPlaceholderText("搜索文档内容")).toBeInTheDocument());
  });

  it("renders assistant Markdown instead of showing syntax markers", async () => {
    vi.mocked(api.listConversations).mockResolvedValue([{ id: 1, biz_id: "conversation-1", kb_biz_id: "kb1", title: "研究摘要", created_at: "2026-09-27" }]);
    vi.mocked(api.listMessages).mockResolvedValue([{ id: 1, biz_id: "message-1", conversation_biz_id: "conversation-1", role: "assistant", content: "**投资配置主线**\n\n- 化工\n- 电力设备", citations: [], created_at: "2026-09-27" }]);
    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: "研究摘要" }));
    expect(await screen.findByText("投资配置主线")).toBeInTheDocument();
    expect(screen.getByText("投资配置主线").tagName).toBe("STRONG");
    expect(screen.queryByText("**投资配置主线**")).not.toBeInTheDocument();
    expect(screen.getByRole("list")).toBeInTheDocument();
  });

  it("turns inline citation markers into buttons that reveal the source", async () => {
    vi.mocked(api.listConversations).mockResolvedValue([{ id: 1, biz_id: "conversation-1", kb_biz_id: "kb1", title: "油运研究", created_at: "2026-09-27" }]);
    vi.mocked(api.listMessages).mockResolvedValue([{ id: 1, biz_id: "message-1", conversation_biz_id: "conversation-1", role: "assistant", content: "油运处于高景气阶段[1]。", citations: [{ media_biz_id: "d1", title: "产业笔记", chunk_biz_id: "chunk-1", snippet: "关键证据 <em>片段</em>", score: 0.032 }], created_at: "2026-09-27" }]);
    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: "油运研究" }));
    const ref = await screen.findByRole("button", { name: "引用 1：产业笔记" });
    expect(ref).toHaveTextContent("[1]");
    const details = document.querySelector("details")!;
    expect(details).not.toHaveAttribute("open");
    await userEvent.click(ref);
    expect(details).toHaveAttribute("open");
    expect(screen.getByText("关键证据 片段")).toBeInTheDocument();
  });

  it("locates the cited document from the citation card", async () => {
    vi.mocked(api.listConversations).mockResolvedValue([{ id: 1, biz_id: "conversation-1", kb_biz_id: "kb1", title: "油运研究", created_at: "2026-09-27" }]);
    vi.mocked(api.listMessages).mockResolvedValue([{ id: 1, biz_id: "message-1", conversation_biz_id: "conversation-1", role: "assistant", content: "油运处于高景气阶段[1]。", citations: [{ media_biz_id: "d1", title: "产业笔记", chunk_biz_id: "chunk-1", snippet: "关键证据", score: 0.032 }], created_at: "2026-09-27" }]);
    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: "油运研究" }));
    await userEvent.click(await screen.findByRole("button", { name: "引用 1：产业笔记" }));
    await userEvent.click(screen.getByRole("button", { name: "在文档列表中查看" }));
    expect(await screen.findByRole("tab", { name: "文档" })).toHaveAttribute("aria-selected", "true");
    const row = document.querySelector(".media-row.highlight");
    expect(row).not.toBeNull();
    expect(row).toHaveTextContent("产业笔记");
  });

  it("renders agent step tree with tool calls above the answer", async () => {
    vi.mocked(api.listConversations).mockResolvedValue([{ id: 1, biz_id: "conversation-3", kb_biz_id: "kb1", title: "Agent 问答", created_at: "2026-09-27" }]);
    vi.mocked(api.listMessages).mockResolvedValue([{ id: 1, biz_id: "message-3", conversation_biz_id: "conversation-3", role: "assistant", content: "兰花代号是 ORCHID-7429[1]。", citations: [{ media_biz_id: "d1", title: "产业笔记", chunk_biz_id: "chunk-1", snippet: "代号 ORCHID-7429", score: 0.9 }], agent_steps: [
      { iteration: 1, thought: "先在知识库里找兰花代号", tool_calls: [{ id: "call_1", name: "search_knowledge", args: { query: "兰花 代号" }, success: true, output: "c1 产业笔记: 代号 ORCHID-7429", duration_ms: 42 }] },
      { iteration: 2, thought: "信息足够，组织答案", tool_calls: [] },
    ], created_at: "2026-09-27" }]);
    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: "Agent 问答" }));
    expect(await screen.findByText("第 1 轮")).toBeInTheDocument();
    expect(screen.getByText("先在知识库里找兰花代号")).toBeInTheDocument();
    expect(screen.getByText("检索知识库")).toBeInTheDocument();
    expect(screen.getByText("42ms")).toBeInTheDocument();
    expect(screen.getByText(/兰花代号是 ORCHID-7429/)).toBeInTheDocument();
  });

  it("renders web citations with an open-link action and no reader actions", async () => {
    vi.mocked(api.listConversations).mockResolvedValue([{ id: 1, biz_id: "conversation-4", kb_biz_id: "kb1", title: "联网问答", created_at: "2026-09-27" }]);
    vi.mocked(api.listMessages).mockResolvedValue([{ id: 1, biz_id: "message-4", conversation_biz_id: "conversation-4", role: "assistant", content: "最新消息[1]。", citations: [{ source_type: "web", title: "示例新闻", url: "https://example.com/news", snippet: "网页摘要" }], created_at: "2026-09-27" }]);
    const openSpy = vi.fn();
    window.open = openSpy;
    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: "联网问答" }));
    await userEvent.click(await screen.findByRole("button", { name: "引用 1：示例新闻" }));
    expect(screen.getByText("网页摘要")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "阅读全文" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "打开网页" }));
    expect(openSpy).toHaveBeenCalledWith("https://example.com/news", "_blank", "noreferrer");
  });

  it("renders fenced code blocks with a working copy button", async () => {
    vi.mocked(api.listConversations).mockResolvedValue([{ id: 1, biz_id: "conversation-2", kb_biz_id: "kb1", title: "代码示例", created_at: "2026-09-27" }]);
    vi.mocked(api.listMessages).mockResolvedValue([{ id: 1, biz_id: "message-2", conversation_biz_id: "conversation-2", role: "assistant", content: "```go\nfmt.Println(\"hi\")\n```", citations: [], created_at: "2026-09-27" }]);
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: "代码示例" }));
    const code = await screen.findByText(/fmt.Println/);
    expect(code.closest(".code-block")?.querySelector("pre")).not.toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "复制代码" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(expect.stringContaining("fmt.Println(\"hi\")")));
  });

  it("opens the document reader from a citation and opens the local file", async () => {
    vi.mocked(api.listConversations).mockResolvedValue([{ id: 1, biz_id: "conversation-1", kb_biz_id: "kb1", title: "油运研究", created_at: "2026-09-27" }]);
    vi.mocked(api.listMessages).mockResolvedValue([{ id: 1, biz_id: "message-1", conversation_biz_id: "conversation-1", role: "assistant", content: "油运处于高景气阶段[1]。", citations: [{ media_biz_id: "d1", title: "产业笔记", chunk_biz_id: "chunk-1", snippet: "关键证据", score: 0.032 }], created_at: "2026-09-27" }]);
    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: "油运研究" }));
    await userEvent.click(await screen.findByRole("button", { name: "引用 1：产业笔记" }));
    await userEvent.click(screen.getByRole("button", { name: "阅读全文" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("全文第一段");
    expect(vi.mocked(api.getMediaContent)).toHaveBeenCalledWith("d1");
    expect(document.querySelector(".reader-body .reader-focus")).toHaveTextContent("全文第一段");
    expect(screen.getByText("引用位置 · 片段 1")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "打开本地文件" }));
    await waitFor(() => expect(vi.mocked(api.openMedia)).toHaveBeenCalledWith("d1"));
    await userEvent.click(screen.getByRole("button", { name: "关闭阅读器" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("numbers every chunk and navigates between them in the reader", async () => {
    vi.mocked(api.getMediaContent).mockResolvedValue({ media_biz_id: "d1", title: "产业笔记", source_type: "file", source_uri: "key", file_type: "md", chunks: [
      { chunk_biz_id: "chunk-1", seq: 0, content: "全文第一段" },
      { chunk_biz_id: "chunk-2", seq: 1, content: "中间证据" },
      { chunk_biz_id: "chunk-3", seq: 2, content: "结尾结论" },
    ] });
    render(<App />);
    await userEvent.click(await screen.findByRole("tab", { name: "文档" }));
    await userEvent.click(screen.getByRole("button", { name: "阅读 产业笔记" }));
    expect(await screen.findByText("中间证据")).toBeInTheDocument();
    // 每个片段都带序号徽标
    expect(document.querySelectorAll(".reader-body .chunk-seq")).toHaveLength(3);
    // 下拉导航带序号与内容预览
    const nav = screen.getByRole("combobox", { name: "片段导航" });
    expect(screen.getByRole("option", { name: "片段 2 · 中间证据" })).toBeInTheDocument();
    await userEvent.selectOptions(nav, "1");
    expect(screen.getByText("第 2 / 3 个")).toBeInTheDocument();
    expect(document.querySelector(".reader-chunk.reader-current")).toHaveTextContent("中间证据");
    // 上一个 / 下一个按钮
    await userEvent.click(screen.getByRole("button", { name: "下一个片段" }));
    expect(screen.getByText("第 3 / 3 个")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "上一个片段" }));
    expect(screen.getByText("第 2 / 3 个")).toBeInTheDocument();
  });

  it("opens the local settings center", async () => {
    render(<App />);
    await userEvent.click(screen.getByRole("button", { name: "配置中心" }));
    expect(await screen.findByRole("heading", { name: "配置中心" })).toBeInTheDocument();
    expect(screen.getByDisplayValue("kimi-for-coding")).toBeInTheDocument();
    expect(screen.getByText("密钥已安全保存在本地")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Anthropic" })).toHaveClass("active");
    await userEvent.click(screen.getByRole("tab", { name: "索引控制台" }));
    expect(screen.getByTitle("Meilisearch mini-dashboard")).toHaveAttribute("src", "http://127.0.0.1:7700/");
  });
});
