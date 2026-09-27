import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { api } from "./api";

vi.mock("./api", () => ({
  api: {
    listKBs: vi.fn().mockResolvedValue([{ biz_id: "kb1", name: "产品研究", description: "AI 与芯片", doc_count: 1, created_at: "2026-09-27" }]),
    listDocuments: vi.fn().mockResolvedValue([{ biz_id: "d1", kb_biz_id: "kb1", title: "产业笔记", source_type: "file", source_uri: "key", file_type: "md", status: "ready", error: "", chunk_count: 3, created_at: "2026-09-27" }]),
    listConversations: vi.fn().mockResolvedValue([]),
    listMessages: vi.fn().mockResolvedValue([]),
    search: vi.fn().mockResolvedValue([]),
    getSettings: vi.fn().mockResolvedValue({ chat_models: [{ model_biz_id: "kimi-id", name: "Kimi", protocol: "anthropic", base_url: "https://api.kimi.com/coding", model: "kimi-for-coding", api_key_configured: true }], default_chat_model_biz_id: "kimi-id", embedder_url: "http://127.0.0.1:11434/api/embeddings", embedder_model: "bge-m3", embedder_dimensions: 1024 }),
    updateSettings: vi.fn(),
    createKB: vi.fn(), deleteKB: vi.fn(), uploadDocument: vi.fn(), ingestURL: vi.fn(), retryDocument: vi.fn(), deleteDocument: vi.fn(),
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
    await waitFor(() => expect(vi.mocked(api.uploadDocument)).toHaveBeenCalledTimes(2));
    expect(vi.mocked(api.uploadDocument)).toHaveBeenNthCalledWith(1, "kb1", first);
    expect(vi.mocked(api.uploadDocument)).toHaveBeenNthCalledWith(2, "kb1", second);
  });

  it("skips unsupported files when batch uploading", async () => {
    render(<App />);
    await userEvent.click(await screen.findByRole("tab", { name: "文档" }));
    const input = document.querySelector<HTMLInputElement>('input[type="file"][accept]');
    await userEvent.upload(input!, [
      new File(["# A"], "a.md", { type: "text/markdown" }),
      new File(["MZ"], "virus.exe", { type: "application/octet-stream" }),
    ], { applyAccept: false });
    await waitFor(() => expect(vi.mocked(api.uploadDocument)).toHaveBeenCalledTimes(1));
    expect(vi.mocked(api.uploadDocument).mock.calls[0][1].name).toBe("a.md");
    expect(await screen.findByText(/跳过 1 个不支持的文件/)).toBeInTheDocument();
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
