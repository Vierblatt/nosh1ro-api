package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func handleAdminLogin(store *Store, cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		if req.Username != cfg.AdminUsername {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}

		admin, err := store.findAdmin(c.Request.Context(), req.Username)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		if err := checkPassword(admin.PasswordHash, req.Password); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}

		token, err := generateToken(cfg.JWTSecret, admin.Username)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token})
	}
}

func handleAdminListPosts(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
		size, _ := strconv.ParseInt(c.DefaultQuery("size", "20"), 10, 64)
		if page < 1 {
			page = 1
		}
		if size < 1 || size > 50 {
			size = 20
		}

		filter := PostFilter{
			Status:   c.Query("status"),
			Tag:      c.Query("tag"),
			Category: c.Query("category"),
			Search:   c.Query("q"),
		}

		result, err := store.findPosts(c.Request.Context(), filter, page, size)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch posts"})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func handleAdminCreatePost(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			ID        string          `json:"id"`
			Title     string          `json:"title"`
			Content   string          `json:"content"`
			Date      string          `json:"date"`
			Tags      []string        `json:"tags"`
			Category  string          `json:"category"`
			Status    string          `json:"status"`
			Encrypted bool            `json:"encrypted"`
			Encrypt   *EncryptionData `json:"encryption,omitempty"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		if req.ID == "" || req.Title == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id and title are required"})
			return
		}
		if store.postExists(c.Request.Context(), req.ID) {
			c.JSON(http.StatusConflict, gin.H{"error": "post already exists"})
			return
		}
		if req.Status == "" {
			req.Status = "draft"
		}
		if req.Date == "" {
			req.Date = time.Now().Format("2006-01-02")
		}
		if req.Tags == nil {
			req.Tags = []string{}
		}

		html := renderMarkdown(req.Content)
		summary := extractSummary(html, 200)
		now := time.Now()

		p := Post{
			ID:          req.ID,
			Title:       req.Title,
			Content:     req.Content,
			ContentHTML: html,
			Summary:     summary,
			Date:        req.Date,
			Tags:        req.Tags,
			Category:    req.Category,
			Status:      req.Status,
			Encrypted:   req.Encrypted,
			Encryption:  req.Encrypt,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		if err := store.insertPost(c.Request.Context(), &p); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create post"})
			return
		}
		c.JSON(http.StatusCreated, p)
	}
}

func handleAdminUpdatePost(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		existing, err := store.findPost(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}

		var req struct {
			Title     *string          `json:"title"`
			Content   *string          `json:"content"`
			Date      *string          `json:"date"`
			Tags      *[]string        `json:"tags"`
			Category  *string          `json:"category"`
			Status    *string          `json:"status"`
			Encrypted *bool            `json:"encrypted"`
			Encrypt   *EncryptionData  `json:"encryption"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}

		if req.Title != nil {
			existing.Title = *req.Title
		}
		if req.Content != nil {
			existing.Content = *req.Content
			existing.ContentHTML = renderMarkdown(*req.Content)
			existing.Summary = extractSummary(existing.ContentHTML, 200)
		}
		if req.Date != nil {
			existing.Date = *req.Date
		}
		if req.Tags != nil {
			existing.Tags = *req.Tags
		}
		if req.Category != nil {
			existing.Category = *req.Category
		}
		if req.Status != nil {
			existing.Status = *req.Status
		}
		if req.Encrypted != nil {
			existing.Encrypted = *req.Encrypted
		}
		if req.Encrypt != nil {
			existing.Encryption = req.Encrypt
		}
		existing.UpdatedAt = time.Now()

		if err := store.replacePost(c.Request.Context(), existing); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update post"})
			return
		}
		c.JSON(http.StatusOK, existing)
	}
}

func handleAdminDeletePost(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if err := store.deletePost(c.Request.Context(), id); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete post"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "deleted"})
	}
}

func handleAdminGetSettings(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		bs, err := store.getSettings(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get settings"})
			return
		}
		c.JSON(http.StatusOK, bs)
	}
}

func handleAdminUpdateSettings(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Title    string `json:"title"`
			Subtitle string `json:"subtitle"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		bs := &BlogSettings{
			Title:    strings.TrimSpace(req.Title),
			Subtitle: strings.TrimSpace(req.Subtitle),
		}
		if err := store.upsertSettings(c.Request.Context(), bs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update settings"})
			return
		}
		c.JSON(http.StatusOK, bs)
	}
}
