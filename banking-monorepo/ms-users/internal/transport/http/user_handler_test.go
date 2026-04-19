package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"strings"
	"time"
	"os"

	"github.com/google/uuid"
	"github.com/golang-jwt/jwt/v5"

	"ms-users/internal/domain"
	"ms-users/internal/usecase"
	transporthttp "ms-users/internal/transport/http"
	"pkg/middleware"
)

type fakeUserRepoForHandler struct {
	user domain.User
}

func (repository *fakeUserRepoForHandler) Create(ctx context.Context, user domain.User) (domain.User, error) { return user, nil }
func (repository *fakeUserRepoForHandler) FindAll(ctx context.Context) ([]domain.User, error) { return []domain.User{repository.user}, nil }
func (repository *fakeUserRepoForHandler) FindByEmail(ctx context.Context, email string) (domain.User, error) { return repository.user, nil }
func (repository *fakeUserRepoForHandler) FindByID(ctx context.Context, id uuid.UUID) (domain.User, error) { return repository.user, nil }
func (repository *fakeUserRepoForHandler) Update(ctx context.Context, user domain.User) error { return nil }
func (repository *fakeUserRepoForHandler) Delete(ctx context.Context, id uuid.UUID) error { return nil }

func TestUserHandlerRoutes(test *testing.T) {
	os.Setenv("JWT_SECRET", "test_secret")
	repository := &fakeUserRepoForHandler{
		user: domain.User{
			ID: uuid.New(),
			FirstName: "Test",
			LastName: "Test",
			Email: "test@example.com",
		},
	}
	
	useCase := usecase.NewUserUseCase(repository)
	handler := transporthttp.NewUserHandler(useCase)

	mux := http.NewServeMux()
	jwtMiddleware := middleware.JWTMiddleware("JWT_SECRET")

	handler.RegisterRoutes(mux, jwtMiddleware)

	generateToken := func() string {
		claims := jwt.MapClaims{
			"sub":   repository.user.ID.String(),
			"email": repository.user.Email,
			"iat":   time.Now().Unix(),
			"exp":   time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, _ := token.SignedString([]byte("test_secret"))
		return signed
	}

	test.Run("GET /users", func(test *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/users", nil)
		request.Header.Set("Authorization", "Bearer "+generateToken())
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			test.Errorf("expected 200, got %v", recorder.Code)
		}
	})

	test.Run("GET /users/:id", func(test *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/users/"+repository.user.ID.String(), nil)
		request.Header.Set("Authorization", "Bearer "+generateToken())
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			test.Errorf("expected 200, got %v", recorder.Code)
		}
	})

	test.Run("PATCH /users/:id", func(test *testing.T) {
		body := strings.NewReader(`{"first_name": "Updates", "last_name": "New"}`)
		request := httptest.NewRequest(http.MethodPatch, "/users/"+repository.user.ID.String(), body)
		request.Header.Set("Authorization", "Bearer "+generateToken())
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			test.Errorf("expected 200, got %v: %s", recorder.Code, recorder.Body.String())
		}
	})

	test.Run("DELETE /users/:id", func(test *testing.T) {
		request := httptest.NewRequest(http.MethodDelete, "/users/"+repository.user.ID.String(), nil)
		request.Header.Set("Authorization", "Bearer "+generateToken())
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			test.Errorf("expected 200, got %v", recorder.Code)
		}
	})
}
