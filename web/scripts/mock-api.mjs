import http from "node:http";

const kb = { id: "kb-demo", name: "产品与研究", description: "团队资料、行业观察与决策记录", media_count: 3, created_at: new Date().toISOString() };
let settings = {
  llm_protocol: "openai",
  llm_base_url: "https://api.kimi.com/coding/v1",
  llm_model: "kimi-for-coding",
  api_key_configured: true,
  embedder_url: "http://127.0.0.1:11434/api/embeddings",
  embedder_model: "bge-m3",
  embedder_dimensions: 1024,
  chunk_size: 512,
  chunk_overlap: 80,
  chunk_separators: ["\\n\\n", "\\n", "。", "?", "!", ";", " "],
};

app.post("/api/reindex", (req, res) => send(res, 202, { enqueued: medias.filter((media) => media.status !== "deleting").length }));
const medias = [
  { id: "doc-1", kb_id: kb.id, title: "AI Agent 产品观察", source_type: "file", source_uri: "a", file_type: "md", status: "ready", error: "", chunk_count: 12, created_at: new Date().toISOString() },
  { id: "doc-2", kb_id: kb.id, title: "半导体供应链报告", source_type: "file", source_uri: "b", file_type: "pdf", status: "indexing", error: "", chunk_count: 0, created_at: new Date().toISOString() },
  { id: "doc-3", kb_id: kb.id, title: "损坏的会议纪要", source_type: "file", source_uri: "c", file_type: "docx", status: "failed", error: "文档结构损坏，无法读取正文", chunk_count: 0, created_at: new Date().toISOString() },
];

function send(response, status, value) {
  response.writeHead(status, { "Content-Type": "application/json; charset=utf-8", "Access-Control-Allow-Origin": "*" });
  response.end(value === undefined ? "" : JSON.stringify(value));
}

http.createServer((request, response) => {
  const url = new URL(request.url, "http://localhost");
  if (request.method === "GET" && url.pathname === "/api/kbs") return send(response, 200, [kb]);
  if (request.method === "GET" && url.pathname === "/api/settings") return send(response, 200, settings);
  if (request.method === "PUT" && url.pathname === "/api/settings") {
    let body = "";
    request.on("data", (chunk) => { body += chunk; });
    request.on("end", () => {
      settings = { ...settings, ...JSON.parse(body || "{}"), api_key_configured: true };
      delete settings.llm_api_key;
      delete settings.clear_api_key;
      send(response, 200, settings);
    });
    return;
  }
  if (request.method === "GET" && url.pathname === `/api/kbs/${kb.id}/medias`) return send(response, 200, medias);
  if (request.method === "GET" && url.pathname === `/api/kbs/${kb.id}/conversations`) return send(response, 200, []);
  if (request.method === "GET" && url.pathname === `/api/kbs/${kb.id}/search`) return send(response, 200, [
    { chunk_id: "chunk-1", media_biz_id: "doc-1", title: "AI Agent 产品观察", snippet: "Agent 的核心价值是把<em>任务规划</em>与工具执行连接起来。", score: 0.927 },
    { chunk_id: "chunk-2", media_biz_id: "doc-2", title: "半导体供应链报告", snippet: "上游设备仍是供应链中的关键瓶颈。", score: 0.811 },
  ]);
  if (request.method === "POST" && url.pathname === `/api/kbs/${kb.id}/chat`) {
    response.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" });
    response.write(`event: token\ndata: {"token":"根据已收录资料，"}\n\n`);
    response.write(`event: token\ndata: {"token":"Agent 的核心是任务规划与工具执行。[1]"}\n\n`);
    response.write(`event: citations\ndata: [{"media_biz_id":"doc-1","title":"AI Agent 产品观察","chunk_id":"chunk-1","snippet":"任务规划与工具执行","score":0.9}]\n\n`);
    response.end(`event: done\ndata: {"conversation_id":"conv-demo"}\n\n`);
    return;
  }
  if (request.method === "GET" && url.pathname === "/api/conversations/conv-demo/messages") return send(response, 200, []);
  if (["POST", "DELETE"].includes(request.method)) return send(response, request.method === "DELETE" ? 204 : 202, { media_biz_id: "mock", duplicate: false });
  send(response, 404, { error: "not found" });
}).listen(8080, "127.0.0.1", () => console.log("mock API on http://127.0.0.1:8080"));
