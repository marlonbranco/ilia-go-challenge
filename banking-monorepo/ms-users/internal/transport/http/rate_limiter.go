package http

import (
	"encoding/json"
	"net/http"
	"time"

	"ms-users/internal/domain"

	"pkg/middleware"
)

func RateLimiter(counter domain.RateLimitCounter, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			ip := request.RemoteAddr
			key := "rate_limit:" + request.URL.Path + ":" + ip

			count, err := counter.Increment(request.Context(), key)
			if err != nil {
				next.ServeHTTP(response, request)
				return
			}

			if count == 1 {
				counter.Expire(request.Context(), key, window)
			}

			if count > int64(limit) {
				response.Header().Set("Content-Type", "application/json")
				response.WriteHeader(http.StatusTooManyRequests)

				requestID, _ := middleware.GetRequestID(request.Context())
				payload := map[string]interface{}{
					"error": map[string]string{
						"code":       "RATE_LIMITED",
						"message":    "Too many requests",
						"request_id": requestID,
					},
				}
				json.NewEncoder(response).Encode(payload)
				return
			}

			next.ServeHTTP(response, request)
		})
	}
}
