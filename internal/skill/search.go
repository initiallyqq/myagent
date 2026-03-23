package skill

import (
	"context"
	"encoding/json"
	"strings"

	"myagent/internal/model"
	"myagent/internal/service"
)

// SearchArgs matches the MCP tool definition for search_companions.
type SearchArgs struct {
	Dest                string   `json:"dest"`
	Gender              string   `json:"gender"`
	Budget              int      `json:"budget"`
	PersonalityKeywords []string `json:"personality_keywords"`
	AvailableMonth      int      `json:"available_month"`
}

// SearchResult is the serialised output returned to the LLM.
type SearchResult struct {
	Found     bool              `json:"found"`
	Count     int               `json:"count"`
	Users     []*model.User     `json:"users,omitempty"`
	Degraded  bool              `json:"degraded,omitempty"`
	Region    bool              `json:"region,omitempty"`
	Suspended bool              `json:"suspended,omitempty"`
	DemandID  int64             `json:"demand_id,omitempty"`
}

// SearchSkill wraps the existing SearchService and exposes it as a Skill.
type SearchSkill struct {
	searchSvc   *service.SearchService
	intentSvc   *service.IntentService
	requesterID int64 // set by the orchestrator per request
}

func NewSearchSkill(ss *service.SearchService, is *service.IntentService) *SearchSkill {
	return &SearchSkill{searchSvc: ss, intentSvc: is}
}

// SetRequester injects the current user ID (call before Execute per request).
func (s *SearchSkill) SetRequester(id int64) { s.requesterID = id }

func (s *SearchSkill) Name() string { return "search_companions" }

func (s *SearchSkill) Execute(ctx context.Context, raw json.RawMessage) (any, error) {
	var args SearchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}

	intent := &model.Intent{
		Dest:                args.Dest,
		Gender:              strings.ToUpper(args.Gender),
		Budget:              args.Budget,
		PersonalityKeywords: args.PersonalityKeywords,
		AvailableMonth:      args.AvailableMonth,
	}
	_ = intent.Validate()

	// generate embedding for vector search
	embedding, _ := s.intentSvc.EmbedIntent(ctx, intent)
	q := &model.SearchQuery{Intent: *intent, Embedding: embedding}

	result, err := s.searchSvc.Search(ctx, q, s.requesterID)
	if err != nil {
		return nil, err
	}

	return &SearchResult{
		Found:     len(result.Users) > 0,
		Count:     len(result.Users),
		Users:     result.Users,
		Degraded:  result.Degraded,
		Region:    result.Region,
		Suspended: result.Suspended,
		DemandID:  result.DemandID,
	}, nil
}
