package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ms-users/internal/domain"
	transporthttp "ms-users/internal/transport/http"
	"ms-users/internal/usecase"

	"github.com/google/uuid"
)

type fakeUserRepo struct {
	mutex sync.RWMutex
	users map[string]domain.User
	byID  map[uuid.UUID]domain.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		users: make(map[string]domain.User),
		byID:  make(map[uuid.UUID]domain.User),
	}
}

func (repository *fakeUserRepo) Create(_ context.Context, user domain.User) (domain.User, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if _, exists := repository.users[user.Email]; exists {
		return domain.User{}, fmt.Errorf("dup: %w", domain.ErrDuplicate)
	}
	repository.users[user.Email] = user
	repository.byID[user.ID] = user
	return user, nil
}

func (repository *fakeUserRepo) FindByEmail(_ context.Context, email string) (domain.User, error) {
	repository.mutex.RLock()
	defer repository.mutex.RUnlock()
	user, ok := repository.users[email]
	if !ok || user.IsDeleted {
		return domain.User{}, fmt.Errorf("not found: %w", domain.ErrNotFound)
	}
	return user, nil
}

func (repository *fakeUserRepo) FindByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	repository.mutex.RLock()
	defer repository.mutex.RUnlock()
	user, ok := repository.byID[id]
	if !ok || user.IsDeleted {
		return domain.User{}, fmt.Errorf("not found: %w", domain.ErrNotFound)
	}
	return user, nil
}

func (repository *fakeUserRepo) Update(_ context.Context, user domain.User) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if _, ok := repository.byID[user.ID]; !ok {
		return fmt.Errorf("not found: %w", domain.ErrNotFound)
	}
	repository.users[user.Email] = user
	repository.byID[user.ID] = user
	return nil
}

func (repository *fakeUserRepo) FindAll(_ context.Context) ([]domain.User, error) {
	repository.mutex.RLock()
	defer repository.mutex.RUnlock()
	
	var all []domain.User
	for _, u := range repository.byID {
		if !u.IsDeleted {
			all = append(all, u)
		}
	}
	return all, nil
}

func (repository *fakeUserRepo) Delete(_ context.Context, id uuid.UUID) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	
	user, ok := repository.byID[id]
	if !ok || user.IsDeleted {
		return fmt.Errorf("not found: %w", domain.ErrNotFound)
	}
	
	user.IsDeleted = true
	repository.byID[id] = user
	repository.users[user.Email] = user
	return nil
}

type fakeTokenStore struct {
	mutex  sync.RWMutex
	tokens map[string]time.Time
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{tokens: make(map[string]time.Time)}
}

func (store *fakeTokenStore) key(userID, tokenID string) string {
	return "refresh:" + userID + ":" + tokenID
}

func (store *fakeTokenStore) Store(_ context.Context, userID, tokenID string, ttl time.Duration) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.tokens[store.key(userID, tokenID)] = time.Now().Add(ttl)
	return nil
}

func (store *fakeTokenStore) Exists(_ context.Context, userID, tokenID string) (bool, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	exp, ok := store.tokens[store.key(userID, tokenID)]
	if !ok {
		return false, nil
	}
	return time.Now().Before(exp), nil
}

func (store *fakeTokenStore) Delete(_ context.Context, userID, tokenID string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	delete(store.tokens, store.key(userID, tokenID))
	return nil
}

const testSecret = "ILIACHALLENGE"

func setupHandler() (*transporthttp.AuthHandler, *usecase.AuthUseCase) {
	repository := newFakeUserRepo()
	store := newFakeTokenStore()
	useCase := usecase.NewAuthUseCase(repository, store, testSecret)
	handler := transporthttp.NewAuthHandler(useCase)
	return handler, useCase
}

func noopJWTMiddleware(next http.Handler) http.Handler { return next }

func jsonBody(v any) *bytes.Buffer {
	data, _ := json.Marshal(v)
	return bytes.NewBuffer(data)
}

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeEnvelope(test *testing.T, recorder *httptest.ResponseRecorder) envelope {
	test.Helper()
	var responseEnvelope envelope
	if err := json.NewDecoder(recorder.Body).Decode(&responseEnvelope); err != nil {
		test.Fatalf("failed to decode response: %v", err)
	}
	return responseEnvelope
}

func TestHandleRegister(test *testing.T) {
	tests := []struct {
		name       string
		body       any
		wantStatus int
		wantErr    string
	}{
		{
			name:       "success",
			body:       map[string]string{"email": "handler@example.com", "password": "strongpass", "first_name": "J", "last_name": "D"},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "invalid JSON",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
			wantErr:    "INVALID_BODY",
		},
		{
			name:       "missing email",
			body:       map[string]string{"password": "strongpass", "first_name": "J", "last_name": "D"},
			wantStatus: http.StatusUnprocessableEntity,
			wantErr:    "VALIDATION_ERROR",
		},
		{
			name:       "short password",
			body:       map[string]string{"email": "test@example.com", "password": "ab", "first_name": "J", "last_name": "D"},
			wantStatus: http.StatusUnprocessableEntity,
			wantErr:    "VALIDATION_ERROR",
		},
	}

	for _, testCase := range tests {
		test.Run(testCase.name, func(test *testing.T) {
			handler, _ := setupHandler()
			mux := http.NewServeMux()
			handler.RegisterRoutes(mux, noopJWTMiddleware)

			var body *bytes.Buffer
			if stringBody, ok := testCase.body.(string); ok {
				body = bytes.NewBufferString(stringBody)
			} else {
				body = jsonBody(testCase.body)
			}

			request := httptest.NewRequest("POST", "/auth/register", body)
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != testCase.wantStatus {
				test.Errorf("expected status %d, got %d", testCase.wantStatus, recorder.Code)
			}

			responseEnvelope := decodeEnvelope(test, recorder)

			if testCase.wantErr != "" {
				if responseEnvelope.Error == nil || responseEnvelope.Error.Code != testCase.wantErr {
					test.Errorf("expected error code %q, got %+v", testCase.wantErr, responseEnvelope.Error)
				}
			} else {
				if !responseEnvelope.Success {
					test.Error("expected success=true")
				}
			}
		})
	}
}

func TestHandleRegister_Duplicate(test *testing.T) {
	handler, _ := setupHandler()
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, noopJWTMiddleware)

	body := map[string]string{"email": "dup@example.com", "password": "strongpass", "first_name": "J", "last_name": "D"}

	request := httptest.NewRequest("POST", "/auth/register", jsonBody(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		test.Fatalf("expected 201, got %d", recorder.Code)
	}

	request = httptest.NewRequest("POST", "/auth/register", jsonBody(body))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		test.Errorf("expected 409, got %d", recorder.Code)
	}

	responseEnvelope := decodeEnvelope(test, recorder)
	if responseEnvelope.Error == nil || responseEnvelope.Error.Code != "DUPLICATE" {
		test.Errorf("expected DUPLICATE error code, got %+v", responseEnvelope.Error)
	}
}

func TestHandleLogin(test *testing.T) {
	handler, useCase := setupHandler()
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, noopJWTMiddleware)

	_, err := useCase.Register(context.Background(), "First", "Last", "login@example.com", "strongpass")
	if err != nil {
		test.Fatalf("failed to register seed user: %v", err)
	}

	tests := []struct {
		name       string
		body       any
		wantStatus int
		wantErr    string
	}{
		{
			name:       "success",
			body:       map[string]string{"email": "login@example.com", "password": "strongpass"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong password",
			body:       map[string]string{"email": "login@example.com", "password": "wrongpass"},
			wantStatus: http.StatusUnauthorized,
			wantErr:    "INVALID_CREDENTIALS",
		},
		{
			name:       "unknown email",
			body:       map[string]string{"email": "nobody@example.com", "password": "strongpass"},
			wantStatus: http.StatusUnauthorized,
			wantErr:    "INVALID_CREDENTIALS",
		},
	}

	for _, testCase := range tests {
		test.Run(testCase.name, func(test *testing.T) {
			request := httptest.NewRequest("POST", "/auth/login", jsonBody(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)

			if recorder.Code != testCase.wantStatus {
				test.Errorf("expected status %d, got %d", testCase.wantStatus, recorder.Code)
			}

			responseEnvelope := decodeEnvelope(test, recorder)
			if testCase.wantErr != "" {
				if responseEnvelope.Error == nil || responseEnvelope.Error.Code != testCase.wantErr {
					test.Errorf("expected error code %q, got %+v", testCase.wantErr, responseEnvelope.Error)
				}
			}
		})
	}
}

func TestHandleRefresh(test *testing.T) {
	handler, useCase := setupHandler()
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, noopJWTMiddleware)

	pair, err := useCase.Register(context.Background(), "First", "Last", "refresh@example.com", "strongpass")
	if err != nil {
		test.Fatalf("failed to register seed user: %v", err)
	}

	tests := []struct {
		name       string
		body       any
		wantStatus int
		wantErr    string
	}{
		{
			name: "success",
			body: map[string]string{
				"access_token":  pair.AccessToken,
				"refresh_token": pair.RefreshToken,
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "bad refresh token",
			body: map[string]string{
				"access_token":  pair.AccessToken,
				"refresh_token": uuid.New().String(),
			},
			wantStatus: http.StatusUnauthorized,
			wantErr:    "INVALID_TOKEN",
		},
		{
			name:       "missing fields",
			body:       map[string]string{},
			wantStatus: http.StatusUnprocessableEntity,
			wantErr:    "VALIDATION_ERROR",
		},
	}

	for _, testCase := range tests {
		test.Run(testCase.name, func(test *testing.T) {
			request := httptest.NewRequest("POST", "/auth/refresh", jsonBody(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)

			if recorder.Code != testCase.wantStatus {
				test.Errorf("expected status %d, got %d", testCase.wantStatus, recorder.Code)
			}

			responseEnvelope := decodeEnvelope(test, recorder)
			if testCase.wantErr != "" {
				if responseEnvelope.Error == nil || responseEnvelope.Error.Code != testCase.wantErr {
					test.Errorf("expected error code %q, got %+v", testCase.wantErr, responseEnvelope.Error)
				}
			}
		})
	}
}
