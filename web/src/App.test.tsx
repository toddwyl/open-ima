import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";

vi.mock("./api", () => ({
  api: {
    listKBs: vi.fn().mockResolvedValue([{ biz_id: "kb1", name: "产品研究", description: "AI 与芯片", doc_count: 1, created_at: "2026-09-27" }]),
    listDocuments: vi.fn().mockResolvedValue([{ biz_id: "d1", kb_biz_id: "kb1", title: "产业笔记", source_type: "file", source_uri: "key", file_type: "md", status: "ready", error: "", chunk_count: 3, created_at: "2026-09-27" }]),
    listConversations: vi.fn().mockResolvedValue([]),
    listMessages: vi.fn().mockResolvedValue([]),
    search: vi.fn().mockResolvedValue([]),
    getSettings: vi.fn().mockResolvedValue({ llm_protocol: "openai", llm_base_url: "https://api.kimi.com/coding/v1", llm_model: "kimi-for-coding", api_key_configured: true, embedder_url: "http://127.0.0.1:11434/api/embeddings", embedder_model: "bge-m3", embedder_dimensions: 1024 }),
    updateSettings: vi.fn(),
    createKB: vi.fn(), deleteKB: vi.fn(), uploadDocument: vi.fn(), ingestURL: vi.fn(), retryDocument: vi.fn(), deleteDocument: vi.fn(),
  },
  streamChat: vi.fn(),
}));

afterEach(() => cleanup());

describe("App", () => {
  it("loads the selected knowledge base and documents", async () => {
    render(<App />);
    expect(await screen.findByRole("heading", { name: "产品研究" })).toBeInTheDocument();
    expect(await screen.findByText("产业笔记")).toBeInTheDocument();
    expect(screen.getByText("3 个片段", { exact: false })).toBeInTheDocument();
  });

  it("switches between chat and search workspaces", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "产品研究" });
    await userEvent.click(screen.getByRole("tab", { name: "问答" }));
    expect(await screen.findByText("向知识库提问")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("tab", { name: "搜索" }));
    await waitFor(() => expect(screen.getByPlaceholderText("搜索文档内容")).toBeInTheDocument());
  });

  it("opens the local settings center", async () => {
    render(<App />);
    await userEvent.click(screen.getByRole("button", { name: "配置中心" }));
    expect(await screen.findByRole("heading", { name: "配置中心" })).toBeInTheDocument();
    expect(screen.getByDisplayValue("kimi-for-coding")).toBeInTheDocument();
    expect(screen.getByText("密钥已安全保存在本地")).toBeInTheDocument();
  });
});
