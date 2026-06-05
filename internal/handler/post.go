package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Vierblatt/nosh1ro-api/internal/cache"
	esclient "github.com/Vierblatt/nosh1ro-api/internal/es"
	"github.com/Vierblatt/nosh1ro-api/internal/crypto"
	"github.com/Vierblatt/nosh1ro-api/internal/model"
	"github.com/gin-gonic/gin"
)

type PostController struct {
	store PostStore
	es    *esclient.Client
	cache *cache.Client
}

func NewPostController(store PostStore, es *esclient.Client, c *cache.Client) *PostController {
	return &PostController{store: store, es: es, cache: c}
}

func (pc *PostController) Register(api *gin.RouterGroup) {
	api.GET("/health", pc.handleHealth)
	api.GET("/posts", pc.handlePosts)
	api.POST("/posts/search", pc.handleSearch)
	api.GET("/posts/:id", pc.handlePostDetail)
	api.POST("/posts/:id/verify", pc.handleVerify)
	api.GET("/tags", pc.handleTags)
}

func (pc *PostController) handleHealth(c *gin.Context) {
	ctx := c.Request.Context()
	dbOK := pc.store.Ping(ctx) == nil
	resp := gin.H{"status": "ok", "db": boolStatus(dbOK)}
	if pc.cache != nil {
		resp["redis"] = boolStatus(pc.cache.IsReady(ctx))
	}
	if pc.es != nil {
		resp["es"] = boolStatus(pc.es.IsReady(ctx))
	}
	if !dbOK {
		resp["status"] = "degraded"
	}
	c.JSON(http.StatusOK, resp)
}

func boolStatus(ok bool) string {
	if ok {
		return "connected"
	}
	return "disconnected"
}

func (pc *PostController) handleSearch(c *gin.Context) {
	var req struct {
		Q        string `json:"q"`
		Category string `json:"category"`
		Tag      string `json:"tag"`
		Page     int64  `json:"page"`
		Size     int64  `json:"size"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Page = 1
		req.Size = 10
	}
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 || req.Size > 50 {
		req.Size = 10
	}

	if pc.es != nil {
		result, err := pc.es.SearchPosts(c.Request.Context(), req.Q, req.Category, req.Tag, req.Page, req.Size)
		if err != nil {
			slog.Error("es search", "error", err)
			respondError(c, ErrInternal)
			return
		}
		if result.Posts == nil {
			result.Posts = []esclient.SearchHit{}
		}
		c.JSON(http.StatusOK, result)
		return
	}

	filter := model.PostFilter{
		Status:   string(model.StatusPublished),
		Tag:      req.Tag,
		Category: req.Category,
		Search:   req.Q,
	}
	result, err := pc.store.FindPosts(c.Request.Context(), filter, req.Page, req.Size)
	if err != nil {
		slog.Error("search fallback", "error", err)
		respondError(c, ErrInternal)
		return
	}
	hits := make([]esclient.SearchHit, 0, len(result.Posts))
	for _, p := range result.Posts {
		hits = append(hits, esclient.SearchHit{
			ID:       p.ID,
			Title:    p.Title,
			Summary:  p.Summary,
			Date:     p.Date,
			Category: p.Category,
			Tags:     p.Tags,
		})
	}
	c.JSON(http.StatusOK, esclient.SearchResult{Posts: hits, Total: result.Total, Aggregations: map[string][]esclient.AggBucket{}})
}

func (pc *PostController) handlePosts(c *gin.Context) {
	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	size, _ := strconv.ParseInt(c.DefaultQuery("size", "10"), 10, 64)
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 50 {
		size = 10
	}

	filter := model.PostFilter{
		Status:   string(model.StatusPublished),
		Tag:      c.Query("tag"),
		Category: c.Query("category"),
		Search:   c.Query("q"),
	}

	ctx := c.Request.Context()

	cacheKey := cacheKeyPosts(filter, page, size)
	if pc.cache != nil && filter.Search == "" {
		var cached model.PostListResult
		if err := pc.cache.Get(ctx, cacheKey, &cached); err == nil {
			c.JSON(http.StatusOK, &cached)
			return
		}
	}

	result, err := pc.store.FindPosts(ctx, filter, page, size)
	if err != nil {
		slog.Error("handlePosts", "error", err)
		respondError(c, ErrInternal)
		return
	}

	items := make([]gin.H, 0, len(result.Posts))
	for _, p := range result.Posts {
		item := gin.H{
			"id":       p.ID,
			"date":     p.Date,
			"title":    p.Title,
			"summary":  p.Summary,
			"tags":     p.Tags,
			"category": p.Category,
		}
		if p.Encrypted {
			item["encrypted"] = true
			item["encryption"] = gin.H{
				"salt":       p.Encryption.Salt,
				"nonce":      p.Encryption.Nonce,
				"ciphertext": p.Encryption.Ciphertext,
			}
		}
		items = append(items, item)
	}

	resp := gin.H{
		"posts": items,
		"total": result.Total,
		"page":  result.Page,
		"size":  result.Size,
	}

	if pc.cache != nil && filter.Search == "" {
		if err := pc.cache.Set(ctx, cacheKey, resp, 5*time.Minute); err != nil {
			slog.Warn("cache set", "error", err)
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (pc *PostController) handlePostDetail(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()

	cacheKey := "post:" + id
	if pc.cache != nil {
		var cached gin.H
		if err := pc.cache.Get(ctx, cacheKey, &cached); err == nil {
			c.JSON(http.StatusOK, cached)
			return
		}
	}

	p, err := pc.store.FindPost(ctx, id)
	if err != nil {
		respondError(c, ErrNotFound)
		return
	}
	if p.Status != string(model.StatusPublished) {
		respondError(c, ErrNotFound)
		return
	}

	resp := gin.H{
		"id":           p.ID,
		"date":         p.Date,
		"title":        p.Title,
		"content_html": p.ContentHTML,
		"tags":         p.Tags,
		"category":     p.Category,
	}
	if p.Encrypted {
		resp["encrypted"] = true
		resp["encryption"] = gin.H{
			"salt":       p.Encryption.Salt,
			"nonce":      p.Encryption.Nonce,
			"ciphertext": p.Encryption.Ciphertext,
		}
	} else {
		resp["content"] = p.Content
	}

	if pc.cache != nil {
		if err := pc.cache.Set(ctx, cacheKey, resp, 10*time.Minute); err != nil {
			slog.Warn("cache set", "error", err)
		}
	}

	if pc.cache != nil {
		_ = pc.cache.IncrViews(ctx, id)
	}

	c.JSON(http.StatusOK, resp)
}

func (pc *PostController) handleVerify(c *gin.Context) {
	id := c.Param("id")
	p, err := pc.store.FindPost(c.Request.Context(), id)
	if err != nil {
		respondError(c, ErrNotFound)
		return
	}
	if !p.Encrypted {
		respondError(c, &AppError{Code: "NOT_ENCRYPTED", Message: "post is not encrypted", Status: http.StatusBadRequest})
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
		respondError(c, &AppError{Code: "PASSWORD_REQUIRED", Message: "password required", Status: http.StatusBadRequest})
		return
	}

	content, err := crypto.DecryptContent(p.Encryption, req.Password)
	if err != nil {
		respondError(c, ErrForbidden)
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}

func (pc *PostController) handleTags(c *gin.Context) {
	ctx := c.Request.Context()
	if pc.cache != nil {
		var tags []string
		if err := pc.cache.Get(ctx, "tags:all", &tags); err == nil {
			c.JSON(http.StatusOK, gin.H{"tags": tags})
			return
		}
	}

	tags, err := pc.store.AllTags(ctx)
	if err != nil {
		slog.Error("handleTags", "error", err)
		respondError(c, ErrInternal)
		return
	}

	if pc.cache != nil {
		if err := pc.cache.Set(ctx, "tags:all", tags, 30*time.Minute); err != nil {
			slog.Warn("cache set", "error", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

func cacheKeyPosts(f model.PostFilter, page, size int64) string {
	var buf bytes.Buffer
	buf.WriteString("posts:list:")
	enc := json.NewEncoder(&buf)
	enc.Encode(map[string]any{
		"page": page, "size": size,
		"tag": f.Tag, "category": f.Category,
	})
	return buf.String()
}
