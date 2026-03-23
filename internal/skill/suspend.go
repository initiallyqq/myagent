package skill

import (
	"context"
	"encoding/json"

	"myagent/internal/model"
	"myagent/internal/repo"
)

// SuspendArgs matches the MCP tool definition for suspend_demand.
type SuspendArgs struct {
	Dest                string   `json:"dest"`
	Gender              string   `json:"gender"`
	Budget              int      `json:"budget"`
	PersonalityKeywords []string `json:"personality_keywords"`
	AvailableMonth      int      `json:"available_month"`
}

// SuspendSkill writes an unfulfilled user intent into the demand_pool.
type SuspendSkill struct {
	demandRepo  *repo.DemandRepo
	requesterID int64
}

func NewSuspendSkill(dr *repo.DemandRepo) *SuspendSkill {
	return &SuspendSkill{demandRepo: dr}
}

func (s *SuspendSkill) SetRequester(id int64) { s.requesterID = id }

func (s *SuspendSkill) Name() string { return "suspend_demand" }

func (s *SuspendSkill) Execute(ctx context.Context, raw json.RawMessage) (any, error) {
	var args SuspendArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if s.requesterID == 0 {
		return map[string]any{"suspended": false, "reason": "user not logged in"}, nil
	}

	intent := &model.Intent{
		Dest:                args.Dest,
		Gender:              args.Gender,
		Budget:              args.Budget,
		PersonalityKeywords: args.PersonalityKeywords,
		AvailableMonth:      args.AvailableMonth,
	}

	id, err := s.demandRepo.Insert(ctx, s.requesterID, intent, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"suspended": true, "demand_id": id}, nil
}
