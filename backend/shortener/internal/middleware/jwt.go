package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const UserIDKey contextKey = "user_id"

// DenylistChecker is the token-revocation behavior JWTAuth depends on.
// *cache.RedisCache satisfies this interface.
type DenylistChecker interface {
	IsJWTDenied(ctx context.Context, jti string) bool
}

func JWTAuth(secret []byte, denylist DenylistChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
				return
			}

			token, err := jwt.Parse(parts[1], func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return secret, nil
			})
			if err != nil || !token.Valid {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				http.Error(w, `{"error":"invalid token claims"}`, http.StatusUnauthorized)
				return
			}

			// Check JWT denylist — fail closed for security. Skip when no denylist
			// is configured (nil), e.g. in unit tests that don't exercise revocation.
			if jti, ok := claims["jti"].(string); ok && jti != "" && denylist != nil {
				if denylist.IsJWTDenied(r.Context(), jti) {
					http.Error(w, `{"error":"token has been revoked"}`, http.StatusUnauthorized)
					return
				}
			}

			userID, ok := claims["sub"].(string)
			if !ok || userID == "" {
				http.Error(w, `{"error":"missing user ID in token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUserID(ctx context.Context) string {
	id, _ := ctx.Value(UserIDKey).(string)
	return id
}
