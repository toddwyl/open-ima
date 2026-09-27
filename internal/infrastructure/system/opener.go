// Package system 提供与操作系统交互的基础设施实现。
package system

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// Opener 用系统默认应用打开文件：macOS 用 open，Linux 用 xdg-open。
type Opener struct{ command string }

func NewOpener() (*Opener, error) {
	switch runtime.GOOS {
	case "darwin":
		return &Opener{command: "open"}, nil
	case "linux":
		return &Opener{command: "xdg-open"}, nil
	default:
		return nil, fmt.Errorf("opening files is not supported on %s", runtime.GOOS)
	}
}

func (o *Opener) Open(ctx context.Context, path string) error {
	// open/xdg-open 把文件交给系统后随即退出，但接管（LaunchServices 调度默认应用）
	// 是异步的；若跟随请求上下文，handler 返回即取消，可能把 open 进程掐死在交接前。
	return exec.CommandContext(context.WithoutCancel(ctx), o.command, path).Start()
}
