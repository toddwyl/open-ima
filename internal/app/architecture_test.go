package app

import (
	"os/exec"
	"strings"
	"testing"
)

// TestArchitectureImportRules 固化分层依赖方向(仅检查非测试导入):
//
//	pkg           仅标准库(共享工具)
//	domain        标准库 + pkg + 同层包
//	application   domain + application(含 port) + pkg
//	port          仅标准库
//	infrastructure domain + application/port(不得依赖用例与接口层)
//	interfaces    domain + application(不得依赖 infrastructure)
//	app           装配根,可依赖一切
func TestArchitectureImportRules(t *testing.T) {
	output, err := exec.Command("go", "list", "-f", "{{.ImportPath}}={{join .Imports \",\"}}", "open-ima/internal/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, output)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		path, imports, _ := strings.Cut(line, "=")
		short := strings.TrimPrefix(path, "open-ima/")
		for _, imported := range strings.Split(imports, ",") {
			if imported == "" {
				continue
			}
			if reason := violation(short, imported); reason != "" {
				t.Errorf("%s imports %s: %s", short, imported, reason)
			}
		}
	}
}

func violation(pkg, imported string) string {
	repo := strings.TrimPrefix(imported, "open-ima/")
	internal := repo != imported && strings.HasPrefix(repo, "internal/")
	external := !internal && strings.Contains(strings.SplitN(imported, "/", 2)[0], ".")

	switch layer := strings.SplitN(strings.TrimPrefix(pkg, "internal/"), "/", 2)[0]; {
	case pkg == "internal/app":
		return ""
	case strings.HasPrefix(pkg, "internal/pkg"):
		if external || internal {
			return "pkg utilities must depend on the standard library only"
		}
	case strings.HasPrefix(pkg, "internal/domain"):
		if external {
			return "domain must depend on the standard library only"
		}
		if internal && !strings.HasPrefix(repo, "internal/domain/") && !strings.HasPrefix(repo, "internal/pkg/") {
			return "domain may only import sibling domain packages and pkg utilities"
		}
	case pkg == "internal/application/port":
		if external || internal {
			return "port must depend on the standard library only"
		}
	case strings.HasPrefix(pkg, "internal/application"):
		if internal && !strings.HasPrefix(repo, "internal/domain/") && !strings.HasPrefix(repo, "internal/application/") && !strings.HasPrefix(repo, "internal/pkg/") {
			return "use cases may only import domain, application and pkg packages"
		}
	case strings.HasPrefix(pkg, "internal/infrastructure"):
		if internal && !strings.HasPrefix(repo, "internal/domain/") && repo != "internal/application/port" {
			return "infrastructure may only import domain and application/port"
		}
	case strings.HasPrefix(pkg, "internal/interfaces"):
		if internal && !strings.HasPrefix(repo, "internal/domain/") && !strings.HasPrefix(repo, "internal/application/") {
			return "interfaces may only import domain and application packages"
		}
	default:
		_ = layer
	}
	return ""
}
