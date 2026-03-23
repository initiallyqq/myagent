package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"myagent/internal/config"
	"myagent/internal/model"
)

// ──────────────────────────────────────────────────────────────
// Shared types
// ──────────────────────────────────────────────────────────────

// Message represents a single turn in the conversation.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // role=tool
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // role=assistant
	Name       string     `json:"name,omitempty"`
}

// ToolCall represents a tool invocation returned by the LLM.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // raw JSON string
	} `json:"function"`
}

// Tool defines a callable tool exposed to the LLM (OpenAI-compatible schema).
type Tool struct {
	Type     string   `json:"type"` // "function"
	Function FuncSpec `json:"function"`
}

// FuncSpec is the JSON Schema description of a tool.
type FuncSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatReply is the response from a chat call.
type ChatReply struct {
	Content   string     // final text content (when no tool calls)
	ToolCalls []ToolCall // tool calls to execute
}

// ──────────────────────────────────────────────────────────────
// Client
// ──────────────────────────────────────────────────────────────

// Client calls the LLM API for intent extraction, embedding, and function calling.
type Client struct {
	cfg  *config.LLMConfig
	http *http.Client
}

func NewClient(cfg *config.LLMConfig) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second},
	}
}

// ──────────────────────────────────────────────────────────────
// Chat with Function Calling (core method)
// ──────────────────────────────────────────────────────────────

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  string    `json:"tool_choice,omitempty"` // "auto" | "none"
	Temperature float64   `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Chat sends a multi-turn conversation to the LLM, optionally with tools.
// Returns either a final text reply or tool calls for the caller to execute.
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (*ChatReply, error) {
	payload := chatRequest{
		Model:       c.cfg.Model,
		Messages:    messages,
		Temperature: 0,
	}
	if len(tools) > 0 {
		payload.Tools = tools
		payload.ToolChoice = "auto"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm_timeout: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	slog.Debug("llm chat raw response", "status", resp.StatusCode, "body", string(data))
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil || len(cr.Choices) == 0 {
		return nil, fmt.Errorf("llm_bad_response: %s", string(data))
	}
	if cr.Error != nil {
		return nil, fmt.Errorf("llm_api_error: %s", cr.Error.Message)
	}

	choice := cr.Choices[0].Message
	return &ChatReply{
		Content:   strings.TrimSpace(choice.Content),
		ToolCalls: choice.ToolCalls,
	}, nil
}

// ──────────────────────────────────────────────────────────────
// Legacy: ExtractIntent (used as fallback when Function Call fails)
// ──────────────────────────────────────────────────────────────

const extractSystemPrompt = `你是一个旅行意图提取助手。从用户输入中提取关键信息，严格返回如下 JSON（不要包含任何 Markdown、解释或多余文字）：
{"dest":"目的地（单个城市或地区，没有则为空字符串）","budget":预算数字（没有则为0）,"gender":"M或F或X或空字符串（M=找男伴，F=找女伴，X或空=不限）","personality_keywords":["性格关键词数组，如E人、内向、佛系等"],"available_month":月份数字（如5代表5月，没有则为0）}`

func (c *Client) ExtractIntent(ctx context.Context, userInput string) (*model.Intent, error) {
	reply, err := c.Chat(ctx, []Message{
		{Role: "system", Content: extractSystemPrompt},
		{Role: "user", Content: userInput},
	}, nil)
	if err != nil {
		return nil, err
	}

	raw := stripCodeFence(reply.Content)
	intent := &model.Intent{}
	if err := json.Unmarshal([]byte(raw), intent); err != nil {
		return nil, fmt.Errorf("llm_parse_json: %w (raw=%q)", err, raw)
	}
	if err := intent.Validate(); err != nil {
		return nil, fmt.Errorf("llm_intent_invalid: %w", err)
	}
	return intent, nil
}

// ──────────────────────────────────────────────────────────────
// Embedding
// ──────────────────────────────────────────────────────────────

func (c *Client) EmbedText(ctx context.Context, text string) ([]float32, error) {
	if c.cfg.EmbeddingModel == "" || c.cfg.EmbeddingURL == "" {
		return nil, fmt.Errorf("embed_disabled: no embedding model configured")
	}
	type embedReq struct {
		Model     string `json:"model"`
		Input     string `json:"input"`
		Dimension int    `json:"dimension,omitempty"`
	}
	type embedResp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	payload, _ := json.Marshal(embedReq{Model: c.cfg.EmbeddingModel, Input: text, Dimension: c.cfg.EmbeddingDim})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.EmbeddingURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed_timeout: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var er embedResp
	if err := json.Unmarshal(data, &er); err != nil || len(er.Data) == 0 {
		return nil, fmt.Errorf("embed_bad_response: %s", string(data))
	}
	if er.Error != nil {
		return nil, fmt.Errorf("embed_api_error: %s", er.Error.Message)
	}
	return er.Data[0].Embedding, nil
}

// ──────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────

func stripCodeFence(s string) string {
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
