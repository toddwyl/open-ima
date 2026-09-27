package copilot

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"open-ima/internal/application/copilot/tools"
	"open-ima/internal/application/port"
	"open-ima/internal/domain/conversation"
	"open-ima/internal/domain/knowledgebase"
	"open-ima/internal/domain/media"
	settingsdom "open-ima/internal/domain/settings"
)

// WebSearchFactory 按 provider 构造联网搜索实现;由装配根注入(infrastructure 细节不外泄)。
type WebSearchFactory func(provider, searxngBaseURL string) (port.WebSearcher, error)

// ChatResult 是一次对话的完成数据(done 事件载荷)。
type ChatResult struct {
	Rounds    int
	Truncated bool
	Citations []conversation.Citation
	Mode      string
	Degraded  bool // 模型不支持工具调用,已降级为固定检索管线
}

// 模式配置档的系统提示。
const (
	agentSystemPrompt = "你是知识库问答助手。通过调用工具检索知识库、阅读文档正文,必要时联网搜索,逐步找到答案。\n" +
		"规则:\n" +
		"1. 先判断是否需要检索:常识、寒暄可直接作答,不调用工具。\n" +
		"2. 检索写完整自然语言问句;结果不足时改写查询补搜,或换用 read_document 深读候选文档。\n" +
		"3. 答案中引用事实时只用来源句柄标注,如 [c1]、[w1];引用整篇文档的结论可用 [d1];不要编造句柄,也不要使用 [分块N/M] 等其它记号。\n" +
		"4. 知识库与网络都没有答案时,明确说明,不要编造。"
	quickSystemPrompt = "你是知识库问答助手。至多调用一次 search_knowledge 检索,然后基于结果直接作答;" +
		"结果不足以回答时明说。答案中引用事实时用来源句柄标注,如 [c1]。"
	fallbackPrompt = "Answer from the provided sources. Cite factual claims with source numbers like [1]. " +
		"Say when the sources do not contain the answer.\n\nSources:\n"
	stoppedNotice = "已停止生成。"
)

// Service 是统一问答用例:快问/agent 两种模式配置档共用同一 ReAct 引擎与 SSE 契约。
type Service struct {
	conv      *conversation.ConversationService
	kbs       *knowledgebase.KBService
	docs      *media.MediaService
	search    port.Searcher
	indexName string
	guards    Guards

	settingsFn func() settingsdom.Values
	webFactory WebSearchFactory

	mu                sync.RWMutex
	models            map[string]port.ChatModel
	defaultModelBizID string
}

func NewService(
	conv *conversation.ConversationService, kbs *knowledgebase.KBService, docs *media.MediaService,
	searcher port.Searcher, indexName string, model port.ChatModel,
	guards Guards, settingsFn func() settingsdom.Values, webFactory WebSearchFactory,
) *Service {
	return &Service{
		conv: conv, kbs: kbs, docs: docs, search: searcher, indexName: indexName,
		guards:     normalizeGuards(guards),
		settingsFn: settingsFn, webFactory: webFactory,
		models: map[string]port.ChatModel{"default": model}, defaultModelBizID: "default",
	}
}

// SetModels 原子替换可选聊天模型集合。
func (s *Service) SetModels(models map[string]port.ChatModel, defaultModelBizID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models = models
	s.defaultModelBizID = defaultModelBizID
}

func (s *Service) chatModel(modelBizID string) (port.ChatModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if modelBizID == "" {
		modelBizID = s.defaultModelBizID
	}
	model := s.models[modelBizID]
	if model == nil {
		return nil, fmt.Errorf("chat model %q is not configured", modelBizID)
	}
	return model, nil
}

// Chat 执行一轮统一问答:落 user 消息 → 重建上下文 → ReAct 循环 → 落 assistant 消息。
// emit 依次收到 thought/tool_call/tool_result/references/token 事件;done 由调用方在返回后推送。
func (s *Service) Chat(ctx context.Context, kbBizID, conversationBizID, modelBizID, mode, query string, emit func(Event) error) (string, *ChatResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil, fmt.Errorf("query is required")
	}
	if conversationBizID == "" {
		if err := s.ensureKnowledgeBase(ctx, kbBizID); err != nil {
			return "", nil, err
		}
	}
	conv, err := s.conv.Ensure(ctx, kbBizID, conversationBizID, query, mode)
	if err != nil {
		return "", nil, err
	}
	mode = conv.Mode // 模式会话级生效:既有会话沿用其创建时的模式
	history, err := s.conv.RecentMessages(ctx, conv.BizID, 10)
	if err != nil {
		return conv.BizID, nil, err
	}
	model, err := s.chatModel(modelBizID)
	if err != nil {
		return conv.BizID, nil, err
	}
	if err := s.conv.Append(ctx, conv.BizID, "user", query, nil, nil); err != nil {
		return conv.BizID, nil, err
	}

	registry, profile, _ := s.buildProfile(mode, kbBizID)
	engine := NewEngine(model, s.guards)
	outcome, err := engine.Run(ctx, buildContext(history, query), registry, profile, emit)
	switch {
	case err == nil:
		// 正常完成
	case errors.Is(err, port.ErrToolsUnsupported):
		return s.degradedChat(ctx, conv.BizID, model, kbBizID, history, query, emit)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// 用户取消且无工具结果可合成:落一条占位助手消息,保证历史不残缺
		if appendErr := s.conv.Append(context.WithoutCancel(ctx), conv.BizID, "assistant", stoppedNotice, nil, outcomeSteps(outcome)); appendErr != nil {
			return conv.BizID, nil, appendErr
		}
		return conv.BizID, &ChatResult{Truncated: true, Citations: []conversation.Citation{}, Mode: mode}, nil
	default:
		return conv.BizID, nil, err
	}

	// 句柄改写已在引擎 emitAnswer 内完成,这里直接持久化最终答案。
	if err := s.conv.Append(ctx, conv.BizID, "assistant", outcome.Answer, outcome.References, outcome.Steps); err != nil {
		return conv.BizID, nil, err
	}
	citations := outcome.References
	if citations == nil {
		citations = []conversation.Citation{}
	}
	return conv.BizID, &ChatResult{
		Rounds: outcome.Rounds, Truncated: outcome.Truncated,
		Citations: citations, Mode: mode,
	}, nil
}

// degradedChat 在模型不支持工具调用时执行降级:一次直接检索 + 流式生成,
// 结果标注 mode=quick 与 degraded,不静默伪劣执行。
func (s *Service) degradedChat(ctx context.Context, conversationBizID string, model port.ChatModel, kbBizID string, history []conversation.Message, query string, emit func(Event) error) (string, *ChatResult, error) {
	hits, err := s.search.Search(ctx, s.indexName, port.SearchRequest{
		Query: query, Filter: "kb_biz_id = '" + escapeFilter(kbBizID) + "'", Limit: 8, Hybrid: true,
	})
	if err != nil {
		return conversationBizID, nil, err
	}
	citations := make([]conversation.Citation, len(hits))
	for index, hit := range hits {
		snippet := hit.Formatted
		if snippet == "" {
			snippet = hit.Content
		}
		citations[index] = conversation.Citation{
			SourceType: conversation.SourceTypeKBChunk,
			MediaBizID: hit.MediaBizID, ChunkBizID: hit.ID,
			Title: hit.Title, Snippet: snippet, Score: hit.Score,
		}
	}
	if err := emit(Event{Type: EventReferences, References: citations}); err != nil {
		return conversationBizID, nil, err
	}
	messages := buildFallbackMessages(history, query, citations)
	var answer strings.Builder
	err = model.Stream(ctx, messages, func(token string) error {
		answer.WriteString(token)
		return emit(Event{Type: EventToken, Content: token})
	})
	if err != nil {
		return conversationBizID, nil, err
	}
	if err := s.conv.Append(ctx, conversationBizID, "assistant", answer.String(), citations, nil); err != nil {
		return conversationBizID, nil, err
	}
	return conversationBizID, &ChatResult{
		Rounds: 1, Citations: citations, Mode: conversation.ModeQuick, Degraded: true,
	}, nil
}

// buildProfile 按模式装配工具注册表与配置档;每 turn 独立句柄表。
func (s *Service) buildProfile(mode, kbBizID string) (*tools.Registry, Profile, *tools.Handles) {
	handles := tools.NewHandles()
	registry := tools.NewRegistry(s.guards.MaxToolOutputChars)
	registry.Register(tools.NewSearchKnowledge(s.search, s.indexName, kbBizID, handles))
	if mode == conversation.ModeQuick {
		return registry, Profile{Name: mode, SystemPrompt: quickSystemPrompt, MaxIterations: 2}, handles
	}
	registry.Register(tools.NewReadDocument(s.docs, s.search, s.indexName, handles))
	registry.Register(tools.NewListDocuments(s.docs, kbBizID, handles))
	if searcher := s.webSearcher(); searcher != nil {
		registry.Register(tools.NewWebSearch(searcher, s.settings().WebSearchMaxResults, handles))
	}
	return registry, Profile{Name: mode, SystemPrompt: agentSystemPrompt, MaxIterations: s.guards.MaxIterations}, handles
}

// webSearcher 在设置开启时按 provider 构造联网搜索实现;未开启或 provider 未实现返回 nil。
func (s *Service) webSearcher() port.WebSearcher {
	settings := s.settings()
	if !settings.WebSearchEnabled || s.webFactory == nil {
		return nil
	}
	searcher, err := s.webFactory(settings.WebSearchProvider, settings.SearxngBaseURL)
	if err != nil {
		return nil
	}
	return searcher
}

func (s *Service) settings() settingsdom.Values {
	if s.settingsFn == nil {
		return settingsdom.Values{}
	}
	return s.settingsFn()
}

func (s *Service) ensureKnowledgeBase(ctx context.Context, kbBizID string) error {
	exists, err := s.kbs.Exists(ctx, kbBizID)
	if err != nil {
		return err
	}
	if !exists {
		return knowledgebase.ErrNotFound
	}
	return nil
}

func (s *Service) ListConversations(ctx context.Context, kbBizID string) ([]conversation.Conversation, error) {
	return s.conv.ListByKB(ctx, kbBizID)
}

func (s *Service) ListMessages(ctx context.Context, conversationBizID string) ([]conversation.Message, error) {
	return s.conv.ListMessages(ctx, conversationBizID)
}

// buildContext 从历史消息重建 LLM 对话上下文(引擎跨 turn 无状态,每 turn 重建)。
func buildContext(history []conversation.Message, query string) []port.ChatMessage {
	messages := make([]port.ChatMessage, 0, len(history)+1)
	for _, message := range history {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		messages = append(messages, port.ChatMessage{Role: message.Role, Content: message.Content})
	}
	return append(messages, port.ChatMessage{Role: "user", Content: query})
}

// buildFallbackMessages 组装降级管线的生成提示(来源编号引用)。
func buildFallbackMessages(history []conversation.Message, query string, citations []conversation.Citation) []port.ChatMessage {
	var sources strings.Builder
	for index, citation := range citations {
		fmt.Fprintf(&sources, "[%d] %s\n%s\n\n", index+1, citation.Title, citation.Snippet)
	}
	messages := []port.ChatMessage{{Role: "system", Content: fallbackPrompt + sources.String()}}
	for _, message := range history {
		if message.Role == "user" || message.Role == "assistant" {
			messages = append(messages, port.ChatMessage{Role: message.Role, Content: message.Content})
		}
	}
	return append(messages, port.ChatMessage{Role: "user", Content: query})
}

// handleCitationPattern 匹配答案中的来源句柄引用,如 [c1]、[d2]、[w1]。
var handleCitationPattern = regexp.MustCompile(`\[([cdw]\d{1,3})\]`)

// rewriteHandleCitations 把答案中的句柄引用改写为 references 序号,与前端引用区对齐;
// snapshot 为句柄→业务键映射,无法解析的句柄保留原文。
func rewriteHandleCitations(answer string, snapshot map[string]string, references []conversation.Citation) string {
	if len(references) == 0 {
		return answer
	}
	indexByKey := make(map[string]int, len(references))
	for index, reference := range references {
		// 分块/URL 精确键优先;媒体键作为兜底,让 [dN] 文档句柄能落到该媒体的首条引用。
		for _, key := range []string{reference.ChunkBizID, reference.URL, reference.MediaBizID} {
			if key == "" {
				continue
			}
			if _, exists := indexByKey[key]; !exists {
				indexByKey[key] = index + 1
			}
		}
	}
	return handleCitationPattern.ReplaceAllStringFunc(answer, func(match string) string {
		handle := match[1 : len(match)-1]
		bizID, ok := snapshot[handle]
		if !ok {
			return match
		}
		if index, ok := indexByKey[bizID]; ok {
			return fmt.Sprintf("[%d]", index)
		}
		return match
	})
}

func outcomeSteps(outcome *Outcome) []conversation.AgentStep {
	if outcome == nil {
		return nil
	}
	return outcome.Steps
}

func escapeFilter(value string) string { return strings.ReplaceAll(value, "'", "''") }
