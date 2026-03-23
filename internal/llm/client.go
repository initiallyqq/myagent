package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"myagent/internal/config"
	"myagent/internal/model"
)

const extractSystemPrompt = `你是一个旅行意图提取助手。从用户输入中提取关键信息，严格返回如下 JSON（不要包含任何 Markdown、解释或多余文字）：
{"dest":"目的地（单个城市或地区，没有则为空字符串）","budget":预算数字（没有则为0）,"gender":"M或F或X或空字符串（M=找男伴，F=找女伴，X或空=不限）","personality_keywords":["性格关键词数组，如E人、内向、佛系等"],"available_month":月份数字（如5代表5月，没有则为0）}`

// Client calls the LLM API to extract structured intent from user input.
type Client struct {
	cfg    *config.LLMConfig
	http   *http.Client
}

func NewClient(cfg *config.LLMConfig) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second},
	}
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	ResponseFormat *respFormat   `json:"response_format,omitempty"`
	Temperature    float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type respFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ExtractIntent calls the LLM to extract a structured Intent from raw user input.
// Falls back to regex extraction if the call times out or returns invalid JSON.
func (c *Client) ExtractIntent(ctx context.Context, userInput string) (*model.Intent, error) {
	payload := chatRequest{
		Model: c.cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: extractSystemPrompt},
			{Role: "user", Content: userInput},
		},
		ResponseFormat: &respFormat{Type: "json_object"},
		Temperature:    0,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		// timeout or network error → signal caller to use regex fallback
		return nil, fmt.Errorf("llm_timeout: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil || len(cr.Choices) == 0 {
		return nil, fmt.Errorf("llm_bad_response: %s", string(data))
	}
	if cr.Error != nil {
		return nil, fmt.Errorf("llm_api_error: %s", cr.Error.Message)
	}

	raw := strings.TrimSpace(cr.Choices[0].Message.Content)
	// strip accidental markdown code fences
	raw = stripCodeFence(raw)

	intent := &model.Intent{}
	if err := json.Unmarshal([]byte(raw), intent); err != nil {
		return nil, fmt.Errorf("llm_parse_json: %w (raw=%q)", err, raw)
	}
	if err := intent.Validate(); err != nil {
		return nil, fmt.Errorf("llm_intent_invalid: %w", err)
	}
	return intent, nil
}

// EmbedText calls the LLM embedding API to produce a vector for given text.
func (c *Client) EmbedText(ctx context.Context, text string) ([]float32, error) {
	type embedReq struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	type embedResp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	payload, _ := json.Marshal(embedReq{Model: c.cfg.EmbeddingModel, Input: text})
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

func stripCodeFence(s string) string {
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
