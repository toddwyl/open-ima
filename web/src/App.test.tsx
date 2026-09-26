import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";

vi.mock("./api", () => ({
  api: {
    listKBs: vi.fn().mockResolvedValue([{ id: "kb1", name: "产品研究", description: "AI 与芯片", doc_count: 1, created_at: "2026-09-27" }]),
    listDocuments: vi.fn().mockResolvedValue([{ id: "d1", kb_id: "kb1", title: "产业笔记", source_type: "file", source_uri: "key", file_type: "md", status: "ready", error: "", chunk_count: 3, created_at: "2026-09-27" }]),
    listConversations: vi.fn().mockResolvedValue([]),
    listMessages: vi.fn().mockResolvedValue([]),
    search: vi.fn().mockResolvedValue([]),
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
});
