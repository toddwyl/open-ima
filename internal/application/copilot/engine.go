// Package copilot 承载统一问答引擎:快问/agent 两种模式共用同一 ReAct 实现,
// 模式仅为工具集、提示与轮次上限的配置档。
package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/conversation"
)

// EventType 是引擎产出的事件类型;references 在答案事件之前,done/error 由上层封装。
type EventType string

const (
	EventThought    EventType = "thought"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventReferences EventType = "references"
	EventToken      EventType = "token"
)

// Event 是引擎向上层推送的流式事件。
type Event struct {
	Type       EventType
	Round      int
	Content    string                  // thought 或 token 文本
	CallID     string                  // tool_call / tool_result
	ToolName   string                  // tool_call / tool_result
	Args       json.RawMessage         // tool_call
	Success    bool                    // tool_result
	Output     string                  // tool_result(已被注册表限长)
	Error      string                  // tool_result
	DurationMs int64                   // tool_result
	References []conversation.Citation // references
}

// Guards 是 ReAct 循环的守护参数;默认值对齐 WeKnora。
type Guards struct {
	MaxIterations              int           // 轮次上限
	MaxEmptyResponseRetries    int           // 自然停止但内容为空的重试次数
	MaxRepeatedResponseRounds  int           // 连续相同轮次(卡死)上限
	MaxConsecutiveLengthRounds int           // 连续被 completion 上限截断的轮次上限
	MaxToolOutputChars         int           // 单工具输出预算
	LLMCallTimeout             time.Duration // 单次 LLM 调用超时
	FinalAnswerChunkRunes      int           // 答案回放为 token 事件的分块粒度
}

// DefaultGuards 返回对齐 WeKnora 默认值的守护参数。
func DefaultGuards() Guards {
	return Guards{
		MaxIterations:              20,
		MaxEmptyResponseRetries:    2,
		MaxRepeatedResponseRounds:  2,
		MaxConsecutiveLengthRounds: 3,
		MaxToolOutputChars:         16000,
		LLMCallTimeout:             120 * time.Second,
		FinalAnswerChunkRunes:      16,
	}
}

// ToolSet 是引擎执行工具调用所依赖的注册表能力;
// Execute 永不返回 error——未知工具、参数错误、panic 均回收为 Success=false 的结果。
type ToolSet interface {
	Defs() []port.ToolDef
	Execute(ctx context.Context, name string, args json.RawMessage) *port.ToolResult
}

// Profile 是一个模式配置档:系统提示与轮次上限的组合;工具集由 ToolSet 承载。
type Profile struct {
	Name          string // quick | agent
	SystemPrompt  string
	MaxIterations int
}

// Outcome 是一次引擎执行的完整结果。
type Outcome struct {
	Answer     string
	Steps      []conversation.AgentStep
	References []conversation.Citation
	Rounds     int
	Truncated  bool // 被截断或兜底合成的答案
}

// Engine 是无状态的 ReAct 引擎:历史由调用方重建后传入,跨 turn 不保存状态。
type Engine struct {
	model  port.ChatModel
	guards Guards
}

func NewEngine(model port.ChatModel, guards Guards) *Engine {
	return &Engine{model: model, guards: normalizeGuards(guards)}
}

// Run 执行一个 turn:四阶段循环(think→analyze→act→observe)直至给出最终答案或触发守护兜底。
// messages 为不含系统提示的对话上下文(历史 + 当前 user 消息);系统提示由配置档注入。
func (e *Engine) Run(ctx context.Context, messages []port.ChatMessage, toolSet ToolSet, profile Profile, emit func(Event) error) (*Outcome, error) {
	maxIterations := profile.MaxIterations
	if maxIterations <= 0 {
		maxIterations = e.guards.MaxIterations
	}
	thread := make([]port.ChatMessage, 0, len(messages)+1)
	if profile.SystemPrompt != "" {
		thread = append(thread, port.ChatMessage{Role: "system", Content: profile.SystemPrompt})
	}
	thread = append(thread, messages...)

	outcome := &Outcome{}
	referenceIndex := make(map[string]bool)
	collectReferences := func(result *port.ToolResult) {
		citations, _ := result.Data["citations"].([]conversation.Citation)
		for _, citation := range citations {
			key := citation.SourceType + "|" + citation.ChunkBizID + "|" + citation.URL
			if referenceIndex[key] {
				continue
			}
			referenceIndex[key] = true
			outcome.References = append(outcome.References, citation)
		}
	}

	var accumulated strings.Builder // 连续 length 截断轮的内容累积
	var lastSignature string        // 上一轮(内容 + 工具调用)签名,用于卡死检测
	emptyRetries, repeatedRounds, lengthRounds := 0, 0, 0

	for outcome.Rounds < maxIterations {
		if err := ctx.Err(); err != nil {
			return e.cancelOutcome(ctx, thread, outcome, accumulated.String(), emit)
		}
		outcome.Rounds++
		callCtx, cancel := context.WithTimeout(ctx, e.guards.LLMCallTimeout)
		resp, err := e.model.CompleteTools(callCtx, thread, toolSet.Defs())
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return e.cancelOutcome(ctx, thread, outcome, accumulated.String(), emit)
			}
			return nil, err
		}
		step := conversation.AgentStep{
			Iteration: outcome.Rounds, Thought: resp.Content, Reasoning: resp.Reasoning,
			Truncated: resp.FinishReason == "length", Timestamp: time.Now(),
		}

		if len(resp.ToolCalls) == 0 {
			content := strings.TrimSpace(resp.Content)
			switch {
			case resp.FinishReason == "length":
				lengthRounds++
				accumulated.WriteString(resp.Content)
				outcome.Steps = append(outcome.Steps, step)
				thread = append(thread, port.ChatMessage{Role: "assistant", Content: resp.Content})
				if lengthRounds >= e.guards.MaxConsecutiveLengthRounds {
					outcome.Answer = accumulated.String()
					outcome.Truncated = true
					return outcome, e.emitAnswer(outcome, emit)
				}
				thread = append(thread, port.ChatMessage{Role: "user", Content: "继续。"})
				continue
			case content == "":
				emptyRetries++
				outcome.Steps = append(outcome.Steps, step)
				if emptyRetries > e.guards.MaxEmptyResponseRetries {
					return e.fallback(ctx, thread, outcome, accumulated.String(), emit)
				}
				thread = append(thread,
					port.ChatMessage{Role: "assistant", Content: resp.Content},
					port.ChatMessage{Role: "user", Content: "请给出完整答案。"})
				continue
			default:
				// 自然停止的纯文本轮:最终答案
				outcome.Answer = accumulated.String() + resp.Content
				outcome.Steps = append(outcome.Steps, step)
				return outcome, e.emitAnswer(outcome, emit)
			}
		}

		// 含工具调用的轮:纯文本是 preamble,只作 Thought 展示,绝不作为答案。
		// 卡死检测:内容 + 工具调用签名与上一轮相同,连续达到阈值即停止,以末轮文本作答。
		signature := roundSignature(resp)
		if signature == lastSignature {
			repeatedRounds++
			if repeatedRounds >= e.guards.MaxRepeatedResponseRounds {
				outcome.Steps = append(outcome.Steps, step)
				outcome.Answer = accumulated.String() + resp.Content
				if strings.TrimSpace(outcome.Answer) == "" {
					outcome.Answer = "模型在同一动作上反复停留,未能继续推进;请换个问法或补充更多上下文再试。"
				}
				return outcome, e.emitAnswer(outcome, emit)
			}
		} else {
			repeatedRounds = 0
			lastSignature = signature
		}

		if strings.TrimSpace(resp.Content) != "" {
			if err := emit(Event{Type: EventThought, Round: outcome.Rounds, Content: resp.Content}); err != nil {
				return nil, err
			}
		}
		calls, handles, err := e.act(ctx, toolSet, resp.ToolCalls, outcome.Rounds, emit, collectReferences)
		if err != nil {
			return nil, err
		}
		step.ToolCalls = calls
		step.Handles = handles
		outcome.Steps = append(outcome.Steps, step)
		thread = append(thread, port.ChatMessage{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
		for index, call := range resp.ToolCalls {
			record := step.ToolCalls[index]
			output := record.Output
			if !record.Success {
				output = "工具执行失败:" + record.Error
			}
			thread = append(thread, port.ChatMessage{
				Role: "tool", ToolCallID: call.ID, Name: call.Name, Content: output,
			})
		}
	}
	// 超轮次:用已有上下文强制生成兜底答案
	return e.fallback(ctx, thread, outcome, accumulated.String(), emit)
}

// normalizeGuards 把零值守护参数归一为默认值。
func normalizeGuards(guards Guards) Guards {
	defaults := DefaultGuards()
	if guards.MaxIterations <= 0 {
		guards.MaxIterations = defaults.MaxIterations
	}
	if guards.MaxEmptyResponseRetries <= 0 {
		guards.MaxEmptyResponseRetries = defaults.MaxEmptyResponseRetries
	}
	if guards.MaxRepeatedResponseRounds <= 0 {
		guards.MaxRepeatedResponseRounds = defaults.MaxRepeatedResponseRounds
	}
	if guards.MaxConsecutiveLengthRounds <= 0 {
		guards.MaxConsecutiveLengthRounds = defaults.MaxConsecutiveLengthRounds
	}
	if guards.MaxToolOutputChars <= 0 {
		guards.MaxToolOutputChars = defaults.MaxToolOutputChars
	}
	if guards.LLMCallTimeout <= 0 {
		guards.LLMCallTimeout = defaults.LLMCallTimeout
	}
	if guards.FinalAnswerChunkRunes <= 0 {
		guards.FinalAnswerChunkRunes = defaults.FinalAnswerChunkRunes
	}
	return guards
}

// act 并发执行本轮全部工具调用,逐项推送 tool_call / tool_result 事件;
// 返回与 toolCalls 等序的执行记录与本轮新分配的句柄映射。
func (e *Engine) act(ctx context.Context, toolSet ToolSet, toolCalls []port.LLMToolCall, round int, emit func(Event) error, collect func(*port.ToolResult)) ([]conversation.AgentToolCall, map[string]string, error) {
	records := make([]conversation.AgentToolCall, len(toolCalls))
	handles := make(map[string]string)
	var wait sync.WaitGroup
	var mu sync.Mutex
	var emitErr error
	for index, call := range toolCalls {
		if err := emit(Event{Type: EventToolCall, Round: round, CallID: call.ID, ToolName: call.Name, Args: call.Arguments}); err != nil {
			return nil, nil, err
		}
		wait.Add(1)
		go func(index int, call port.LLMToolCall) {
			defer wait.Done()
			start := time.Now()
			result := toolSet.Execute(ctx, call.Name, call.Arguments)
			mu.Lock()
			defer mu.Unlock()
			records[index] = conversation.AgentToolCall{
				ID: call.ID, Name: call.Name, Args: call.Arguments,
				Success: result.Success, Output: result.Output, Error: result.Error,
				DurationMs: time.Since(start).Milliseconds(),
			}
			if result.Success {
				collect(result)
			}
			for handle, bizID := range resultHandles(result) {
				handles[handle] = bizID
			}
			if emitErr != nil {
				return
			}
			emitErr = emit(Event{
				Type: EventToolResult, Round: round, CallID: call.ID, ToolName: call.Name,
				Success: result.Success, Output: result.Output, Error: result.Error,
				DurationMs: records[index].DurationMs,
			})
		}(index, call)
	}
	wait.Wait()
	if emitErr != nil {
		return nil, nil, emitErr
	}
	if len(handles) == 0 {
		handles = nil
	}
	return records, handles, nil
}

// resultHandles 从工具结果的结构化数据中提取新分配的句柄映射。
func resultHandles(result *port.ToolResult) map[string]string {
	handles, _ := result.Data["handles"].(map[string]string)
	return handles
}

// fallback 用已有上下文非流式生成兜底答案(超轮次/空回答重试耗尽时)。
func (e *Engine) fallback(ctx context.Context, thread []port.ChatMessage, outcome *Outcome, prefix string, emit func(Event) error) (*Outcome, error) {
	prompted := append(append([]port.ChatMessage{}, thread...),
		port.ChatMessage{Role: "user", Content: "请基于以上已经获得的信息,直接给出最终答案;信息不足时请明说。"})
	answer, err := e.model.Complete(context.WithoutCancel(ctx), prompted)
	if err != nil {
		answer = ""
	}
	if strings.TrimSpace(answer) == "" {
		answer = "抱歉,未能在限定步骤内完成回答,请换个问法或缩小范围再试。"
	}
	outcome.Answer = prefix + answer
	outcome.Truncated = true
	return outcome, e.emitAnswer(outcome, emit)
}

// cancelOutcome 处理用户取消:已有工具结果时合成答案收尾,否则原样返回取消错误。
func (e *Engine) cancelOutcome(ctx context.Context, thread []port.ChatMessage, outcome *Outcome, prefix string, emit func(Event) error) (*Outcome, error) {
	if !hasToolResults(thread) {
		return nil, ctx.Err()
	}
	return e.fallback(ctx, thread, outcome, prefix, emit)
}

func hasToolResults(thread []port.ChatMessage) bool {
	for _, message := range thread {
		if message.Role == "tool" {
			return true
		}
	}
	return false
}

// roundSignature 计算一轮响应的卡死检测签名:纯文本内容 + 全部工具调用的名称与参数。
func roundSignature(resp *port.ChatResponse) string {
	var signature strings.Builder
	signature.WriteString(strings.TrimSpace(resp.Content))
	for _, call := range resp.ToolCalls {
		fmt.Fprintf(&signature, "|%s:%s", call.Name, string(call.Arguments))
	}
	return signature.String()
}

// emitAnswer 在最终答案确定后推送 references 事件与 token 流;
// done 不在此处推送,由上层在持久化完成后恰好发一次。
func (e *Engine) emitAnswer(outcome *Outcome, emit func(Event) error) error {
	if outcome.References == nil {
		outcome.References = []conversation.Citation{}
	}
	if err := emit(Event{Type: EventReferences, References: outcome.References}); err != nil {
		return err
	}
	runes := []rune(outcome.Answer)
	for start := 0; start < len(runes); start += e.guards.FinalAnswerChunkRunes {
		end := min(start+e.guards.FinalAnswerChunkRunes, len(runes))
		if err := emit(Event{Type: EventToken, Content: string(runes[start:end])}); err != nil {
			return err
		}
	}
	return nil
}
