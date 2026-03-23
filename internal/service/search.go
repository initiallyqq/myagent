package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"myagent/internal/llm"
	"myagent/internal/model"
	"myagent/internal/repo"
)

const defaultResultLimit = 5

// regionMap expands a specific destination to a broader region for fallback search.
var regionMap = map[string]string{
	"西藏":   "大西北",
	"新疆":   "大西北",
	"青海":   "大西北",
	"敦煌":   "大西北",
	"拉萨":   "大西北",
	"大理":   "云南",
	"丽江":   "云南",
	"香格里拉": "云南",
	"稻城":   "四川",
	"成都":   "四川",
	"桂林":   "广西",
	"三亚":   "海南",
}

// SearchResult is returned by the search service.
type SearchResult struct {
	Users    []*model.User
	Degraded bool   // true when running with relaxed constraints
	Region   bool   // true when dest was expanded to region
	Suspended bool  // true when demand was pushed to demand_pool
	DemandID int64
}

// SearchService orchestrates hybrid retrieval and graceful degradation.
type SearchService struct {
	userRepo   *repo.UserRepo
	demandRepo *repo.DemandRepo
	llmClient  *llm.Client
}

func NewSearchService(ur *repo.UserRepo, dr *repo.DemandRepo, lc *llm.Client) *SearchService {
	return &SearchService{userRepo: ur, demandRepo: dr, llmClient: lc}
}

// Search runs the full retrieval pipeline for a given intent.
// Caller is responsible for providing the requesterID (0 = anonymous).
func (s *SearchService) Search(ctx context.Context, q *model.SearchQuery, requesterID int64) (*SearchResult, error) {
	// Step 1: strict search
	users, err := s.userRepo.HybridSearch(ctx, q, defaultResultLimit)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: %w", err)
	}
	if len(users) > 0 {
		return &SearchResult{Users: users}, nil
	}

	// Step 2: relax budget + time constraint
	slog.Info("search: no results, relaxing constraints", "dest", q.Dest)
	relaxed := *q
	relaxed.Relaxed = true
	relaxed.Budget = 0
	users, err = s.userRepo.HybridSearch(ctx, &relaxed, defaultResultLimit)
	if err != nil {
		return nil, fmt.Errorf("relaxed search: %w", err)
	}
	if len(users) > 0 {
		return &SearchResult{Users: users, Degraded: true}, nil
	}

	// Step 3: expand destination to region
	if region, ok := regionMap[q.Dest]; ok {
		slog.Info("search: expanding dest to region", "dest", q.Dest, "region", region)
		regional := relaxed
		regional.Dest = region
		regional.RegionOnly = true
		users, err = s.userRepo.HybridSearch(ctx, &regional, defaultResultLimit)
		if err != nil {
			return nil, fmt.Errorf("regional search: %w", err)
		}
		if len(users) > 0 {
			return &SearchResult{Users: users, Degraded: true, Region: true}, nil
		}
	}

	// Step 4: intent suspension — write to demand_pool
	if requesterID > 0 {
		demandID, suspendErr := s.suspend(ctx, requesterID, &q.Intent, q.Embedding)
		if suspendErr != nil {
			slog.Warn("search: failed to suspend demand", "err", suspendErr)
		}
		return &SearchResult{Suspended: true, DemandID: demandID}, nil
	}

	return &SearchResult{Suspended: true}, nil
}

func (s *SearchService) suspend(ctx context.Context, requesterID int64, intent *model.Intent, embedding []float32) (int64, error) {
	// embed the intent keywords if we have them but no vector yet
	if len(embedding) == 0 && len(intent.PersonalityKeywords) > 0 {
		kw := strings.Join(intent.PersonalityKeywords, " ")
		embedCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		vec, err := s.llmClient.EmbedText(embedCtx, kw)
		if err == nil {
			embedding = vec
		}
	}
	return s.demandRepo.Insert(ctx, requesterID, intent, embedding)
}
