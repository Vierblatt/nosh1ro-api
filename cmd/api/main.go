package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vierblatt/nosh1ro-api/internal/auth"
	"github.com/Vierblatt/nosh1ro-api/internal/cache"
	esclient "github.com/Vierblatt/nosh1ro-api/internal/es"
	"github.com/Vierblatt/nosh1ro-api/internal/config"
	"github.com/Vierblatt/nosh1ro-api/internal/email"
	"github.com/Vierblatt/nosh1ro-api/internal/events"
	"github.com/Vierblatt/nosh1ro-api/internal/handler"
	"github.com/Vierblatt/nosh1ro-api/internal/middleware"
	"github.com/Vierblatt/nosh1ro-api/internal/seed"
	"github.com/Vierblatt/nosh1ro-api/internal/store"
	"github.com/gin-gonic/gin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}

	db, err := store.New(cfg.DBDSN)
	if err != nil {
		slog.Error("database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.InitSchema(context.Background()); err != nil {
		slog.Error("schema", "error", err)
		os.Exit(1)
	}
	if err := db.MigrateAdminSchema(context.Background()); err != nil {
		slog.Error("admin migration", "error", err)
		os.Exit(1)
	}

	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		slog.Error("admin password", "error", err)
		os.Exit(1)
	}
	if err := db.UpsertAdmin(context.Background(), cfg.AdminUsername, hash); err != nil {
		slog.Warn("admin init", "error", err)
	}

	if err := seed.Posts(db); err != nil {
		slog.Warn("seed", "error", err)
	}

	// ---- Infrastructure ----

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	var esCli *esclient.Client
	if cfg.ESURL != "" {
		cli, err := esclient.New(cfg.ESURL)
		if err != nil {
			slog.Warn("elasticsearch", "error", err)
		} else {
			if err := cli.EnsureIndex(appCtx); err != nil {
				slog.Warn("es ensure index", "error", err)
			}
			esCli = cli
			slog.Info("elasticsearch connected")
		}
	}

	var cacheCli *cache.Client
	if cfg.RedisURL != "" {
		cli, err := cache.New(cfg.RedisURL)
		if err != nil {
			slog.Warn("redis", "error", err)
		} else {
			defer cli.Close()
			cacheCli = cli
			slog.Info("redis connected")

			go func() {
				ticker := time.NewTicker(10 * time.Minute)
				defer ticker.Stop()
				for {
					select {
					case <-appCtx.Done():
						return
					case <-ticker.C:
						cacheCli.RefreshHotPosts(appCtx)
					}
				}
			}()
		}
	}

	var eventsCli *events.Client
	if cfg.RabbitMQURL != "" {
		cli, err := events.New(cfg.RabbitMQURL)
		if err != nil {
			slog.Warn("rabbitmq", "error", err)
		} else {
			defer cli.Close()
			eventsCli = cli
			slog.Info("rabbitmq connected")

			if esCli != nil {
				if err := cli.Consume(appCtx, events.QueueESSync, func(ctx context.Context, ev events.PostEvent) error {
					switch ev.Type {
					case "post.created", "post.updated":
						p, err := db.FindPost(ctx, ev.ID)
						if err != nil {
							return err
						}
						return esCli.IndexPost(ctx, p)
					case "post.deleted":
						return esCli.DeletePost(ctx, ev.ID)
					}
					return nil
				}); err != nil {
					slog.Warn("es consumer", "error", err)
				}
				slog.Info("es consumer started")
			}

			if cacheCli != nil {
				if err := cli.Consume(appCtx, events.QueueCache, func(ctx context.Context, ev events.PostEvent) error {
					return cacheCli.DeletePattern(ctx, "posts:list:*")
				}); err != nil {
					slog.Warn("cache consumer", "error", err)
				}
				slog.Info("cache consumer started")
			}
		}
	}

	// ---- Router ----

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.SlogLogger())
	r.Use(middleware.RequestID())
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())

	if cacheCli != nil {
		r.Use(redisRateLimit(cacheCli))
	} else {
		r.Use(middleware.RateLimit(20, 40))
	}

	postCtrl := handler.NewPostController(db, esCli, cacheCli)
	postCtrl.Register(r.Group("/api"))

	emailCfg := &email.Config{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.SMTPFrom,
		BaseURL:  cfg.BaseURL,
	}

	authCtrl := handler.NewAuthController(db, cfg.JWTSecret, emailCfg)
	authCtrl.Register(r.Group("/api"))

	adminCtrl := handler.NewAdminController(db, db, cfg.JWTSecret, esCli, cacheCli, eventsCli)
	adminGroup := r.Group("/api/admin")
	if cacheCli != nil {
		adminGroup.Use(redisRateLimit(cacheCli))
	} else {
		adminGroup.Use(middleware.RateLimit(5, 10))
	}
	adminCtrl.Register(adminGroup)

	feedCtrl := handler.NewFeedController(db, db, cfg.BlogTitle)
	r.GET("/api/feed.xml", feedCtrl.Handle)

	// ---- Server ----

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}
}

func redisRateLimit(cacheCli *cache.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := "ratelimit:" + c.ClientIP() + ":" + c.Request.URL.Path
		if !cacheCli.Allow(c.Request.Context(), key, 20, time.Second) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}
