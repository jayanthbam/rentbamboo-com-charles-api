package generator

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestDeepSeekClient_NewClientDefaults(t *testing.T) {
	// Clear env vars to test defaults.
	os.Unsetenv("DEEPSEEK_BASE_URL")
	os.Unsetenv("REASONING_EFFORT")

	c := NewDeepSeekClient("test-key")
	if c.baseURL != defaultDeepSeekBaseURL {
		t.Errorf("expected default baseURL %q, got %q", defaultDeepSeekBaseURL, c.baseURL)
	}
	if c.reasoningEffort != defaultReasoningEffort {
		t.Errorf("expected default reasoning effort %q, got %q", defaultReasoningEffort, c.reasoningEffort)
	}
	if c.apiKey != "test-key" {
		t.Errorf("expected apiKey to be set, got %q", c.apiKey)
	}
}

func TestDeepSeekClient_EnvOverrides(t *testing.T) {
	os.Setenv("DEEPSEEK_BASE_URL", "https://custom.deepseek.example")
	os.Setenv("REASONING_EFFORT", "high")
	defer func() {
		os.Unsetenv("DEEPSEEK_BASE_URL")
		os.Unsetenv("REASONING_EFFORT")
	}()

	c := NewDeepSeekClient("test-key")
	if c.baseURL != "https://custom.deepseek.example" {
		t.Errorf("expected custom baseURL, got %q", c.baseURL)
	}
	if c.reasoningEffort != "high" {
		t.Errorf("expected reasoning effort 'high', got %q", c.reasoningEffort)
	}
}

// TestDeepSeekClient_SendsThinkingAndEffort verifies the request body
// includes `thinking.type: "enabled"` and `reasoning_effort: "medium"`.
func TestDeepSeekClient_SendsThinkingAndEffort(t *testing.T) {
	os.Unsetenv("REASONING_EFFORT")
	defer os.Unsetenv("REASONING_EFFORT")

	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &capturedBody); err != nil {
			t.Errorf("failed to unmarshal captured body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"id": "test",
			"object": "chat.completion",
			"created": 0,
			"model": "deepseek-v4-flash",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello!"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	defer server.Close()

	os.Setenv("DEEPSEEK_BASE_URL", server.URL)
	defer os.Unsetenv("DEEPSEEK_BASE_URL")

	c := NewDeepSeekClient("test-key")
	req := openai.ChatCompletionRequest{
		Model: "deepseek-v4-flash",
		Messages: []openai.ChatCompletionMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hi"},
		},
		MaxCompletionTokens: 350,
		Temperature:         0.5,
	}

	resp, err := c.CreateChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateChatCompletion failed: %v", err)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello!" {
		t.Errorf("expected content 'Hello!', got %q", resp.Choices[0].Message.Content)
	}

	// Verify thinking block.
	thinking, ok := capturedBody["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("expected thinking block in request, got: %v", capturedBody["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Errorf("expected thinking.type 'enabled', got %v", thinking["type"])
	}

	// Verify reasoning_effort.
	if capturedBody["reasoning_effort"] != "medium" {
		t.Errorf("expected reasoning_effort 'medium', got %v", capturedBody["reasoning_effort"])
	}

	// Verify standard fields were preserved.
	if capturedBody["model"] != "deepseek-v4-flash" {
		t.Errorf("expected model 'deepseek-v4-flash', got %v", capturedBody["model"])
	}
	if capturedBody["max_completion_tokens"].(float64) != 350 {
		t.Errorf("expected max_completion_tokens 350, got %v", capturedBody["max_completion_tokens"])
	}
	if capturedBody["temperature"].(float64) != 0.5 {
		t.Errorf("expected temperature 0.5, got %v", capturedBody["temperature"])
	}
}

// TestDeepSeekClient_CapturesReasoningContent verifies that the
// reasoning_content field from the response is stashed on the client
// and accessible via ReasoningContent().
func TestDeepSeekClient_CapturesReasoningContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"id": "test",
			"object": "chat.completion",
			"created": 0,
			"model": "deepseek-v4-flash",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Public reply.",
					"reasoning_content": "The user said hi. I should respond politely."
				},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	defer server.Close()

	os.Setenv("DEEPSEEK_BASE_URL", server.URL)
	defer os.Unsetenv("DEEPSEEK_BASE_URL")

	c := NewDeepSeekClient("test-key")
	_ = c // ensure c is used (auto-clear of lastReasoningContent)
	// NewDeepSeekClient doesn't clear lastReasoningContent; clear it now.
	c.lastReasoningContent = ""

	req := openai.ChatCompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "Hi"}},
	}

	resp, err := c.CreateChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateChatCompletion failed: %v", err)
	}

	if resp.Choices[0].Message.Content != "Public reply." {
		t.Errorf("expected visible content 'Public reply.', got %q", resp.Choices[0].Message.Content)
	}

	if got := c.ReasoningContent(); got != "The user said hi. I should respond politely." {
		t.Errorf("expected reasoning content, got %q", got)
	}
}

// TestDeepSeekClient_ReasoningClearedBetweenCalls verifies that
// reasoning content is cleared at the start of each call so callers
// always see only the most recent call's thinking.
func TestDeepSeekClient_ReasoningClearedBetweenCalls(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if callCount == 1 {
			// First call: returns reasoning content.
			w.Write([]byte(`{
				"id": "t", "object": "c", "created": 0, "model": "m",
				"choices": [{"index": 0, "message": {"role": "assistant", "content": "A", "reasoning_content": "thinking-A"}, "finish_reason": "stop"}],
				"usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
			}`))
		} else {
			// Second call: no reasoning content.
			w.Write([]byte(`{
				"id": "t", "object": "c", "created": 0, "model": "m",
				"choices": [{"index": 0, "message": {"role": "assistant", "content": "B"}, "finish_reason": "stop"}],
				"usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
			}`))
		}
	}))
	defer server.Close()

	os.Setenv("DEEPSEEK_BASE_URL", server.URL)
	defer os.Unsetenv("DEEPSEEK_BASE_URL")

	c := NewDeepSeekClient("test-key")
	c.lastReasoningContent = ""

	req := openai.ChatCompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "Hi"}},
	}

	// First call: returns reasoning content.
	_, err := c.CreateChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if c.ReasoningContent() == "" {
		t.Errorf("expected reasoning content from first call, got empty")
	}

	// Second call: no reasoning content. ReasoningContent() should reset.
	_, err = c.CreateChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if c.ReasoningContent() != "" {
		t.Errorf("expected reasoning content to be cleared after second call, got %q", c.ReasoningContent())
	}
}

// TestDeepSeekClient_HandlesHTTPError verifies the client surfaces a
// non-200 status as an error (not silently empty response).
func TestDeepSeekClient_HandlesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": {"message": "invalid model"}}`))
	}))
	defer server.Close()

	os.Setenv("DEEPSEEK_BASE_URL", server.URL)
	defer os.Unsetenv("DEEPSEEK_BASE_URL")

	c := NewDeepSeekClient("test-key")
	c.lastReasoningContent = ""

	req := openai.ChatCompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "Hi"}},
	}

	_, err := c.CreateChatCompletion(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to mention 400, got: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid model") {
		t.Errorf("expected error to include body, got: %v", err)
	}
}
