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
	"github.com/Vierblatt/nosh1ro-api/internal/config"
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

	db, err := store.New(cfg.DBPath)
	if err != nil {
		slog.Error("database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.InitSchema(context.Background()); err != nil {
		slog.Error("schema", "error", err)
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

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.SlogLogger())
	r.Use(middleware.RequestID())
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.RateLimit(20, 40))

	postCtrl := handler.NewPostController(db)
	postCtrl.Register(r.Group("/api"))

	adminCtrl := handler.NewAdminController(db, db, cfg.JWTSecret)
	adminGroup := r.Group("/api/admin")
	adminGroup.Use(middleware.RateLimit(5, 10))
	adminCtrl.Register(adminGroup)

	feedCtrl := handler.NewFeedController(db, db, cfg.BlogTitle)
	r.GET("/api/feed.xml", feedCtrl.Handle)

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
