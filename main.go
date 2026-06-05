package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := loadConfig()

	store, err := newStore(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer store.close()

	if err := store.initSchema(context.Background()); err != nil {
		log.Fatalf("Failed to init schema: %v", err)
	}

	hash, err := hashPassword(cfg.AdminPassword)
	if err != nil {
		log.Fatalf("Failed to hash admin password: %v", err)
	}
	if err := store.upsertAdmin(context.Background(), cfg.AdminUsername, hash); err != nil {
		log.Printf("Warning: failed to init admin user: %v", err)
	}

	if err := seedPosts(store); err != nil {
		log.Printf("Warning: failed to seed posts: %v", err)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(corsMiddleware())
	r.Use(rateLimitMiddleware(20, 40))

	api := r.Group("/api")
	{
		api.GET("/health", handleHealth)
		api.GET("/posts", handlePosts(store))
		api.GET("/posts/:id", handlePostDetail(store))
		api.POST("/posts/:id/verify", handleVerify(store))
		api.GET("/tags", handleTags(store))
		api.GET("/feed.xml", handleFeed(store, cfg))
	}

	admin := r.Group("/api/admin")
	admin.Use(rateLimitMiddleware(5, 10))
	{
		admin.POST("/login", handleAdminLogin(store, cfg))
		admin.GET("/settings", handleAdminGetSettings(store))

		protected := admin.Group("")
		protected.Use(authMiddleware(cfg.JWTSecret))
		{
			protected.GET("/posts", handleAdminListPosts(store))
			protected.POST("/posts", handleAdminCreatePost(store))
			protected.PUT("/posts/:id", handleAdminUpdatePost(store))
			protected.DELETE("/posts/:id", handleAdminDeletePost(store))
			protected.PUT("/settings", handleAdminUpdateSettings(store))
		}
	}

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}
	go func() {
		log.Printf("blog-api listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
}
