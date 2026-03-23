package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"myagent/internal/config"
	"myagent/internal/cron"
	"myagent/internal/handler"
	"myagent/internal/llm"
	"myagent/internal/middleware"
	"myagent/internal/repo"
	"myagent/internal/service"
)

func main() {
	// ── Config ─────────────────────────────────────────────────────────────
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "config/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	// ── PostgreSQL ──────────────────────────────────────────────────────────
	db, err := sql.Open("postgres", cfg.Database.DSN)
	if err != nil {
		slog.Error("postgres open failed", "err", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		slog.Error("postgres ping failed", "err", err)
		os.Exit(1)
	}
	slog.Info("postgres connected")

	// ── Redis ───────────────────────────────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})
	rPingCtx, rPingCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer rPingCancel()
	if err := rdb.Ping(rPingCtx).Err(); err != nil {
		slog.Error("redis ping failed", "err", err)
		os.Exit(1)
	}
	slog.Info("redis connected")

	// ── Repositories ────────────────────────────────────────────────────────
	userRepo := repo.NewUserRepo(db)
	demandRepo := repo.NewDemandRepo(db)

	// ── LLM client ──────────────────────────────────────────────────────────
	llmClient := llm.NewClient(&cfg.LLM)

	// ── Services ────────────────────────────────────────────────────────────
	cacheSvc := service.NewCacheService(rdb, cfg.Cache.TTLSeconds)
	intentSvc := service.NewIntentService(llmClient)
	searchSvc := service.NewSearchService(userRepo, demandRepo, llmClient)
	notifySvc := service.NewNotifyService(&cfg.WeChat)

	// ── Cron ────────────────────────────────────────────────────────────────
	matchJob := cron.NewMatchJob(userRepo, demandRepo, notifySvc, &cfg.Cron)
	cronCtx, cronCancel := context.WithCancel(context.Background())
	go matchJob.Start(cronCtx)

	// ── HTTP router ─────────────────────────────────────────────────────────
	router := buildRouter(cfg, rdb, intentSvc, searchSvc, cacheSvc, userRepo, llmClient, notifySvc)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: router,
	}

	// ── Graceful shutdown ───────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("server starting", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down...")
	cronCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
	_ = db.Close()
	_ = rdb.Close()
	slog.Info("server exited")
}

func buildRouter(
	cfg *config.Config,
	rdb *redis.Client,
	intentSvc *service.IntentService,
	searchSvc *service.SearchService,
	cacheSvc *service.CacheService,
	userRepo *repo.UserRepo,
	llmClient *llm.Client,
	notifySvc *service.NotifyService,
) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())
	router.Use(middleware.Timeout(time.Duration(cfg.Server.TimeoutSeconds) * time.Second))

	// Health check (no rate limit)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "time": time.Now().Format(time.RFC3339)})
	})

	api := router.Group("/api/v1")
	api.Use(middleware.RateLimit(rdb, cfg.RateLimit.RequestsPerMinute))

	searchH := handler.NewSearchHandler(intentSvc, searchSvc, cacheSvc)
	api.POST("/search", searchH.Handle)

	userH := handler.NewUserHandler(userRepo, llmClient)
	api.POST("/user/register", userH.Register)

	subscribeH := handler.NewSubscribeHandler(notifySvc)
	api.POST("/subscribe", subscribeH.Subscribe)

	return router
}
