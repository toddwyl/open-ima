import { afterEach, describe, expect, it, vi } from "vitest";
import { streamChat } from "./api";

afterEach(() => vi.restoreAllMocks());

describe("streamChat", () => {
  it("reassembles fragmented SSE frames", async () => {
    const encoder = new TextEncoder();
    const chunks = [
      "event: token\ndata: {\"tok",
      "en\":\"你\"}\n\nevent: references\ndata: {\"items\":[{\"chunk_biz_id\":\"c1\",\"media_biz_id\":\"d1\",\"title\":\"Doc\",\"snippet\":\"S\",\"score\":1}]}\n\n",
      "event: done\ndata: {\"conversation_biz_id\":\"conv1\",\"rounds\":1,\"truncated\":false,\"citations\":[],\"mode\":\"agent\",\"degraded\":false}\n\n",
    ];
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(new ReadableStream({
      start(controller) {
        chunks.forEach((chunk) => controller.enqueue(encoder.encode(chunk)));
        controller.close();
      },
    }), { status: 200 }));
    const tokens: string[] = [];
    let citations = 0;
    let conversationID = "";
    await streamChat("kb1", null, "model-1", "agent", "q", {
      onToken: (token) => tokens.push(token),
      onReferences: (items) => { citations = items.length; },
      onDone: (done) => { conversationID = done.conversation_biz_id; },
    });
    expect(tokens).toEqual(["你"]);
    expect(citations).toBe(1);
    expect(conversationID).toBe("conv1");
    expect(fetchSpy).toHaveBeenCalledWith("/api/kbs/kb1/chat", expect.objectContaining({ body: expect.stringContaining('"model_biz_id":"model-1"') }));
    expect(fetchSpy).toHaveBeenCalledWith("/api/kbs/kb1/chat", expect.objectContaining({ body: expect.stringContaining('"mode":"agent"') }));
  });

  it("dispatches agent step events", async () => {
    const frames = [
      "event: thought\ndata: {\"round\":1,\"content\":\"先检索知识库\"}",
      "event: tool_call\ndata: {\"round\":1,\"id\":\"call_1\",\"name\":\"search_knowledge\",\"args\":{\"query\":\"兰花\"}}",
      "event: tool_result\ndata: {\"round\":1,\"id\":\"call_1\",\"name\":\"search_knowledge\",\"success\":true,\"output\":\"c1 兰花手册\",\"duration_ms\":42}",
      "event: token\ndata: {\"token\":\"答\"}",
      "event: done\ndata: {\"conversation_biz_id\":\"conv1\",\"rounds\":2,\"truncated\":false,\"citations\":[],\"mode\":\"agent\",\"degraded\":false}",
    ];
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(`${frames.join("\n\n")}\n\n`, { status: 200 }));
    const events: string[] = [];
    await streamChat("kb1", "conv1", "model-1", "agent", "q", {
      onToken: () => events.push("token"),
      onThought: (round, content) => events.push(`thought:${round}:${content}`),
      onToolCall: (call) => events.push(`tool_call:${call.name}`),
      onToolResult: (result) => events.push(`tool_result:${result.success}`),
      onReferences: () => events.push("references"),
      onDone: () => events.push("done"),
    });
    expect(events).toEqual(["thought:1:先检索知识库", "tool_call:search_knowledge", "tool_result:true", "token", "done"]);
  });

  it("throws stream error events", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("event: error\ndata: {\"error\":\"model down\"}\n\n", { status: 200 }));
    await expect(streamChat("kb1", null, "model-1", "quick", "q", { onToken() {}, onReferences() {}, onDone() {} })).rejects.toThrow("model down");
  });
});
