package skill

import (
	"context"
	"encoding/json"

	"myagent/internal/memory"
)

// MemoryArgs matches the MCP tool definition for save_memory.
type MemoryArgs struct {
	Content string `json:"content"`
}

// MemorySkill persists a user preference or fact to the long-term memory store.
type MemorySkill struct {
	memManager  *memory.Manager
	memStore    *memory.Store
	llmEmbed    interface {
		EmbedText(ctx context.Context, text string) ([]float32, error)
	}
	userID int64 // set per request by orchestrator
}

func NewMemorySkill(mgr *memory.Manager, store *memory.Store) *MemorySkill {
	return &MemorySkill{memManager: mgr, memStore: store}
}

func (s *MemorySkill) SetUserID(id int64) { s.userID = id }

func (s *MemorySkill) Name() string { return "save_memory" }

func (s *MemorySkill) Execute(ctx context.Context, raw json.RawMessage) (any, error) {
	var args MemoryArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if args.Content == "" || s.userID == 0 {
		return map[string]any{"saved": false, "reason": "no content or user"}, nil
	}

	// Save without embedding (embedding happens inside ExtractAndSave for full turns;
	// here the LLM already gave us a crisp fact).
	_, err := s.memStore.Save(ctx, s.userID, args.Content, "agent", nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"saved": true, "content": args.Content}, nil
}
