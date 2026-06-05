package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/Vierblatt/nosh1ro-api/internal/auth"
	"github.com/Vierblatt/nosh1ro-api/internal/markdown"
	"github.com/Vierblatt/nosh1ro-api/internal/middleware"
	"github.com/Vierblatt/nosh1ro-api/internal/model"
	"github.com/gin-gonic/gin"
)

type AdminController struct {
	store  PostStore
	admin  AdminStore
	secret string
}

func NewAdminController(store PostStore, admin AdminStore, jwtSecret string) *AdminController {
	return &AdminController{store: store, admin: admin, secret: jwtSecret}
}

func (ac *AdminController) Register(admin *gin.RouterGroup) {
	admin.POST("/login", ac.handleLogin)

	protected := admin.Group("")
	protected.Use(middleware.Auth(ac.secret))
	{
		protected.GET("/posts", ac.handleListPosts)
		protected.POST("/posts", ac.handleCreatePost)
		protected.PUT("/posts/:id", ac.handleUpdatePost)
		protected.DELETE("/posts/:id", ac.handleDeletePost)
		protected.GET("/settings", ac.handleGetSettings)
		protected.PUT("/settings", ac.handleUpdateSettings)
	}
}

func (ac *AdminController) handleLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, ErrBadRequest)
		return
	}

	u, err := ac.admin.FindAdmin(c.Request.Context(), req.Username)
	if err != nil {
		respondError(c, ErrUnauthorized)
		return
	}
	if err := auth.CheckPassword(u.PasswordHash, req.Password); err != nil {
		respondError(c, ErrUnauthorized)
		return
	}
	if !u.Verified {
		respondError(c, ErrNotVerified)
		return
	}

	token, err := auth.GenerateToken(ac.secret, req.Username)
	if err != nil {
		slog.Error("generate token", "error", err)
		respondError(c, ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

func (ac *AdminController) handleListPosts(c *gin.Context) {
	filter := model.PostFilter{}
	result, err := ac.store.FindPosts(c.Request.Context(), filter, 1, 1000)
	if err != nil {
		slog.Error("admin list posts", "error", err)
		respondError(c, ErrInternal)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (ac *AdminController) handleCreatePost(c *gin.Context) {
	var req struct {
		ID       string   `json:"id"`
		Title    string   `json:"title"`
		Content  string   `json:"content"`
		Category string   `json:"category"`
		Status   string   `json:"status"`
		Tags     []string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, ErrBadRequest)
		return
	}
	if req.ID == "" || req.Title == "" {
		respondError(c, ErrBadRequest)
		return
	}

	now := time.Now()
	p := &model.Post{
		ID:          req.ID,
		Title:       req.Title,
		Content:     req.Content,
		ContentHTML: markdown.Render(req.Content),
		Summary:     markdown.ExtractSummary(markdown.Render(req.Content), 200),
		Date:        now.Format("2006-01-02"),
		Category:    req.Category,
		Status:      req.Status,
		Tags:        req.Tags,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if p.Status == "" {
		p.Status = string(model.StatusDraft)
	}

	if err := ac.store.InsertPost(c.Request.Context(), p); err != nil {
		slog.Error("admin create post", "error", err)
		respondError(c, ErrInternal)
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (ac *AdminController) handleUpdatePost(c *gin.Context) {
	id := c.Param("id")
	existing, err := ac.store.FindPost(c.Request.Context(), id)
	if err != nil {
		respondError(c, ErrNotFound)
		return
	}

	var req struct {
		Title    *string   `json:"title"`
		Content  *string   `json:"content"`
		Category *string   `json:"category"`
		Status   *string   `json:"status"`
		Tags     *[]string `json:"tags"`
		Date     *string   `json:"date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, ErrBadRequest)
		return
	}

	if req.Title != nil {
		existing.Title = *req.Title
	}
	if req.Content != nil {
		existing.Content = *req.Content
		existing.ContentHTML = markdown.Render(*req.Content)
		existing.Summary = markdown.ExtractSummary(existing.ContentHTML, 200)
	}
	if req.Category != nil {
		existing.Category = *req.Category
	}
	if req.Status != nil {
		existing.Status = *req.Status
	}
	if req.Tags != nil {
		existing.Tags = *req.Tags
	}
	if req.Date != nil {
		existing.Date = *req.Date
	}
	existing.UpdatedAt = time.Now()

	if err := ac.store.ReplacePost(c.Request.Context(), existing); err != nil {
		slog.Error("admin update post", "error", err)
		respondError(c, ErrInternal)
		return
	}
	c.JSON(http.StatusOK, existing)
}

func (ac *AdminController) handleDeletePost(c *gin.Context) {
	id := c.Param("id")
	if err := ac.store.DeletePost(c.Request.Context(), id); err != nil {
		respondError(c, ErrNotFound)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

func (ac *AdminController) handleGetSettings(c *gin.Context) {
	bs, err := ac.admin.GetSettings(c.Request.Context())
	if err != nil {
		respondError(c, ErrInternal)
		return
	}
	c.JSON(http.StatusOK, bs)
}

func (ac *AdminController) handleUpdateSettings(c *gin.Context) {
	var req struct {
		Title    *string `json:"title"`
		Subtitle *string `json:"subtitle"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, ErrBadRequest)
		return
	}

	bs, err := ac.admin.GetSettings(c.Request.Context())
	if err != nil {
		respondError(c, ErrInternal)
		return
	}
	if req.Title != nil {
		bs.Title = *req.Title
	}
	if req.Subtitle != nil {
		bs.Subtitle = *req.Subtitle
	}
	if err := ac.admin.UpsertSettings(c.Request.Context(), bs); err != nil {
		slog.Error("admin update settings", "error", err)
		respondError(c, ErrInternal)
		return
	}
	c.JSON(http.StatusOK, bs)
}
