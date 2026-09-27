package port

import "context"

// Opener 用系统默认应用打开本地文件（如 macOS 的 open）。
// 由 infrastructure 按平台实现，应用层只依赖本契约。
type Opener interface {
	Open(ctx context.Context, path string) error
}
