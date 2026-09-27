package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestKimiCompatibleProtocols(t *testing.T) {
	apiKey := os.Getenv("KIMI_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("KIMI_TEST_API_KEY is not set")
	}

	tests := []struct {
		name     string
		protocol string
		baseURL  string
	}{
		{name: "openai", protocol: "openai", baseURL: "https://api.kimi.com/coding/v1"},
		{name: "anthropic", protocol: "anthropic", baseURL: "https://api.kimi.com/coding/"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			client := NewChatClientWithProtocol(test.protocol, test.baseURL, apiKey, "kimi-for-coding")
			var output strings.Builder
			err := client.Stream(ctx, []Message{{Role: "user", Content: "Reply with exactly KIMI_OK"}}, func(token string) error {
				output.WriteString(token)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "KIMI_OK") {
				t.Fatalf("reply = %q", output.String())
			}
		})
	}
}
