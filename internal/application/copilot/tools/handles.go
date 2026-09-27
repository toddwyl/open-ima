// Package tools 承载 copilot 引擎的工具实现:知识库检索、文档读取、
// 文档浏览与联网搜索,以及统一注册表(输出限长、故障回收)。
package tools

import (
	"fmt"
	"sync"
)

// Handles 是 turn 级句柄注册表:为检索/浏览结果中的媒体与分块分配
// 短句柄(d1、c1),供模型在后续工具调用中引用;线程安全。
type Handles struct {
	mu         sync.Mutex
	byHandle   map[string]string
	byBizID    map[string]string
	chunkMedia map[string]string // chunk_biz_id → media_biz_id(检索时记录)
	counters   map[string]int
}

func NewHandles() *Handles {
	return &Handles{
		byHandle: make(map[string]string), byBizID: make(map[string]string),
		chunkMedia: make(map[string]string), counters: make(map[string]int),
	}
}

// Assign 为业务键分配句柄;同一业务键重复分配返回既有句柄。
// 返回 (句柄, 是否新分配)。
func (h *Handles) Assign(prefix, bizID string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if handle, ok := h.byBizID[bizID]; ok {
		return handle, false
	}
	h.counters[prefix]++
	handle := fmt.Sprintf("%s%d", prefix, h.counters[prefix])
	h.byHandle[handle] = bizID
	h.byBizID[bizID] = handle
	return handle, true
}

// LinkChunkMedia 记录分块所属媒体,供按分块句柄读正文时定位。
func (h *Handles) LinkChunkMedia(chunkBizID, mediaBizID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chunkMedia[chunkBizID] = mediaBizID
}

// Resolve 把句柄解析回业务键;非句柄输入原样返回,视为业务键直通。
func (h *Handles) Resolve(handleOrID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if bizID, ok := h.byHandle[handleOrID]; ok {
		return bizID
	}
	return handleOrID
}

// ChunkMedia 返回分块所属媒体业务键。
func (h *Handles) ChunkMedia(chunkBizID string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	mediaBizID, ok := h.chunkMedia[chunkBizID]
	return mediaBizID, ok
}

// Snapshot 返回当前全部句柄映射的副本(句柄 → 业务键)。
func (h *Handles) Snapshot() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	snapshot := make(map[string]string, len(h.byHandle))
	for handle, bizID := range h.byHandle {
		snapshot[handle] = bizID
	}
	return snapshot
}
