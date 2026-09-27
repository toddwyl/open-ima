export type KnowledgeBase = {
  id: number;
  biz_id: string;
  name: string;
  description: string;
  media_count: number;
  created_at: string;
};

export type Media = {
  id: number;
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
  source_type?: "kb_chunk" | "web";
  media_biz_id?: string;
  title: string;
  chunk_biz_id?: string;
  url?: string;
  snippet: string;
  score?: number;
};

export type AgentToolCall = {
  id: string;
  name: string;
  args?: unknown;
  success?: boolean; // undefined 表示执行中
  output?: string;
  error?: string;
  duration_ms?: number;
};

export type AgentStep = {
  iteration: number;
  thought?: string;
  tool_calls?: AgentToolCall[];
  truncated?: boolean;
  timestamp?: string;
};

export type Conversation = { id: number; biz_id: string; kb_biz_id: string; title: string; mode?: "quick" | "agent"; created_at: string };
export type Message = {
  id: number;
  biz_id: string;
  conversation_biz_id: string;
  role: "user" | "assistant";
  content: string;
  citations: Citation[];
  agent_steps?: AgentStep[];
  created_at: string;
};

export type SearchResult = Citation;

export type MediaChunk = {
  chunk_biz_id: string;
  seq: number;
  content: string;
};

export type MediaContent = {
  media_biz_id: string;
  title: string;
  source_type: "file" | "url";
  source_uri: string;
  file_type: string;
  chunks: MediaChunk[];
};

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
  chunk_size: number;
  chunk_overlap: number;
  chunk_separators: string[];
  web_search_enabled: boolean;
  web_search_provider: "duckduckgo" | "searxng";
  searxng_base_url: string;
  web_search_max_results: number;
};
