package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"open-ima/internal/domain/conversation"
)

// sseFrame 从 SSE 响应体中提取指定事件的 data 载荷。
func sseFrames(body, event string) []string {
	var frames []string
	for _, block := range strings.Split(body, "\n\n") {
		if strings.HasPrefix(block, "event: "+event+"\n") {
			frames = append(frames, strings.TrimPrefix(strings.SplitN(block, "\n", 2)[1], "data: "))
		}
	}
	return frames
}

func TestChatAgentSSEAndHistory(t *testing.T) {
	services := newTestServices(t)
	mux := http.NewServeMux()
	(&chatHandler{chat: services.chat, copilot: services.copilot}).register(mux)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/chat", bytes.NewBufferString(`{"query":"original query"}`))
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	// 统一契约:tool_call → tool_result → references → token* → done;references 在 token 之前
	for _, event := range []string{"event: tool_call", "event: tool_result", "event: references", "event: token", "event: done"} {
		if !strings.Contains(body, event) {
			t.Fatalf("missing %q in %s", event, body)
		}
	}
	if strings.Contains(body, "event: citations") {
		t.Fatalf("legacy citations event must be replaced by references: %s", body)
	}
	if !strings.Contains(body, "event: thought") {
		t.Fatalf("preamble should surface as thought event: %s", body)
	}
	if strings.Index(body, "event: references") > strings.Index(body, "event: token") {
		t.Fatalf("references must precede tokens: %s", body)
	}

	doneFrames := sseFrames(body, "done")
	if len(doneFrames) != 1 {
		t.Fatalf("done frames = %d, want exactly 1", len(doneFrames))
	}
	var done struct {
		ConversationBizID string                  `json:"conversation_biz_id"`
		Rounds            int                     `json:"rounds"`
		Truncated         bool                    `json:"truncated"`
		Citations         []conversation.Citation `json:"citations"`
		Mode              string                  `json:"mode"`
		Degraded          bool                    `json:"degraded"`
	}
	if err := json.Unmarshal([]byte(doneFrames[0]), &done); err != nil {
		t.Fatal(err)
	}
	if done.ConversationBizID == "" || done.Rounds != 2 || done.Truncated || done.Degraded || done.Mode != "agent" {
		t.Fatalf("done = %+v", done)
	}
	if len(done.Citations) != 2 || done.Citations[0].SourceType != conversation.SourceTypeKBChunk {
		t.Fatalf("citations = %+v", done.Citations)
	}

	messages, err := services.copilot.ListMessages(context.Background(), done.ConversationBizID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	// 句柄引用 [c1] 落库前被改写为 references 序号 [1]
	if messages[1].Content != "Answer [1]" {
		t.Fatalf("answer = %q", messages[1].Content)
	}
	if len(messages[1].AgentSteps) != 2 || messages[1].AgentSteps[0].ToolCalls[0].Name != "search_knowledge" {
		t.Fatalf("agent steps = %+v", messages[1].AgentSteps)
	}
	if messages[1].AgentSteps[0].Thought != "先查库。" {
		t.Fatalf("thought = %q", messages[1].AgentSteps[0].Thought)
	}
}

func TestChatQuickMode(t *testing.T) {
	services := newTestServices(t)
	mux := http.NewServeMux()
	(&chatHandler{chat: services.chat, copilot: services.copilot}).register(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/chat", bytes.NewBufferString(`{"query":"q","mode":"quick"}`)))
	doneFrames := sseFrames(recorder.Body.String(), "done")
	if len(doneFrames) != 1 {
		t.Fatalf("done frames = %d", len(doneFrames))
	}
	var done struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal([]byte(doneFrames[0]), &done)
	if done.Mode != "quick" {
		t.Fatalf("mode = %q", done.Mode)
	}
	// 会话级模式落库
	conversations, err := services.copilot.ListConversations(context.Background(), "kb1")
	if err != nil || len(conversations) != 1 || conversations[0].Mode != conversation.ModeQuick {
		t.Fatalf("conversations = %+v err=%v", conversations, err)
	}
}

func TestChatRejectsEmptyQuery(t *testing.T) {
	services := newTestServices(t)
	mux := http.NewServeMux()
	(&chatHandler{chat: services.chat, copilot: services.copilot}).register(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/chat", bytes.NewBufferString(`{"query":" "}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", recorder.Code)
	}
}

func TestChatRejectsBadMode(t *testing.T) {
	services := newTestServices(t)
	mux := http.NewServeMux()
	(&chatHandler{chat: services.chat, copilot: services.copilot}).register(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/chat", bytes.NewBufferString(`{"query":"q","mode":"turbo"}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", recorder.Code)
	}
}

func TestSearchRejectsUnknownKnowledgeBase(t *testing.T) {
	services := newTestServices(t)
	mux := http.NewServeMux()
	(&chatHandler{chat: services.chat, copilot: services.copilot}).register(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/kbs/missing/search?q=alpha", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("code = %d body = %s", recorder.Code, recorder.Body.String())
	}
}
