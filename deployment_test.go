package openima_test

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestComposeDeploymentShape(t *testing.T) {
	data, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Build       any            `yaml:"build"`
			Image       string         `yaml:"image"`
			Profiles    []string       `yaml:"profiles"`
			Healthcheck map[string]any `yaml:"healthcheck"`
			DependsOn   map[string]any `yaml:"depends_on"`
			Volumes     []string       `yaml:"volumes"`
			Environment map[string]any `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app", "parser", "meilisearch"} {
		service, ok := compose.Services[name]
		if !ok {
			t.Fatalf("missing service %q", name)
		}
		if len(service.Profiles) != 0 {
			t.Fatalf("default service %q unexpectedly profile-gated", name)
		}
		if len(service.Healthcheck) == 0 {
			t.Fatalf("service %q has no healthcheck", name)
		}
	}
	mock, ok := compose.Services["model-mock"]
	if !ok || len(mock.Profiles) != 1 || mock.Profiles[0] != "smoke" {
		t.Fatalf("model-mock must be smoke-only: %+v", mock.Profiles)
	}
	app := compose.Services["app"]
	if _, ok := app.DependsOn["parser"]; !ok {
		t.Fatal("app must depend on parser")
	}
	if _, ok := app.DependsOn["meilisearch"]; !ok {
		t.Fatal("app must depend on meilisearch")
	}
	if len(app.Volumes) == 0 {
		t.Fatal("app must persist data")
	}
}

func TestHarnessHasNoTemplateTODOs(t *testing.T) {
	data, err := os.ReadFile("scripts/harness.sh")
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "TODO") || strings.Contains(content, "栈无关骨架") {
		t.Fatal("harness still contains template TODOs")
	}
	if !strings.Contains(content, "npm audit --audit-level=high") {
		t.Fatal("harness must audit frontend production dependencies")
	}
}
