package settings

import "context"

// Repository 是应用设置的持久化契约,以键值对形式存取,实现位于 infrastructure。
type Repository interface {
	// Load 返回全部已持久化的键值对;未设置的键不出现在结果中。
	Load(ctx context.Context) (map[string]string, error)
	// Save 原子地覆盖写入给定键值对。
	Save(ctx context.Context, values map[string]string) error
}
