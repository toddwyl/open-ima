package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"unicode/utf8"

	"open-ima/internal/application/port"
)

// Registry 是工具注册表:同名注册 first-wins(防同名劫持);
// Execute 永不返回 error,未知工具、参数错误、panic 均回收为 Success=false 的结果;
// 输出统一限长(超长 head+tail 截断)。
type Registry struct {
	mu             sync.RWMutex
	tools          map[string]port.Tool
	order          []string
	maxOutputChars int
}

func NewRegistry(maxOutputChars int) *Registry {
	return &Registry{tools: make(map[string]port.Tool), maxOutputChars: maxOutputChars}
}

// Register 注册工具;同名时保留先注册者。
func (r *Registry) Register(tool port.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[tool.Name()]; exists {
		return
	}
	r.tools[tool.Name()] = tool
	r.order = append(r.order, tool.Name())
}

// Defs 按注册顺序返回工具定义。
func (r *Registry) Defs() []port.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]port.ToolDef, 0, len(r.order))
	for _, name := range r.order {
		tool := r.tools[name]
		defs = append(defs, port.ToolDef{Name: tool.Name(), Description: tool.Description(), Parameters: tool.Parameters()})
	}
	return defs
}

// Execute 执行命名工具并回收一切故障为 ToolResult;输出限长。
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (result *port.ToolResult) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return &port.ToolResult{Success: false, Error: fmt.Sprintf("unknown tool %q", name)}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = &port.ToolResult{Success: false, Error: fmt.Sprintf("tool %s panicked: %v", name, recovered)}
		}
		result.Output = truncateHeadTail(result.Output, r.maxOutputChars)
	}()
	result, err := tool.Execute(ctx, args)
	if err != nil {
		return &port.ToolResult{Success: false, Error: err.Error()}
	}
	if result == nil {
		return &port.ToolResult{Success: false, Error: fmt.Sprintf("tool %s returned nil result", name)}
	}
	return result
}

// truncateHeadTail 把超长输出截断为 头 + 省略标记 + 尾,预算按字符(rune)计。
func truncateHeadTail(output string, maxChars int) string {
	if maxChars <= 0 || utf8.RuneCountInString(output) <= maxChars {
		return output
	}
	runes := []rune(output)
	head := maxChars * 2 / 3
	tail := maxChars - head
	return string(runes[:head]) + fmt.Sprintf("\n…(已截断,共 %d 字符)…\n", len(runes)) + string(runes[len(runes)-tail:])
}
