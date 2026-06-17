package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

const (
	defaultDeepSeekBaseURL = "https://api.deepseek.com"
	defaultReasoningEffort = "medium"
	chatCompletionsPath    = "/chat/completions"
)

// DeepSeekClient is a thin HTTP client that talks to DeepSeek's
// OpenAI-compatible API. The standard go-openai library doesn't expose
// DeepSeek's `thinking` and `reasoning_effort` request parameters, so
// we build a small wrapper that injects them at the JSON level.
//
// Thinking mode reduces hallucinations by giving the model explicit
// reasoning budget before it generates the visible response. SMS
// responses are short (≤400 chars) but the model benefits from
// "thinking out loud" when verifying prices, units, and phone numbers
// against the property context.
type DeepSeekClient struct {
	baseURL         string
	apiKey          string
	httpClient      *http.Client
	reasoningEffort string

	// Last reasoning content returned by the most recent call. Cleared
	// at the start of every CreateChatCompletion. The generator reads
	// this via ReasoningContent() to dump it to the debug folder.
	lastReasoningContent string
}

// NewDeepSeekClient constructs a client targeting the configured
// DeepSeek endpoint. `REASONING_EFFORT` env var controls the depth of
// the model's reasoning (low|medium|high). Default is "medium" — fast
// enough for SMS but enough budget to verify factual claims.
func NewDeepSeekClient(apiKey string) *DeepSeekClient {
	baseURL := os.Getenv("DEEPSEEK_BASE_URL")
	if baseURL == "" {
		baseURL = defaultDeepSeekBaseURL
	}
	effort := os.Getenv("REASONING_EFFORT")
	if effort == "" {
		effort = defaultReasoningEffort
	}
	return &DeepSeekClient{
		baseURL:         baseURL,
		apiKey:          apiKey,
		httpClient:      &http.Client{Timeout: 60 * time.Second},
		reasoningEffort: effort,
	}
}

// ReasoningEffort returns the configured reasoning effort level.
func (c *DeepSeekClient) ReasoningEffort() string {
	return c.reasoningEffort
}

// ReasoningContent returns the reasoning_content field from the most
// recent DeepSeek response. Returns empty string if thinking was not
// enabled, the response had no choices, or the model returned no
// reasoning.
func (c *DeepSeekClient) ReasoningContent() string {
	return c.lastReasoningContent
}

// deepSeekRequest mirrors openai.ChatCompletionRequest plus the
// DeepSeek-specific `thinking` and `reasoning_effort` fields.
type deepSeekRequest struct {
	Model               string                               `json:"model"`
	Messages            []openai.ChatCompletionMessage       `json:"messages"`
	MaxCompletionTokens int                                  `json:"max_completion_tokens,omitempty"`
	Temperature         float32                              `json:"temperature,omitempty"`
	ResponseFormat      *openai.ChatCompletionResponseFormat `json:"response_format,omitempty"`
	Thinking            *deepSeekThinking                    `json:"thinking,omitempty"`
	ReasoningEffort     string                               `json:"reasoning_effort,omitempty"`
	Stream              bool                                 `json:"stream,omitempty"`
}

type deepSeekThinking struct {
	Type string `json:"type"`
}

// deepSeekMessage captures both `content` (the visible response) and
// `reasoning_content` (the model's internal thinking, if thinking was
// enabled). The reasoning_content is logged for debugging but never
// returned to the lead.
type deepSeekMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// deepSeekResponse mirrors openai.ChatCompletionResponse plus the
// `reasoning_content` field on the message.
type deepSeekResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int                 `json:"index"`
		Message      deepSeekMessage     `json:"message"`
		FinishReason openai.FinishReason `json:"finish_reason"`
	} `json:"choices"`
	Usage openai.Usage `json:"usage"`
}

// CreateChatCompletion sends the request to DeepSeek with thinking
// enabled and the configured reasoning effort. Returns a response
// whose `Choices[i].Message.Content` is the visible SMS text. The
// reasoning content (if any) is stored on the client and accessible
// via ReasoningContent().
//
// The signature intentionally matches openai.Client.CreateChatCompletion
// so callers can swap between the two clients transparently.
func (c *DeepSeekClient) CreateChatCompletion(
	ctx context.Context,
	req openai.ChatCompletionRequest,
) (openai.ChatCompletionResponse, error) {
	c.lastReasoningContent = ""

	dsReq := deepSeekRequest{
		Model:               req.Model,
		Messages:            req.Messages,
		MaxCompletionTokens: req.MaxCompletionTokens,
		Temperature:         req.Temperature,
		ResponseFormat:      req.ResponseFormat,
		Thinking:            &deepSeekThinking{Type: "enabled"},
		ReasoningEffort:     c.reasoningEffort,
		Stream:              false,
	}

	body, err := json.Marshal(dsReq)
	if err != nil {
		return openai.ChatCompletionResponse{}, fmt.Errorf("deepseek: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+chatCompletionsPath, bytes.NewReader(body))
	if err != nil {
		return openai.ChatCompletionResponse{}, fmt.Errorf("deepseek: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return openai.ChatCompletionResponse{}, fmt.Errorf("deepseek: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return openai.ChatCompletionResponse{}, fmt.Errorf("deepseek: status %d: %s", resp.StatusCode, string(raw))
	}

	var dsResp deepSeekResponse
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil {
		return openai.ChatCompletionResponse{}, fmt.Errorf("deepseek: decode response: %w", err)
	}

	// Capture reasoning content from the first choice (we only ever
	// send n=1). Log it for visibility and stash it for the generator
	// to dump to the debug folder.
	if len(dsResp.Choices) > 0 {
		c.lastReasoningContent = dsResp.Choices[0].Message.ReasoningContent
		if c.lastReasoningContent != "" {
			logThinking(c.lastReasoningContent)
		}
	}

	// Map deepSeekResponse → openai.ChatCompletionResponse.
	result := openai.ChatCompletionResponse{
		ID:      dsResp.ID,
		Object:  dsResp.Object,
		Created: dsResp.Created,
		Model:   dsResp.Model,
		Usage:   dsResp.Usage,
	}
	for _, choice := range dsResp.Choices {
		result.Choices = append(result.Choices, openai.ChatCompletionChoice{
			Index: choice.Index,
			Message: openai.ChatCompletionMessage{
				Role:    choice.Message.Role,
				Content: choice.Message.Content,
			},
			FinishReason: choice.FinishReason,
		})
	}

	return result, nil
}

// logThinking prints the model's reasoning to the console with a
// distinctive color so it's easy to find in the test output. Truncated
// to 500 chars in the log line to avoid flooding the terminal; the
// full content is dumped to debug-prompts/<session>/turn-NNN-thinking.txt.
func logThinking(reasoning string) {
	const maxLogChars = 500
	truncated := reasoning
	if len(truncated) > maxLogChars {
		truncated = truncated[:maxLogChars] + "... [truncated; see turn-NNN-thinking.txt for full content]"
	}
	// Indent each line for readability.
	var sb strings.Builder
	sb.WriteString("\n\x1b[36m")
	sb.WriteString("┌─── [DEEPSEEK THINKING] ")
	for i := 0; i < 60; i++ {
		sb.WriteString("─")
	}
	sb.WriteString("\x1b[0m\n")
	for _, line := range strings.Split(truncated, "\n") {
		sb.WriteString("\x1b[36m│  \x1b[0m")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\x1b[36m└")
	for i := 0; i < 80; i++ {
		sb.WriteString("─")
	}
	sb.WriteString("\x1b[0m\n")
	log.Print(sb.String())
}
