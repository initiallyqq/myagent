package memory

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"myagent/internal/llm"
)

const extractMemorySystemPrompt = `你是一个记忆提取助手。从下面的对话中提取值得长期记住的用户偏好或事实（每条一句话，中文），严格返回 JSON 数组，不要包含任何 Markdown：
["记忆1","记忆2"]
如果没有值得记忆的信息，返回空数组：[]`

// Manager orchestrates memory extraction, embedding, and retrieval.
type Manager struct {
	store     *Store
	llmClient *llm.Client
}

func NewManager(store *Store, lc *llm.Client) *Manager {
	return &Manager{store: store, llmClient: lc}
}

// ExtractAndSave calls the LLM to identify memorable facts from a conversation turn,
// embeds each fact, and persists them. Non-blocking best-effort: errors are logged only.
func (m *Manager) ExtractAndSave(ctx context.Context, userID int64, userInput, agentReply string) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	dialogue := "用户: " + userInput + "\n助手: " + agentReply
	reply, err := m.llmClient.Chat(ctx, []llm.Message{
		{Role: "system", Content: extractMemorySystemPrompt},
		{Role: "user", Content: dialogue},
	}, nil)
	if err != nil {
		slog.Warn("memory: extraction LLM call failed", "err", err)
		return
	}

	raw := strings.TrimSpace(reply.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var facts []string
	if err := json.Unmarshal([]byte(raw), &facts); err != nil {
		slog.Warn("memory: parse facts failed", "raw", raw, "err", err)
		return
	}

	for _, fact := range facts {
		fact = strings.TrimSpace(fact)
		if fact == "" {
			continue
		}
		// embed the fact
		embedCtx, embedCancel := context.WithTimeout(ctx, 5*time.Second)
		vec, embedErr := m.llmClient.EmbedText(embedCtx, fact)
		embedCancel()
		if embedErr != nil {
			slog.Warn("memory: embed failed, saving without vector", "err", embedErr)
		}
		if _, saveErr := m.store.Save(ctx, userID, fact, "chat", vec); saveErr != nil {
			slog.Warn("memory: save failed", "err", saveErr)
		}
	}
}

// RetrieveContext fetches the top-k relevant memories and formats them for prompt injection.
func (m *Manager) RetrieveContext(ctx context.Context, userID int64, queryText string) string {
	if userID == 0 {
		return ""
	}
	// embed the query for semantic retrieval
	var queryVec []float32
	embedCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	vec, err := m.llmClient.EmbedText(embedCtx, queryText)
	cancel()
	if err == nil {
		queryVec = vec
	}

	mems, err := m.store.RetrieveRelevant(ctx, userID, queryVec, 5)
	if err != nil {
		slog.Warn("memory: retrieve failed", "err", err)
		return ""
	}
	return FormatForPrompt(mems)
}
