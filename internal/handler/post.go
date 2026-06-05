package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Vierblatt/nosh1ro-api/internal/crypto"
	"github.com/Vierblatt/nosh1ro-api/internal/model"
	"github.com/gin-gonic/gin"
)

type PostController struct {
	store PostStore
}

func NewPostController(store PostStore) *PostController {
	return &PostController{store: store}
}

func (pc *PostController) Register(api *gin.RouterGroup) {
	api.GET("/health", pc.handleHealth)
	api.GET("/posts", pc.handlePosts)
	api.GET("/posts/:id", pc.handlePostDetail)
	api.POST("/posts/:id/verify", pc.handleVerify)
	api.GET("/tags", pc.handleTags)
}

func (pc *PostController) handleHealth(c *gin.Context) {
	ctx := c.Request.Context()
	if err := pc.store.Ping(ctx); err != nil {
		slog.Warn("health check: db ping failed", "error", err)
		c.JSON(http.StatusOK, gin.H{"status": "degraded", "db": "disconnected"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "db": "connected"})
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

	result, err := pc.store.FindPosts(c.Request.Context(), filter, page, size)
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

	c.JSON(http.StatusOK, gin.H{
		"posts": items,
		"total": result.Total,
		"page":  result.Page,
		"size":  result.Size,
	})
}

func (pc *PostController) handlePostDetail(c *gin.Context) {
	id := c.Param("id")
	p, err := pc.store.FindPost(c.Request.Context(), id)
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
	tags, err := pc.store.AllTags(c.Request.Context())
	if err != nil {
		slog.Error("handleTags", "error", err)
		respondError(c, ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": tags})
}
