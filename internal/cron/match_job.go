package cron

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"myagent/internal/config"
	"myagent/internal/model"
	"myagent/internal/repo"
	"myagent/internal/service"
)

// MatchJob periodically scans demand_pool and notifies users when a new
// user profile matches their pending intent.
type MatchJob struct {
	userRepo   *repo.UserRepo
	demandRepo *repo.DemandRepo
	notifySvc  *service.NotifyService
	cfg        *config.CronConfig
}

func NewMatchJob(
	ur *repo.UserRepo,
	dr *repo.DemandRepo,
	ns *service.NotifyService,
	cfg *config.CronConfig,
) *MatchJob {
	return &MatchJob{
		userRepo:   ur,
		demandRepo: dr,
		notifySvc:  ns,
		cfg:        cfg,
	}
}

// Start launches the cron loop in a background goroutine.
// Call cancel() on the returned context to stop it.
func (j *MatchJob) Start(ctx context.Context) {
	interval := time.Duration(j.cfg.MatchIntervalMinutes) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("cron: match job started", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("cron: match job stopped")
			return
		case <-ticker.C:
			j.run(ctx)
		}
	}
}

// RunOnce triggers a single reverse-match scan. Used for testing and
// on new user registration events.
func (j *MatchJob) RunOnce(ctx context.Context, newUserID int64) {
	j.matchForUser(ctx, newUserID)
}

// run performs the full scan over all pending demands.
func (j *MatchJob) run(ctx context.Context) {
	slog.Info("cron: starting reverse match scan")

	// Expire stale demands first
	expired, err := j.demandRepo.ExpireOld(ctx)
	if err != nil {
		slog.Error("cron: expire old demands failed", "err", err)
	} else if expired > 0 {
		slog.Info("cron: expired old demands", "count", expired)
	}

	demands, err := j.demandRepo.PendingAll(ctx)
	if err != nil {
		slog.Error("cron: fetch pending demands failed", "err", err)
		return
	}
	if len(demands) == 0 {
		return
	}

	// For each pending demand, check if any user matches using PG vector similarity.
	// We delegate the heavy lifting to PG: compute cosine similarity in SQL.
	for _, d := range demands {
		j.matchDemand(ctx, d)
	}
}

func (j *MatchJob) matchDemand(ctx context.Context, d *model.DemandPool) {
	if len(d.IntentVector) == 0 {
		return
	}

	var intent model.Intent
	if err := json.Unmarshal(d.IntentJSON, &intent); err != nil {
		slog.Warn("cron: unmarshal intent failed", "demand_id", d.ID, "err", err)
		return
	}

	q := &model.SearchQuery{
		Intent:    intent,
		Embedding: d.IntentVector,
	}

	users, err := j.userRepo.HybridSearch(ctx, q, 3)
	if err != nil {
		slog.Error("cron: hybrid search for demand failed", "demand_id", d.ID, "err", err)
		return
	}

	for _, u := range users {
		if u.Similarity < j.cfg.MatchSimilarityThreshold {
			continue
		}
		// Exclude the requester themselves
		if u.ID == d.RequesterID {
			continue
		}

		// Fetch the requester's openid
		openid, err := j.demandRepo.GetOpenIDByUserID(ctx, d.RequesterID)
		if err != nil {
			slog.Warn("cron: get requester openid failed", "demand_id", d.ID, "err", err)
			return
		}

		dest := intent.Dest
		if dest == "" && len(u.Destinations) > 0 {
			dest = strings.Join(u.Destinations, "/")
		}

		notifyErr := j.notifySvc.SendMatchNotification(ctx, openid, u.Nickname, dest)
		if notifyErr != nil {
			slog.Warn("cron: notify failed", "openid", openid, "err", notifyErr)
			continue
		}

		// Mark demand as matched after first successful notification
		if err := j.demandRepo.MarkMatched(ctx, d.ID); err != nil {
			slog.Error("cron: mark matched failed", "demand_id", d.ID, "err", err)
		}
		slog.Info("cron: demand matched and notified", "demand_id", d.ID, "openid", openid, "match_user", u.Nickname)
		return // one match per demand per cycle
	}
}

// matchForUser finds demands that are satisfied by a specific newly-registered user.
func (j *MatchJob) matchForUser(ctx context.Context, newUserID int64) {
	demands, err := j.demandRepo.PendingAll(ctx)
	if err != nil || len(demands) == 0 {
		return
	}

	embedding, err := j.userRepo.GetEmbeddingByID(ctx, newUserID)
	if err != nil || len(embedding) == 0 {
		return
	}

	// Batch similarity: ask PG to compute cosine sim between new user and all demand vectors
	demandIDs := make([]int64, len(demands))
	for i, d := range demands {
		demandIDs[i] = d.ID
	}

	sims, err := j.demandRepo.CosineSimilarityBatch(ctx, newUserID, demandIDs)
	if err != nil {
		slog.Error("cron: batch similarity failed", "err", err)
		return
	}

	for _, d := range demands {
		sim, ok := sims[d.ID]
		if !ok || sim < j.cfg.MatchSimilarityThreshold {
			continue
		}
		if d.RequesterID == newUserID {
			continue
		}
		j.matchDemand(ctx, d)
	}
}
