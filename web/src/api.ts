import type { AppSettings, Citation, Conversation, Document, KnowledgeBase, Message, SearchResult } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(body.error || `Request failed (${response.status})`);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

const json = (method: string, body?: unknown): RequestInit => ({
  method,
  headers: body === undefined ? undefined : { "Content-Type": "application/json" },
  body: body === undefined ? undefined : JSON.stringify(body),
});

export const api = {
  listKBs: () => request<KnowledgeBase[]>("/api/kbs"),
  createKB: (name: string, description: string) => request<KnowledgeBase>("/api/kbs", json("POST", { name, description })),
  deleteKB: (id: string) => request<void>(`/api/kbs/${id}`, json("DELETE")),
  listDocuments: (kbID: string) => request<Document[]>(`/api/kbs/${kbID}/documents`),
  uploadDocument: (kbID: string, file: File) => {
    const body = new FormData();
    body.append("file", file);
    return request<{ document_biz_id: string; duplicate: boolean }>(`/api/kbs/${kbID}/documents`, { method: "POST", body });
  },
  ingestURL: (kbID: string, url: string) => request<{ document_biz_id: string; duplicate: boolean }>(`/api/kbs/${kbID}/documents:url`, json("POST", { url })),
  retryDocument: (id: string) => request<void>(`/api/documents/${id}/retry`, json("POST")),
  deleteDocument: (id: string) => request<void>(`/api/documents/${id}`, json("DELETE")),
  search: (kbID: string, query: string, mode: "hybrid" | "text") => request<SearchResult[]>(`/api/kbs/${kbID}/search?q=${encodeURIComponent(query)}&mode=${mode}`),
  listConversations: (kbID: string) => request<Conversation[]>(`/api/kbs/${kbID}/conversations`),
  listMessages: (id: string) => request<Message[]>(`/api/conversations/${id}/messages`),
  getSettings: () => request<AppSettings>("/api/settings"),
  updateSettings: (settings: AppSettings) => request<AppSettings>("/api/settings", json("PUT", settings)),
};

type ChatCallbacks = {
  onToken: (token: string) => void;
  onCitations: (citations: Citation[]) => void;
  onDone: (conversationID: string) => void;
};

export async function streamChat(kbID: string, conversationID: string | null, modelBizID: string, query: string, callbacks: ChatCallbacks) {
  const response = await fetch(`/api/kbs/${kbID}/chat`, json("POST", { conversation_biz_id: conversationID || undefined, model_biz_id: modelBizID, query }));
  if (!response.ok || !response.body) throw new Error(`Chat failed (${response.status})`);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value, { stream: !done });
    const frames = buffer.split("\n\n");
    buffer = frames.pop() || "";
    for (const frame of frames) parseFrame(frame, callbacks);
    if (done) break;
  }
  if (buffer.trim()) parseFrame(buffer, callbacks);
}

function parseFrame(frame: string, callbacks: ChatCallbacks) {
  let event = "message";
  let data = "";
  for (const line of frame.split("\n")) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    if (line.startsWith("data:")) data += line.slice(5).trim();
  }
  if (!data) return;
  const payload = JSON.parse(data);
  if (event === "token") callbacks.onToken(payload.token);
  if (event === "citations") callbacks.onCitations(payload);
  if (event === "done") callbacks.onDone(payload.conversation_biz_id);
  if (event === "error") throw new Error(payload.error || "Chat stream failed");
}
