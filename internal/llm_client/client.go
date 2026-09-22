package llmclient

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"time"

	"hack-go-thon/pkg/log"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
)

// LLMClient wraps OpenAI API interactions with convenience helpers and raw client access.
type LLMClient struct {
	OpenAIClient openai.Client
}

// NewClient initializes an LLM client using an API key.
func NewClient(apiKey string, opts ...option.RequestOption) *LLMClient {
	if apiKey == "" {
		log.Warn("OpenAI API key is empty, LLM calls will fail unless configured")
		return nil
	}

	options := append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	client := openai.NewClient(options...)

	log.Info("LLM client initialized successfully")
	return &LLMClient{
		OpenAIClient: client,
	}
}

// GenerateChatCompletion sends a prompt to the model and returns the assistant's text response.
func (l *LLMClient) GenerateChatCompletion(ctx context.Context, model, prompt string) (string, error) {
	if l == nil {
		return "", fmt.Errorf("llm client is not configured (missing API key)")
	}

	chatModel := openai.ChatModelGPT4oMini
	if model != "" {
		chatModel = model
	}

	completion, err := l.OpenAIClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: chatModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate completion: %w", err)
	}

	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("no completion choices returned by model")
	}

	return completion.Choices[0].Message.Content, nil
}

// GenerateChatCompletionStream streams model completion tokens one-by-one via the onChunk callback.
// If the LLM client is not configured (no API key), it falls back to a realistic simulated token stream
// so developers can test real-time WebSocket streaming immediately during hackathons.
func (l *LLMClient) GenerateChatCompletionStream(ctx context.Context, model, prompt string, onChunk func(chunk string) error) error {
	if l == nil {
		return streamSimulatedResponse(ctx, prompt, onChunk)
	}

	chatModel := openai.ChatModelGPT4oMini
	if model != "" {
		chatModel = model
	}

	stream := l.OpenAIClient.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model: chatModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	})
	defer stream.Close()

	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				if err := onChunk(delta); err != nil {
					return err
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("error in llm token stream: %w", err)
	}

	return nil
}

// GenerateEmbedding generates a 1536-dimensional vector for text input.
// If the client is unconfigured, it generates a normalized deterministic pseudo-embedding for testing.
func (l *LLMClient) GenerateEmbedding(ctx context.Context, input string) ([]float32, error) {
	if l == nil {
		return GenerateMockEmbedding(input), nil
	}

	resp, err := l.OpenAIClient.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: param.NewOpt(input),
		},
		Model: openai.EmbeddingModelTextEmbedding3Small,
	})
	if err != nil {
		log.Warn("Failed to generate OpenAI embedding, falling back to mock embedding", "error", err.Error())
		return GenerateMockEmbedding(input), nil
	}

	if len(resp.Data) == 0 || len(resp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding returned by provider")
	}

	raw := resp.Data[0].Embedding
	vec := make([]float32, len(raw))
	for i, v := range raw {
		vec[i] = float32(v)
	}
	return vec, nil
}

// GenerateMockEmbedding generates a deterministic, L2-normalized 1536-dimensional vector for offline testing.
func GenerateMockEmbedding(input string) []float32 {
	const dim = 1536
	h := sha256.Sum256([]byte(input))
	seed := int64(binary.BigEndian.Uint64(h[:8]))
	rng := rand.New(rand.NewSource(seed))

	vec := make([]float32, dim)
	var sumSq float64
	for i := 0; i < dim; i++ {
		val := float32(rng.NormFloat64())
		vec[i] = val
		sumSq += float64(val * val)
	}

	norm := float32(math.Sqrt(sumSq))
	if norm > 0 {
		for i := 0; i < dim; i++ {
			vec[i] /= norm
		}
	}

	return vec
}

func streamSimulatedResponse(ctx context.Context, prompt string, onChunk func(chunk string) error) error {
	words := []string{
		"Hello! ", "I ", "am ", "streaming ", "this ", "response ", "in ", "real-time ",
		"over ", "simplysocket ", "WebSockets! ", "\n\n",
		"Your ", "prompt ", "was: ", fmt.Sprintf("%q. ", prompt), "\n\n",
		"This ", "is ", "a ", "zero-dependency ", "demonstration ", "stream. ",
		"Add ", "your ", "OPENAI_API_KEY ", "in ", ".env ",
		"to ", "stream ", "live ", "GPT-4o-mini ", "tokens."}

	for _, w := range words {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(35 * time.Millisecond):
			if err := onChunk(w); err != nil {
				return err
			}
		}
	}
	return nil
}
