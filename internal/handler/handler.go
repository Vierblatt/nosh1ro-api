package handler

import (
	"context"
	"log/slog"

	"github.com/Vierblatt/nosh1ro-api/internal/model"
	"github.com/gin-gonic/gin"
)

// Store interfaces — defined where consumed, implemented by store.Store.
type PostStore interface {
	FindPosts(ctx context.Context, f model.PostFilter, page, size int64) (*model.PostListResult, error)
	FindPost(ctx context.Context, id string) (*model.Post, error)
	InsertPost(ctx context.Context, p *model.Post) error
	ReplacePost(ctx context.Context, p *model.Post) error
	DeletePost(ctx context.Context, id string) error
	PostExists(ctx context.Context, id string) bool
	AllTags(ctx context.Context) ([]string, error)
	Ping(ctx context.Context) error
}

type AdminStore interface {
	FindAdmin(ctx context.Context, username string) (*model.AdminUser, error)
	UpsertAdmin(ctx context.Context, username, passwordHash string) error
	GetSettings(ctx context.Context) (*model.BlogSettings, error)
	UpsertSettings(ctx context.Context, bs *model.BlogSettings) error
}

// AppError is the unified error response type.
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *AppError) Error() string { return e.Message }

// Predefined application errors.
var (
	ErrNotFound      = &AppError{Code: "NOT_FOUND", Message: "resource not found", Status: 404}
	ErrUnauthorized  = &AppError{Code: "UNAUTHORIZED", Message: "authentication required", Status: 401}
	ErrForbidden     = &AppError{Code: "FORBIDDEN", Message: "access denied", Status: 403}
	ErrBadRequest    = &AppError{Code: "BAD_REQUEST", Message: "invalid request", Status: 400}
	ErrInternal      = &AppError{Code: "INTERNAL", Message: "internal server error", Status: 500}
	ErrTooManyReqs   = &AppError{Code: "RATE_LIMITED", Message: "rate limit exceeded", Status: 429}
)

func respondError(c *gin.Context, err *AppError) {
	c.JSON(err.Status, err)
}

func recoverMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic recovered", "error", r)
				c.AbortWithStatusJSON(500, ErrInternal)
			}
		}()
		c.Next()
	}
}
