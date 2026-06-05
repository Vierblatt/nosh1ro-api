package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/Vierblatt/nosh1ro-api/internal/auth"
	"github.com/Vierblatt/nosh1ro-api/internal/email"
	"github.com/Vierblatt/nosh1ro-api/internal/model"
	"github.com/gin-gonic/gin"
)

type AuthController struct {
	store   AdminStore
	secret  string
	email   *email.Config
}

func NewAuthController(store AdminStore, jwtSecret string, emailCfg *email.Config) *AuthController {
	return &AuthController{store: store, secret: jwtSecret, email: emailCfg}
}

func (ac *AuthController) Register(public *gin.RouterGroup) {
	public.POST("/auth/register", ac.handleRegister)
	public.GET("/auth/verify", ac.handleVerify)
	public.POST("/auth/resend-verification", ac.handleResend)
}

func (ac *AuthController) handleRegister(c *gin.Context) {
	var req struct {
		Username        string `json:"username"`
		Email           string `json:"email"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, ErrBadRequest)
		return
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		respondError(c, ErrBadRequest)
		return
	}
	if req.Password != req.ConfirmPassword {
		respondError(c, &AppError{Code: "BAD_REQUEST", Message: "两次输入的密码不一致", Status: 400})
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		respondError(c, &AppError{Code: "WEAK_PASSWORD", Message: err.Error(), Status: 400})
		return
	}

	count, err := ac.store.CountAdmins(c.Request.Context())
	if err != nil {
		slog.Error("register count", "error", err)
		respondError(c, ErrInternal)
		return
	}
	if count >= 10 {
		respondError(c, ErrUserLimit)
		return
	}

	if _, err := ac.store.FindAdmin(c.Request.Context(), req.Username); err == nil {
		respondError(c, ErrUserExists)
		return
	}
	if _, err := ac.store.FindAdminByEmail(c.Request.Context(), req.Email); err == nil {
		respondError(c, ErrEmailExists)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("register hash", "error", err)
		respondError(c, ErrInternal)
		return
	}

	token, err := auth.GenerateVerificationToken(ac.secret, req.Username)
	if err != nil {
		slog.Error("register token", "error", err)
		respondError(c, ErrInternal)
		return
	}

	user := &model.AdminUser{
		Username:     req.Username,
		PasswordHash: hash,
		Email:        req.Email,
		Verified:     false,
		VerifyToken:  token,
		CreatedAt:    time.Now(),
	}
	if err := ac.store.CreateAdmin(c.Request.Context(), user); err != nil {
		slog.Error("register create", "error", err)
		respondError(c, ErrInternal)
		return
	}

	ac.email.SendVerification(req.Email, req.Username, token)

	c.JSON(http.StatusCreated, gin.H{"message": "注册成功，请查收验证邮件"})
}

func (ac *AuthController) handleVerify(c *gin.Context) {
	tokenStr := c.Query("token")
	if tokenStr == "" {
		respondError(c, ErrBadRequest)
		return
	}

	claims, err := auth.ValidateToken(ac.secret, tokenStr)
	if err != nil || claims.Purpose != "verify" {
		respondError(c, ErrInvalidToken)
		return
	}

	u, err := ac.store.FindAdmin(c.Request.Context(), claims.Username)
	if err != nil {
		respondError(c, ErrInvalidToken)
		return
	}
	if u.Verified {
		c.JSON(http.StatusOK, gin.H{"message": "邮箱已验证，无需重复验证"})
		return
	}

	if err := ac.store.MarkVerified(c.Request.Context(), claims.Username); err != nil {
		slog.Error("verify mark", "error", err)
		respondError(c, ErrInternal)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "邮箱验证成功，现在可以登录了"})
}

func (ac *AuthController) handleResend(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Email == "" {
		respondError(c, ErrBadRequest)
		return
	}

	u, err := ac.store.FindAdminByEmail(c.Request.Context(), req.Email)
	if err != nil {
		// Don't reveal whether the email exists
		c.JSON(http.StatusOK, gin.H{"message": "如果该邮箱已注册，验证邮件已重新发送"})
		return
	}
	if u.Verified {
		c.JSON(http.StatusOK, gin.H{"message": "邮箱已验证，无需重复验证"})
		return
	}

	token, err := auth.GenerateVerificationToken(ac.secret, u.Username)
	if err != nil {
		slog.Error("resend token", "error", err)
		respondError(c, ErrInternal)
		return
	}

	if err := ac.store.SetVerifyToken(c.Request.Context(), u.Username, token); err != nil {
		slog.Error("resend save token", "error", err)
		respondError(c, ErrInternal)
		return
	}

	ac.email.SendVerification(u.Email, u.Username, token)
	c.JSON(http.StatusOK, gin.H{"message": "如果该邮箱已注册，验证邮件已重新发送"})
}
