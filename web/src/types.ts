export type KnowledgeBase = {
  biz_id: string;
  name: string;
  description: string;
  doc_count: number;
  created_at: string;
};

export type Document = {
  biz_id: string;
  kb_biz_id: string;
  title: string;
  source_type: "file" | "url";
  source_uri: string;
  file_type: string;
  status: "pending" | "parsing" | "chunking" | "indexing" | "ready" | "failed" | "deleting";
  error: string;
  chunk_count: number;
  created_at: string;
};

export type Citation = {
  document_biz_id: string;
  title: string;
  chunk_biz_id: string;
  snippet: string;
  score: number;
};

export type Conversation = { biz_id: string; kb_biz_id: string; title: string; created_at: string };
export type Message = {
  biz_id: string;
  conversation_biz_id: string;
  role: "user" | "assistant";
  content: string;
  citations: Citation[];
  created_at: string;
};

export type SearchResult = Citation;

export type ChatModel = {
  model_biz_id: string;
  name: string;
  protocol: "openai" | "anthropic";
  base_url: string;
  model: string;
  api_key?: string;
  api_key_configured: boolean;
  clear_api_key?: boolean;
};

export type AppSettings = {
  chat_models: ChatModel[];
  default_chat_model_biz_id: string;
  embedder_url: string;
  embedder_model: string;
  embedder_dimensions: number;
};
