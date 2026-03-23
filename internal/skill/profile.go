package skill

import (
	"context"
	"encoding/json"

	"myagent/internal/model"
	"myagent/internal/repo"
)

// ProfileSkill returns the current user's profile for personalised context.
type ProfileSkill struct {
	userRepo *repo.UserRepo
	openID   string // set per request by orchestrator
}

func NewProfileSkill(ur *repo.UserRepo) *ProfileSkill {
	return &ProfileSkill{userRepo: ur}
}

func (s *ProfileSkill) SetOpenID(openID string) { s.openID = openID }

func (s *ProfileSkill) Name() string { return "get_user_profile" }

func (s *ProfileSkill) Execute(ctx context.Context, _ json.RawMessage) (any, error) {
	if s.openID == "" {
		return map[string]any{"found": false}, nil
	}
	u, err := s.userRepo.GetByOpenID(ctx, s.openID)
	if err != nil {
		return map[string]any{"found": false, "reason": err.Error()}, nil
	}
	return &model.User{
		ID:           u.ID,
		Nickname:     u.Nickname,
		Gender:       u.Gender,
		Tags:         u.Tags,
		Destinations: u.Destinations,
		BudgetMin:    u.BudgetMin,
		BudgetMax:    u.BudgetMax,
	}, nil
}
