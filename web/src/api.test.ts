import { afterEach, describe, expect, it, vi } from "vitest";
import { streamChat } from "./api";

afterEach(() => vi.restoreAllMocks());

describe("streamChat", () => {
  it("reassembles fragmented SSE frames", async () => {
    const encoder = new TextEncoder();
    const chunks = [
      "event: token\ndata: {\"tok",
      "en\":\"你\"}\n\nevent: citations\ndata: [{\"chunk_biz_id\":\"c1\",\"media_biz_id\":\"d1\",\"title\":\"Doc\",\"snippet\":\"S\",\"score\":1}]\n\n",
      "event: done\ndata: {\"conversation_biz_id\":\"conv1\"}\n\n",
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
    await streamChat("kb1", null, "model-1", "q", {
      onToken: (token) => tokens.push(token),
      onCitations: (items) => { citations = items.length; },
      onDone: (id) => { conversationID = id; },
    });
    expect(tokens).toEqual(["你"]);
    expect(citations).toBe(1);
    expect(conversationID).toBe("conv1");
    expect(fetchSpy).toHaveBeenCalledWith("/api/kbs/kb1/chat", expect.objectContaining({ body: expect.stringContaining('"model_biz_id":"model-1"') }));
  });

  it("throws stream error events", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("event: error\ndata: {\"error\":\"model down\"}\n\n", { status: 200 }));
    await expect(streamChat("kb1", null, "model-1", "q", { onToken() {}, onCitations() {}, onDone() {} })).rejects.toThrow("model down");
  });
});
