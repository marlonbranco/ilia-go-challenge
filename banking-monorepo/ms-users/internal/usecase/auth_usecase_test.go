package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"ms-users/internal/domain"
	"ms-users/internal/usecase"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	firstName 					= "First"
	lastName 					= "Last"

	errUnexpected 				= "unexpected error: %v"
	errNilReceived 				= "expected error %v, got nil"
	errExpectedMessage 			= "expected error %v, got %v"
	errDuplicateMsg 			= "duplicate: %w"
	errNotFoundMsg  			= "not found: %w"
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
		return domain.User{}, fmt.Errorf(errDuplicateMsg, domain.ErrDuplicate)
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
		return domain.User{}, fmt.Errorf(errNotFoundMsg, domain.ErrNotFound)
	}
	return user, nil
}

func (repository *fakeUserRepo) FindByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	repository.mutex.RLock()
	defer repository.mutex.RUnlock()
	user, ok := repository.byID[id]
	if !ok || user.IsDeleted {
		return domain.User{}, fmt.Errorf(errNotFoundMsg, domain.ErrNotFound)
	}
	return user, nil
}

func (repository *fakeUserRepo) Update(_ context.Context, user domain.User) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if _, ok := repository.byID[user.ID]; !ok {
		return fmt.Errorf(errNotFoundMsg, domain.ErrNotFound)
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
		return fmt.Errorf(errNotFoundMsg, domain.ErrNotFound)
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
	if time.Now().After(exp) {
		return false, nil
	}
	return true, nil
}

func (store *fakeTokenStore) Delete(_ context.Context, userID, tokenID string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	delete(store.tokens, store.key(userID, tokenID))
	return nil
}

const testSecret = "ILIACHALLENGE"

func newTestUseCase(repo domain.UserRepository, store domain.TokenStore) *usecase.AuthUseCase {
	return usecase.NewAuthUseCase(repo, store, testSecret)
}

func parseAccessToken(test *testing.T, tokenStr string) jwt.MapClaims {
	test.Helper()
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf(usecase.ErrUnexpectedSigningMethodMsg, token.Header["alg"])
		}
		return []byte(testSecret), nil
	})
	if err != nil {
		test.Fatalf("failed to parse access token: %v", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		test.Fatal("invalid access token claims")
	}
	return claims
}

func assertTokenPair(test *testing.T, pair domain.TokenPair, expectedEmail string) {
	test.Helper()

	claims := parseAccessToken(test, pair.AccessToken)

	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		test.Error("access token missing 'sub' claim")
	}

	email, ok := claims["email"].(string)
	if !ok || email != expectedEmail {
		test.Errorf("expected email claim %q, got %q", expectedEmail, email)
	}

	if _, ok := claims["iat"]; !ok {
		test.Error("access token missing 'iat' claim")
	}

	if _, ok := claims["exp"]; !ok {
		test.Error("access token missing 'exp' claim")
	}

	jti, ok := claims["jti"].(string)
	if !ok || jti == "" {
		test.Error("access token missing 'jti' claim")
	}
	if _, err := uuid.Parse(jti); err != nil {
		test.Errorf("jti is not a valid UUID: %v", err)
	}

	exp, ok := claims["exp"].(float64)
	if ok {
		expTime := time.Unix(int64(exp), 0)
		diff := time.Until(expTime)
		if diff < 14*time.Minute || diff > 16*time.Minute {
			test.Errorf("expected access token TTL ~15min, got %v", diff)
		}
	}

	if _, err := uuid.Parse(pair.RefreshToken); err != nil {
		test.Errorf("refresh token is not a valid UUID: %v", err)
	}
}


func TestRegister(test *testing.T) {
	tests := []struct {
		name      string
		email     string
		password  string
		wantErr   error
		setupRepo func(*fakeUserRepo)
	}{
		{
			name:     "success",
			email:    "test@example.com",
			password: "strongpassword",
		},
		{
			name:     "duplicate email",
			email:    "dup@example.com",
			password: "strongpassword",
			wantErr:  domain.ErrDuplicate,
			setupRepo: func(r *fakeUserRepo) {
				u, _ := domain.NewUser(firstName, lastName, "dup@example.com", "somepassword")
				r.Create(context.Background(), u)
			},
		},
		{
			name:     "invalid email",
			email:    "not-an-email",
			password: "strongpassword",
			wantErr:  domain.ErrInvalidEmail,
		},
		{
			name:     "empty password",
			email:    "test@example.com",
			password: "",
			wantErr:  domain.ErrInvalidPassword,
		},
	}

	for _, tc := range tests {
		test.Run(tc.name, func(test *testing.T) {
			repository := newFakeUserRepo()
			store := newFakeTokenStore()
			if tc.setupRepo != nil {
				tc.setupRepo(repository)
			}
			useCase := newTestUseCase(repository, store)

			pair, err := useCase.Register(context.Background(), firstName, lastName, tc.email, tc.password)

			if tc.wantErr != nil {
				if err == nil {
					test.Fatalf(errNilReceived, tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					test.Fatalf(errExpectedMessage, tc.wantErr, err)
				}
				return
			}

			if err != nil {
				test.Fatalf(errUnexpected, err)
			}

			assertTokenPair(test, pair, tc.email)
		})
	}
}


func TestLogin(test *testing.T) {
	tests := []struct {
		name      string
		email     string
		password  string
		wantErr   error
		setupRepo func(*fakeUserRepo)
	}{
		{
			name:     "success",
			email:    "login@example.com",
			password: "correctpassword",
			setupRepo: func(r *fakeUserRepo) {
				u, _ := domain.NewUser(firstName, lastName, "login@example.com", "correctpassword")
				r.Create(context.Background(), u)
			},
		},
		{
			name:     "unknown email",
			email:    "unknown@example.com",
			password: "somepassword",
			wantErr:  domain.ErrInvalidCredentials,
		},
		{
			name:     "wrong password",
			email:    "login2@example.com",
			password: "wrongpassword",
			wantErr:  domain.ErrInvalidCredentials,
			setupRepo: func(r *fakeUserRepo) {
				u, _ := domain.NewUser(firstName, lastName, "login2@example.com", "correctpassword")
				r.Create(context.Background(), u)
			},
		},
	}

	for _, tc := range tests {
		test.Run(tc.name, func(test *testing.T) {
			repository := newFakeUserRepo()
			store := newFakeTokenStore()
			if tc.setupRepo != nil {
				tc.setupRepo(repository)
			}
			useCase := newTestUseCase(repository, store)

			pair, err := useCase.Login(context.Background(), tc.email, tc.password)

			if tc.wantErr != nil {
				if err == nil {
					test.Fatalf(errNilReceived, tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					test.Fatalf(errExpectedMessage, tc.wantErr, err)
				}
				return
			}

			if err != nil {
				test.Fatalf(errUnexpected, err)
			}

			assertTokenPair(test, pair, tc.email)
		})
	}
}

func TestRefreshToken(test *testing.T) {
	registerUser := func(test *testing.T, useCase *usecase.AuthUseCase, email, password string) domain.TokenPair {
		test.Helper()
		pair, err := useCase.Register(context.Background(), firstName, lastName, email, password)
		if err != nil {
			test.Fatalf("failed to register user: %v", err)
		}
		return pair
	}

	tests := []struct {
		name    string
		setup   func(test *testing.T) (*usecase.AuthUseCase, string, string)
		wantErr error
	}{
		{
			name: "success",
			setup: func(test *testing.T) (*usecase.AuthUseCase, string, string) {
				repository := newFakeUserRepo()
				store := newFakeTokenStore()
				useCase := newTestUseCase(repository, store)
				pair := registerUser(test, useCase, "refresh@example.com", "password")
				return useCase, pair.AccessToken, pair.RefreshToken
			},
		},
		{
			name: "unknown refresh token",
			setup: func(test *testing.T) (*usecase.AuthUseCase, string, string) {
				repository := newFakeUserRepo()
				store := newFakeTokenStore()
				useCase := newTestUseCase(repository, store)
				pair := registerUser(test, useCase, "refresh2@example.com", "password")
				return useCase, pair.AccessToken, uuid.New().String() 
			},
			wantErr: domain.ErrInvalidToken,
		},
		{
			name: "invalid access token format",
			setup: func(test *testing.T) (*usecase.AuthUseCase, string, string) {
				repository := newFakeUserRepo()
				store := newFakeTokenStore()
				useCase := newTestUseCase(repository, store)
				return useCase, "not.a.jwt", uuid.New().String()
			},
			wantErr: domain.ErrInvalidToken,
		},
	}

	for _, tc := range tests {
		test.Run(tc.name, func(test *testing.T) {
			useCase, accessToken, refreshToken := tc.setup(test)

			pair, err := useCase.RefreshToken(context.Background(), accessToken, refreshToken)

			if tc.wantErr != nil {
				if err == nil {
					test.Fatalf(errNilReceived, tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					test.Fatalf(errExpectedMessage, tc.wantErr, err)
				}
				return
			}

			if err != nil {
				test.Fatalf(errUnexpected, err)
			}

			assertTokenPair(test, pair, "refresh@example.com")

			if pair.RefreshToken == refreshToken {
				test.Error("expected new refresh token after rotation, got the same one")
			}
		})
	}
}

func TestLogout(test *testing.T) {
	tests := []struct {
		name    string
		setup   func(test *testing.T) (*usecase.AuthUseCase, string, string)
		wantErr error
	}{
		{
			name: "success",
			setup: func(test *testing.T) (*usecase.AuthUseCase, string, string) {
				repository := newFakeUserRepo()
				store := newFakeTokenStore()
				useCase := newTestUseCase(repository, store)

				pair, err := useCase.Register(context.Background(), firstName, lastName, "logout@example.com", "password")
				if err != nil {
					test.Fatalf("failed to register: %v", err)
				}

				claims := parseAccessToken(test, pair.AccessToken)
				userID := claims["sub"].(string)
				return useCase, userID, pair.RefreshToken
			},
		},
		{
			name: "non-existent token is idempotent",
			setup: func(test *testing.T) (*usecase.AuthUseCase, string, string) {
				repository := newFakeUserRepo()
				store := newFakeTokenStore()
				useCase := newTestUseCase(repository, store)
				return useCase, uuid.New().String(), uuid.New().String()
			},
		},
	}

	for _, tc := range tests {
		test.Run(tc.name, func(test *testing.T) {
			useCase, userID, refreshToken := tc.setup(test)

			err := useCase.Logout(context.Background(), userID, refreshToken)

			if tc.wantErr != nil {
				if err == nil {
					test.Fatalf(errNilReceived, tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					test.Fatalf(errExpectedMessage, tc.wantErr, err)
				}
				return
			}

			if err != nil {
				test.Fatalf(errUnexpected, err)
			}
		})
	}
}

func TestLogoutInvalidatesRefreshToken(test *testing.T) {
	repository := newFakeUserRepo()
	store := newFakeTokenStore()
	useCase := newTestUseCase(repository, store)

	pair, err := useCase.Register(context.Background(), firstName, lastName, "invalidate@example.com", "password")
	if err != nil {
		test.Fatalf("failed to register: %v", err)
	}

	claims := parseAccessToken(test, pair.AccessToken)
	userID := claims["sub"].(string)

	if err := useCase.Logout(context.Background(), userID, pair.RefreshToken); err != nil {
		test.Fatalf("failed to logout: %v", err)
	}

	_, err = useCase.RefreshToken(context.Background(), pair.AccessToken, pair.RefreshToken)
	if !errors.Is(err, domain.ErrInvalidToken) {
		test.Fatalf("expected ErrInvalidToken after logout, got %v", err)
	}
}
