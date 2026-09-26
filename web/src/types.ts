export type KnowledgeBase = {
  id: string;
  name: string;
  description: string;
  doc_count: number;
  created_at: string;
};

export type Document = {
  id: string;
  kb_id: string;
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
  document_id: string;
  title: string;
  chunk_id: string;
  snippet: string;
  score: number;
};

export type Conversation = { id: string; kb_id: string; title: string; created_at: string };
export type Message = {
  id: string;
  conversation_id: string;
  role: "user" | "assistant";
  content: string;
  citations: Citation[];
  created_at: string;
};

export type SearchResult = Citation;
