package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"ms-users/internal/domain"
	"ms-users/internal/usecase"

	"pkg/middleware"
	apiresponse "pkg/response"

	"github.com/go-playground/validator/v10"
)

type AuthHandler struct {
	useCase  usecase.AuthService
	validate *validator.Validate
}

func NewAuthHandler(useCase usecase.AuthService) *AuthHandler {
	return &AuthHandler{
		useCase:  useCase,
		validate: validator.New(),
	}
}

type registerRequest struct {
	FirstName string `json:"first_name" validate:"required"`
	LastName  string `json:"last_name"  validate:"required"`
	Email     string `json:"email"      validate:"required,email"`
	Password  string `json:"password"   validate:"required,min=6"`
}

type loginRequest struct {
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type refreshRequest struct {
	AccessToken  string `json:"access_token"  validate:"required"`
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type tokenResponse struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	Balance      *string `json:"balance,omitempty"`
}

func (handler *AuthHandler) RegisterRoutes(mux *http.ServeMux, jwtMiddleware func(http.Handler) http.Handler) {
	mux.HandleFunc("POST /auth/register", handler.handleRegister)
	mux.HandleFunc("POST /auth/login", handler.handleLogin)
	mux.HandleFunc("POST /auth/refresh", handler.handleRefresh)

	mux.Handle("POST /auth/logout", jwtMiddleware(http.HandlerFunc(handler.handleLogout)))
}

func (handler *AuthHandler) handleRegister(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	var payload registerRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(response, http.StatusBadRequest, apiresponse.Error("INVALID_BODY", "invalid JSON body", requestID))
		return
	}

	if err := handler.validate.Struct(payload); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, apiresponse.Error("VALIDATION_ERROR", err.Error(), requestID))
		return
	}

	pair, err := handler.useCase.Register(request.Context(), usecase.RegisterRequest{
		FirstName: payload.FirstName,
		LastName:  payload.LastName,
		Email:     payload.Email,
		Password:  payload.Password,
	})
	if err != nil {
		handler.handleUseCaseError(response, err, requestID)
		return
	}

	writeJSON(response, http.StatusCreated, apiresponse.Success(tokenResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	}, requestID))
}

func (handler *AuthHandler) handleLogin(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	var payload loginRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(response, http.StatusBadRequest, apiresponse.Error("INVALID_BODY", "invalid JSON body", requestID))
		return
	}

	if err := handler.validate.Struct(payload); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, apiresponse.Error("VALIDATION_ERROR", err.Error(), requestID))
		return
	}

	pair, err := handler.useCase.Login(request.Context(), usecase.LoginRequest{
		Email:    payload.Email,
		Password: payload.Password,
	})
	if err != nil {
		handler.handleUseCaseError(response, err, requestID)
		return
	}

	writeJSON(response, http.StatusOK, apiresponse.Success(tokenResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		Balance:      pair.Balance,
	}, requestID))
}

func (handler *AuthHandler) handleRefresh(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	var payload refreshRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(response, http.StatusBadRequest, apiresponse.Error("INVALID_BODY", "invalid JSON body", requestID))
		return
	}

	if err := handler.validate.Struct(payload); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, apiresponse.Error("VALIDATION_ERROR", err.Error(), requestID))
		return
	}

	pair, err := handler.useCase.RefreshToken(request.Context(), payload.AccessToken, payload.RefreshToken)
	if err != nil {
		handler.handleUseCaseError(response, err, requestID)
		return
	}

	writeJSON(response, http.StatusOK, apiresponse.Success(tokenResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	}, requestID))
}

func (handler *AuthHandler) handleLogout(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	claims, ok := middleware.GetClaims(request.Context())
	if !ok {
		writeJSON(response, http.StatusUnauthorized, apiresponse.Error("UNAUTHORIZED", "missing JWT claims", requestID))
		return
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		writeJSON(response, http.StatusUnauthorized, apiresponse.Error("UNAUTHORIZED", "invalid JWT claims", requestID))
		return
	}

	var payload logoutRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(response, http.StatusBadRequest, apiresponse.Error("INVALID_BODY", "invalid JSON body", requestID))
		return
	}

	if err := handler.validate.Struct(payload); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, apiresponse.Error("VALIDATION_ERROR", err.Error(), requestID))
		return
	}

	if err := handler.useCase.Logout(request.Context(), userID, payload.RefreshToken); err != nil {
		slog.Error("logout failed", "error", err, "user_id", userID)
		writeJSON(response, http.StatusInternalServerError, apiresponse.Error("INTERNAL_ERROR", "failed to logout", requestID))
		return
	}

	writeJSON(response, http.StatusOK, apiresponse.Success(nil, requestID))
}

func (handler *AuthHandler) handleUseCaseError(response http.ResponseWriter, err error, requestID string) {
	switch {
	case errors.Is(err, domain.ErrDuplicate):
		writeJSON(response, http.StatusConflict, apiresponse.Error("DUPLICATE", "email already registered", requestID))
	case errors.Is(err, domain.ErrInvalidEmail):
		writeJSON(response, http.StatusUnprocessableEntity, apiresponse.Error("INVALID_EMAIL", "invalid email address", requestID))
	case errors.Is(err, domain.ErrInvalidPassword):
		writeJSON(response, http.StatusUnprocessableEntity, apiresponse.Error("INVALID_PASSWORD", "password cannot be empty", requestID))
	case errors.Is(err, domain.ErrInvalidCredentials):
		writeJSON(response, http.StatusUnauthorized, apiresponse.Error("INVALID_CREDENTIALS", "invalid email or password", requestID))
	case errors.Is(err, domain.ErrInvalidToken):
		writeJSON(response, http.StatusUnauthorized, apiresponse.Error("INVALID_TOKEN", "invalid or expired token", requestID))
	default:
		slog.Error("unhandled use case error", "error", err)
		writeJSON(response, http.StatusInternalServerError, apiresponse.Error("INTERNAL_ERROR", "an unexpected error occurred", requestID))
	}
}
