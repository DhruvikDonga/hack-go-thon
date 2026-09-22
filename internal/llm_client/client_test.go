package llmclient

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestLLMClient(t *testing.T) {
	t.Run("Empty API Key returns nil client", func(t *testing.T) {
		client := NewClient("")
		if client != nil {
			t.Errorf("expected nil client for empty api key")
		}
	})

	t.Run("GenerateChatCompletion with nil client returns error", func(t *testing.T) {
		var client *LLMClient
		_, err := client.GenerateChatCompletion(context.Background(), "gpt-4o-mini", "Hello")
		if err == nil {
			t.Errorf("expected error when generating completion with nil client")
		}
	})

	t.Run("GenerateMockEmbedding returns 1536 unit normalized vector", func(t *testing.T) {
		vec1 := GenerateMockEmbedding("hello world")
		if len(vec1) != 1536 {
			t.Fatalf("expected 1536 dimensions, got %d", len(vec1))
		}

		// Verify L2 norm is ~1.0
		var sumSq float64
		for _, v := range vec1 {
			sumSq += float64(v * v)
		}
		norm := math.Sqrt(sumSq)
		if math.Abs(norm-1.0) > 1e-4 {
			t.Errorf("expected unit vector norm ~1.0, got %f", norm)
		}

		// Verify deterministic reproducibility
		vec2 := GenerateMockEmbedding("hello world")
		for i := range vec1 {
			if vec1[i] != vec2[i] {
				t.Fatalf("expected deterministic output for identical inputs at index %d", i)
			}
		}
	})

	t.Run("GenerateChatCompletionStream fallback streams chunks", func(t *testing.T) {
		var client *LLMClient
		var chunks []string
		err := client.GenerateChatCompletionStream(context.Background(), "gpt-4o-mini", "Test prompt", func(chunk string) error {
			chunks = append(chunks, chunk)
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected streaming error: %v", err)
		}
		if len(chunks) == 0 {
			t.Fatalf("expected chunks in simulated stream, got 0")
		}
		full := strings.Join(chunks, "")
		if !strings.Contains(full, "Test prompt") {
			t.Errorf("expected simulated stream to reflect prompt, got %q", full)
		}
	})
}
