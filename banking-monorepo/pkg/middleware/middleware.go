package middleware

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"errors"
)

var (
	ErrNoTokenInRequest = errors.New("no token provided in request")
)

type contextKey string

const (
	claimsKey    contextKey = "jwt_claims"
	requestIDKey contextKey = "request_id"
)

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestID := uuid.New().String()
		responseWriter.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(request.Context(), requestIDKey, requestID)
		next.ServeHTTP(responseWriter, request.WithContext(ctx))
	})
}

func GetRequestID(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(requestIDKey).(string)
	return requestID, ok
}

func JWTMiddleware(secretEnv string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			secret := os.Getenv(secretEnv)
			if secret == "" {
				http.Error(responseWriter, "internal server error: missing JWT secret", http.StatusInternalServerError)
				return
			}

			token, err := extractBearerToken(request)
			if err != nil {
				http.Error(responseWriter, "unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}

			claims, err := parseToken(token, secret)
			if err != nil {
				http.Error(responseWriter, "unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(request.Context(), claimsKey, claims)
			next.ServeHTTP(responseWriter, request.WithContext(ctx))
		})
	}
}

func extractBearerToken(request *http.Request) (string, error) {
	authHeader := request.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		return "", ErrNoTokenInRequest
	}
	return strings.TrimPrefix(authHeader, "Bearer "), nil
}

func parseToken(token, secret string) (jwt.MapClaims, error) {
	parsedToken, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	if !parsedToken.Valid {
		return nil, jwt.ErrSignatureInvalid
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok {
		return nil, jwt.ErrTokenInvalidClaims
	}

	return claims, nil
}

func GetClaims(ctx context.Context) (jwt.MapClaims, bool) {
	claims, ok := ctx.Value(claimsKey).(jwt.MapClaims)
	return claims, ok
}
