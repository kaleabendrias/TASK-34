package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/harborworks/booking-hub/internal/api/middleware"
	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
	"github.com/harborworks/booking-hub/internal/views"
)

type AuthHandler struct {
	auth *service.AuthService
	log  *slog.Logger
}

func NewAuthHandler(auth *service.AuthService, log *slog.Logger) *AuthHandler {
	return &AuthHandler{auth: auth, log: log}
}

// ---------- requests ----------

type registerRequest struct {
	Username string `json:"username" form:"username" binding:"required"`
	Password string `json:"password" form:"password" binding:"required"`
}

type loginRequest struct {
	Username      string `json:"username"       form:"username"       binding:"required"`
	Password      string `json:"password"       form:"password"       binding:"required"`
	CaptchaToken  string `json:"captcha_token"  form:"captcha_token"`
	CaptchaAnswer string `json:"captcha_answer" form:"captcha_answer"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password" form:"current_password" binding:"required"`
	NewPassword     string `json:"new_password"     form:"new_password"     binding:"required"`
}

// ---------- JSON endpoints ----------

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	user, err := h.auth.Register(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		writeAuthError(c, h.auth, err)
		return
	}
	c.JSON(http.StatusCreated, user)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.auth.Login(c.Request.Context(), service.LoginInput{
		Username:      req.Username,
		Password:      req.Password,
		CaptchaToken:  req.CaptchaToken,
		CaptchaAnswer: req.CaptchaAnswer,
		UserAgent:     c.Request.UserAgent(),
		IP:            c.ClientIP(),
	})
	if err != nil {
		writeAuthError(c, h.auth, err)
		return
	}

	settings := h.auth.Settings()
	maxAge := int(settings.SessionInactivity.Seconds())
	// Secure flag is on by default; toggled off only via COOKIE_SECURE=false
	// for plain-HTTP local development. HttpOnly is always true so the
	// session id is never exposed to JavaScript.
	c.SetCookie(middleware.SessionCookieName, res.Session.ID, maxAge, "/", "", settings.CookieSecure, true)
	c.JSON(http.StatusOK, gin.H{
		"user":               res.User,
		"session_expires_at": res.Session.ExpiresAt,
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	if cookie, err := c.Cookie(middleware.SessionCookieName); err == nil {
		_ = h.auth.Logout(c.Request.Context(), cookie)
	}
	c.SetCookie(middleware.SessionCookieName, "", -1, "/", "", h.auth.Settings().CookieSecure, true)
	c.JSON(http.StatusOK, gin.H{"status": "logged_out"})
}

func (h *AuthHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, middleware.CurrentUser(c))
}

// POST /api/auth/change-password — required when the must_rotate_password
// flag is set on the current user. Validates the current password against
// bcrypt, applies the password policy to the new value, and clears the
// rotation flag on success.
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	user := middleware.CurrentUser(c)
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.auth.ChangePassword(c.Request.Context(), user.ID, req.CurrentPassword, req.NewPassword); err != nil {
		writeAuthError(c, h.auth, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "rotated"})
}

// Captcha mints a fresh challenge.
func (h *AuthHandler) Captcha(c *gin.Context) {
	ch, err := h.auth.IssueCaptcha(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "captcha unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token":      ch.Token,
		"question":   ch.Question,
		"expires_at": ch.ExpiresAt,
	})
}

// ---------- HTML pages ----------

func (h *AuthHandler) RegisterPage(c *gin.Context) {
	renderTempl(c, http.StatusOK, views.RegisterPage(viewError(c)))
}

func (h *AuthHandler) LoginPage(c *gin.Context) {
	renderTempl(c, http.StatusOK, views.LoginPage(viewError(c), c.Query("captcha_token"), c.Query("captcha_question")))
}

// ---------- shared ----------

func writeAuthError(c *gin.Context, auth *service.AuthService, err error) {
	switch {
	case errors.Is(err, domain.ErrPasswordPolicy),
		errors.Is(err, domain.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrCredentialInvalid):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
	case errors.Is(err, domain.ErrLocked):
		c.JSON(http.StatusLocked, gin.H{
			"error":          "account temporarily locked",
			"detail":         err.Error(),
			"unlock_window":  auth.Settings().LockoutDuration.String(),
		})
	case errors.Is(err, domain.ErrCaptchaRequired):
		// Mint a fresh captcha so the client can immediately retry.
		ch, mintErr := auth.IssueCaptcha(c.Request.Context())
		if mintErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "captcha unavailable"})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":            "captcha required",
			"captcha_token":    ch.Token,
			"captcha_question": ch.Question,
			"captcha_expires":  ch.ExpiresAt.Format(time.RFC3339),
		})
	case errors.Is(err, domain.ErrCaptchaInvalid):
		ch, mintErr := auth.IssueCaptcha(c.Request.Context())
		if mintErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "captcha unavailable"})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":            "captcha incorrect",
			"captcha_token":    ch.Token,
			"captcha_question": ch.Question,
		})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

func viewError(c *gin.Context) string { return c.Query("error") }
