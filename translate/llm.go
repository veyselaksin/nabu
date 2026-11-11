package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Message is one chat message.
type Message struct {
	Role    string `json:"role"` // "system", "user" or "assistant"
	Content string `json:"content"`
}

// ChatOptions tunes one completion.
type ChatOptions struct {
	MaxTokens int
	// JSON asks the server to constrain the reply to a JSON object.
	JSON bool
}

// Chatter is a chat-completion backend. *Client implements it.
type Chatter interface {
	Chat(ctx context.Context, messages []Message, opts ChatOptions) (string, error)
}

// Client talks to any server implementing the OpenAI chat-completions API,
// such as llama.cpp's llama-server or Ollama. It sends no data anywhere else.
type Client struct {
	// BaseURL is the API root, e.g. http://127.0.0.1:8090/v1.
	BaseURL string
	// Model is sent as the "model" field (llama-server ignores it; Ollama
	// needs a pulled model name such as qwen3:4b-instruct).
	Model string
	// APIKey, if set, is sent as a bearer token.
	APIKey string
	HTTP   *http.Client
}

// LLMError is a failed or unusable reply from the chat backend.
type LLMError struct {
	Status int // HTTP status, or 0 if the request did not complete
	Msg    string
	Err    error
}

func (e *LLMError) Error() string {
	switch {
	case e.Status != 0:
		return fmt.Sprintf("language model server: HTTP %d: %s", e.Status, e.Msg)
	case e.Err != nil:
		return fmt.Sprintf("language model server: %s: %v", e.Msg, e.Err)
	}
	return "language model server: " + e.Msg
}

func (e *LLMError) Unwrap() error { return e.Err }

const maxReplyBytes = 4 << 20

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) endpoint(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + path
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	return req, nil
}

// Chat sends one chat-completion request (temperature 0) and returns the
// reply text.
func (c *Client) Chat(ctx context.Context, messages []Message, opts ChatOptions) (string, error) {
	body := map[string]any{
		"model":       c.Model,
		"messages":    messages,
		"temperature": 0,
		"stream":      false,
	}
	if c.Model == "" {
		body["model"] = "default"
	}
	if opts.MaxTokens > 0 {
		body["max_tokens"] = opts.MaxTokens
	}
	if opts.JSON {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", &LLMError{Msg: "bad request", Err: err}
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return "", &LLMError{Msg: "request failed", Err: err}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxReplyBytes))
	if err != nil {
		return "", &LLMError{Msg: "reading reply", Err: err}
	}
	if res.StatusCode/100 != 2 {
		return "", &LLMError{Status: res.StatusCode, Msg: snippet(raw)}
	}
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return "", &LLMError{Msg: "reply is not a chat completion", Err: err}
	}
	if len(reply.Choices) == 0 {
		return "", &LLMError{Msg: "reply has no choices"}
	}
	return reply.Choices[0].Message.Content, nil
}

// Ping checks that the server answers GET /models.
func (c *Client) Ping(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return &LLMError{Msg: "bad request", Err: err}
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return &LLMError{Msg: "not reachable at " + c.BaseURL, Err: err}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return &LLMError{Status: res.StatusCode, Msg: "GET /models failed"}
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// decodeObject parses the first JSON object in a model reply. Models
// sometimes wrap JSON in prose or Markdown fences even when asked not to.
func decodeObject(reply string, dst any) error {
	// Reasoning models may prefix their answer with a <think> block.
	if i := strings.LastIndex(reply, "</think>"); i >= 0 {
		reply = reply[i+len("</think>"):]
	}
	start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if start < 0 || end < start {
		return errors.New("reply contains no JSON object")
	}
	return json.Unmarshal([]byte(reply[start:end+1]), dst)
}
