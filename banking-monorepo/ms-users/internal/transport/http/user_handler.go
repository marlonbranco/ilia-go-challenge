package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"ms-users/internal/domain"
	"ms-users/internal/usecase"

	"pkg/middleware"
	apiresponse "pkg/response"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

type UserHandler struct {
	useCase  usecase.UserService
	validate *validator.Validate
}

func NewUserHandler(useCase usecase.UserService) *UserHandler {
	return &UserHandler{
		useCase:  useCase,
		validate: validator.New(),
	}
}

type userUpdatePayload struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"    validate:"omitempty,email"`
	Password  string `json:"password" validate:"omitempty,min=6"`
}

type usersResponseItem struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

func toUserResponse(user domain.User) usersResponseItem {
	return usersResponseItem{
		ID:        user.ID.String(),
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Email:     user.Email,
	}
}

func (handler *UserHandler) RegisterRoutes(mux *http.ServeMux, jwtMiddleware func(http.Handler) http.Handler) {
	mux.Handle("GET /users", jwtMiddleware(http.HandlerFunc(handler.handleList)))
	mux.Handle("GET /users/{id}", jwtMiddleware(http.HandlerFunc(handler.handleGet)))
	mux.Handle("PATCH /users/{id}", jwtMiddleware(http.HandlerFunc(handler.handlePatch)))
	mux.Handle("DELETE /users/{id}", jwtMiddleware(http.HandlerFunc(handler.handleDelete)))
}

func (handler *UserHandler) handleList(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	users, err := handler.useCase.FindAll(request.Context())
	if err != nil {
		slog.Error("failed to find all users", "error", err)
		writeJSON(response, http.StatusInternalServerError, apiresponse.Error("INTERNAL_ERROR", "failed to list users", requestID))
		return
	}

	userResponses := make([]usersResponseItem, 0, len(users))
	for _, user := range users {
		userResponses = append(userResponses, toUserResponse(user))
	}

	writeJSON(response, http.StatusOK, userResponses)
}

func (handler *UserHandler) handleGet(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	idParam := request.PathValue("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, apiresponse.Error("BAD_REQUEST", "invalid user id", requestID))
		return
	}

	user, err := handler.useCase.FindByID(request.Context(), id)
	if err != nil {
		writeJSON(response, http.StatusNotFound, apiresponse.Error("NOT_FOUND", "user not found", requestID))
		return
	}

	writeJSON(response, http.StatusOK, toUserResponse(user))
}

func (handler *UserHandler) handlePatch(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	idParam := request.PathValue("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, apiresponse.Error("BAD_REQUEST", "invalid user id", requestID))
		return
	}

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

	var payload userUpdatePayload
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(response, http.StatusBadRequest, apiresponse.Error("INVALID_BODY", "invalid JSON body", requestID))
		return
	}

	if err := handler.validate.Struct(payload); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, apiresponse.Error("VALIDATION_ERROR", err.Error(), requestID))
		return
	}

	updateRequest := usecase.UserUpdateRequest{
		FirstName: payload.FirstName,
		LastName:  payload.LastName,
		Email:     payload.Email,
		Password:  payload.Password,
	}

	user, err := handler.useCase.Update(request.Context(), id, userID, updateRequest)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, apiresponse.Error("INTERNAL_ERROR", err.Error(), requestID))
		return
	}

	writeJSON(response, http.StatusOK, toUserResponse(user))
}

func (handler *UserHandler) handleDelete(response http.ResponseWriter, request *http.Request) {
	requestID, _ := middleware.GetRequestID(request.Context())

	idParam := request.PathValue("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, apiresponse.Error("BAD_REQUEST", "invalid user id", requestID))
		return
	}

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

	err = handler.useCase.Delete(request.Context(), id, userID)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, apiresponse.Error("INTERNAL_ERROR", err.Error(), requestID))
		return
	}

	response.WriteHeader(http.StatusOK)
}
