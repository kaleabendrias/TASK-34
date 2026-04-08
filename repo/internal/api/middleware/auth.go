package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
)

// SessionCookieName is the name of the cookie holding the opaque session id.
const SessionCookieName = "harborworks_session"

// Context keys for the resolved user/session.
const (
	ctxKeyUser    = "auth.user"
	ctxKeySession = "auth.session"
)

// Authenticator builds Gin middleware that resolves the session cookie into a
// *domain.User attached to the gin context. If `required` is true, missing or
// expired sessions short-circuit with 401. Used by both API and HTML routes.
func Authenticator(auth *service.AuthService, required bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(SessionCookieName)
		if err != nil || cookie == "" {
			if required {
				wantsHTML(c)
				return
			}
			c.Next()
			return
		}
		user, sess, err := auth.ResolveSession(c.Request.Context(), cookie)
		if err != nil {
			// Clear the bad cookie regardless.
			clearCookie(c)
			if required {
				switch {
				case errors.Is(err, domain.ErrSessionExpired):
					unauthorized(c, "session expired due to inactivity")
				case errors.Is(err, domain.ErrUnauthorized):
					unauthorized(c, "authentication required")
				default:
					unauthorized(c, "authentication error")
				}
				return
			}
			c.Next()
			return
		}
		c.Set(ctxKeyUser, user)
		c.Set(ctxKeySession, sess)
		c.Next()
	}
}

// CurrentUser fetches the resolved user from the gin context, or nil.
func CurrentUser(c *gin.Context) *domain.User {
	v, ok := c.Get(ctxKeyUser)
	if !ok {
		return nil
	}
	u, _ := v.(*domain.User)
	return u
}

// RequireNotBlacklisted is a gate handler that 403s if the current user is on
// the blacklist. Routes that should be hard-blocked for blacklisted users
// (notably booking creation) compose this after Authenticator(required=true).
func RequireNotBlacklisted() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := CurrentUser(c)
		if u == nil {
			unauthorized(c, "authentication required")
			return
		}
		if u.IsBlacklisted {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":  "user is blacklisted",
				"reason": u.BlacklistReason,
			})
			return
		}
		c.Next()
	}
}

// RequirePasswordRotated blocks every endpoint except logout, /me, and the
// change-password endpoint when the current user's must_rotate_password
// flag is set. The seeded admin starts with this flag on so the operator
// is forced to set a real password before doing anything else.
func RequirePasswordRotated() gin.HandlerFunc {
	const changePath = "/api/auth/change-password"
	const logoutPath = "/api/auth/logout"
	const mePath = "/api/auth/me"
	return func(c *gin.Context) {
		u := CurrentUser(c)
		if u == nil || !u.MustRotatePassword {
			c.Next()
			return
		}
		switch c.Request.URL.Path {
		case changePath, logoutPath, mePath:
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "password rotation required",
			"hint":  "POST " + changePath + " with current_password + new_password",
		})
	}
}

func unauthorized(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": msg})
}

func wantsHTML(c *gin.Context) {
	// HTML pages get a redirect to /auth/login; API callers get JSON 401.
	if isHTMLRoute(c) {
		c.Redirect(http.StatusFound, "/auth/login")
		c.Abort()
		return
	}
	unauthorized(c, "authentication required")
}

func isHTMLRoute(c *gin.Context) bool {
	accept := c.GetHeader("Accept")
	return c.Request.Method == http.MethodGet && (accept == "" || strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*"))
}

func clearCookie(c *gin.Context) {
	c.SetCookie(SessionCookieName, "", -1, "/", "", false, true)
}
