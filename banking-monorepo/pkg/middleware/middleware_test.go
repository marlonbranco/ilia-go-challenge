package middleware_test

import (
	"net/http"
	httpTest "net/http/httptest"
	"os"
	"pkg/middleware"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestRequestIDMiddleware(test *testing.T) {
	handler := middleware.RequestIDMiddleware(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestID, ok := middleware.GetRequestID(request.Context())
		if !ok {
			test.Error("expected request ID in context")
		}
		if _, err := uuid.Parse(requestID); err != nil {
			test.Errorf("expected valid UUID, got %q", requestID)
		}
		responseWriter.WriteHeader(http.StatusOK)
	}))

	requestMock := httpTest.NewRequest("GET", "/", nil)
	responseWriterMock := httpTest.NewRecorder()

	handler.ServeHTTP(responseWriterMock, requestMock)

	responseMock := responseWriterMock.Result()
	requestIDHeader := responseMock.Header.Get("X-Request-ID")
	if requestIDHeader == "" {
		test.Error("expected X-Request-ID header")
	}
	if _, err := uuid.Parse(requestIDHeader); err != nil {
		test.Errorf("expected valid UUID in header, got %q", requestIDHeader)
	}
}

func TestJWTMiddleware(test *testing.T) {
	secretKey := "test_secret"
	os.Setenv("JWT_SECRET", secretKey)
	defer os.Unsetenv("JWT_SECRET")

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user123",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, err := token.SignedString([]byte(secretKey))
	if err != nil {
		test.Fatalf("failed to sign token: %v", err)
	}

	handler := middleware.JWTMiddleware("JWT_SECRET")(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		claims, ok := middleware.GetClaims(request.Context())
		if !ok {
			test.Error("expected claims in context")
		}
		if claims["sub"] != "user123" {
			test.Errorf("expected sub to be user123, got %v", claims["sub"])
		}
		responseWriter.WriteHeader(http.StatusOK)
	}))

	requestMock := httpTest.NewRequest("GET", "/", nil)
	requestMock.Header.Set("Authorization", "Bearer "+tokenString)
	responseWriterMock := httpTest.NewRecorder()

	handler.ServeHTTP(responseWriterMock, requestMock)

	if responseWriterMock.Code != http.StatusOK {
		test.Errorf("expected status OK, got %d", responseWriterMock.Code)
	}
}

func TestJWTMiddlewareNoToken(test *testing.T) {
	os.Setenv("JWT_SECRET", "test_secret")
	defer os.Unsetenv("JWT_SECRET")

	handler := middleware.JWTMiddleware("JWT_SECRET")(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		test.Error("handler should not be called")
	}))

	requestMock := httpTest.NewRequest("GET", "/", nil)
	responseWriterMock := httpTest.NewRecorder()

	handler.ServeHTTP(responseWriterMock, requestMock)

	if responseWriterMock.Code != http.StatusUnauthorized {
		test.Errorf("expected status Unauthorized, got %d", responseWriterMock.Code)
	}
}
