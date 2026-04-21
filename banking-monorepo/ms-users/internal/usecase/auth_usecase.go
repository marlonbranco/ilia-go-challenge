package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"ms-users/internal/domain"

	"github.com/google/uuid"
)

const (
	errLoginFailedMsg               = "login failed: %w"
	errRefreshTokenNotFoundMsg      = "refresh token not found: %w"
	errInvalidUserIDMsg             = "invalid user id: %w"
	errFailedToStoreRefreshTokenMsg = "failed to store refresh token: %w"
)

type RegisterRequest struct {
	FirstName string
	LastName  string
	Email     string
	Password  string
}

type LoginRequest struct {
	Email    string
	Password string
}

type AuthUseCase struct {
	repository   domain.UserRepository
	tokens       domain.TokenStore
	tokenSvc     domain.TokenService
	walletClient domain.WalletClient
}

func NewAuthUseCase(repo domain.UserRepository, tokens domain.TokenStore, tokenSvc domain.TokenService) *AuthUseCase {
	return &AuthUseCase{
		repository: repo,
		tokens:     tokens,
		tokenSvc:   tokenSvc,
	}
}

func (useCase *AuthUseCase) WithWalletClient(wc domain.WalletClient) *AuthUseCase {
	useCase.walletClient = wc
	return useCase
}

func (useCase *AuthUseCase) Register(ctx context.Context, req RegisterRequest) (domain.TokenPair, error) {
	user, err := domain.NewUser(req.FirstName, req.LastName, req.Email, req.Password)
	if err != nil {
		return domain.TokenPair{}, err
	}

	user, err = useCase.repository.Create(ctx, user)
	if err != nil {
		return domain.TokenPair{}, err
	}

	if useCase.walletClient != nil {
		if _, err := useCase.walletClient.ValidateUser(ctx, user.ID.String()); err != nil {
			slog.Warn("wallet init failed on register", "user_id", user.ID.String(), "error", err)
		}
	}

	return useCase.generateTokenPair(ctx, user)
}

func (useCase *AuthUseCase) Login(ctx context.Context, req LoginRequest) (domain.TokenPair, error) {
	user, err := useCase.repository.FindByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.TokenPair{}, fmt.Errorf(errLoginFailedMsg, domain.ErrInvalidCredentials)
		}
		return domain.TokenPair{}, err
	}

	if !user.CheckPassword(req.Password) {
		return domain.TokenPair{}, fmt.Errorf(errLoginFailedMsg, domain.ErrInvalidCredentials)
	}

	pair, err := useCase.generateTokenPair(ctx, user)
	if err != nil {
		return domain.TokenPair{}, err
	}

	if useCase.walletClient != nil && pair.UserID != "" {
		if bal, err := useCase.walletClient.GetBalance(ctx, pair.UserID); err != nil {
			slog.Warn("wallet balance unavailable", "user_id", pair.UserID, "error", err)
		} else {
			pair.Balance = &bal
		}
	}

	return pair, nil
}

func (useCase *AuthUseCase) RefreshToken(ctx context.Context, accessToken, refreshToken string) (domain.TokenPair, error) {
	userID, email, err := useCase.tokenSvc.ParseUnvalidated(accessToken)
	if err != nil {
		return domain.TokenPair{}, err
	}

	exists, err := useCase.tokens.Exists(ctx, userID, refreshToken)
	if err != nil {
		return domain.TokenPair{}, err
	}
	if !exists {
		return domain.TokenPair{}, fmt.Errorf(errRefreshTokenNotFoundMsg, domain.ErrInvalidToken)
	}

	if err := useCase.tokens.Delete(ctx, userID, refreshToken); err != nil {
		return domain.TokenPair{}, err
	}

	uid, err := uuid.Parse(userID)
	if err != nil {
		return domain.TokenPair{}, fmt.Errorf(errInvalidUserIDMsg, domain.ErrInvalidToken)
	}

	user := domain.User{ID: uid, Email: email}
	return useCase.generateTokenPair(ctx, user)
}

func (useCase *AuthUseCase) Logout(ctx context.Context, userID, refreshToken string) error {
	return useCase.tokens.Delete(ctx, userID, refreshToken)
}

func (useCase *AuthUseCase) generateTokenPair(ctx context.Context, user domain.User) (domain.TokenPair, error) {
	accessToken, refreshTokenID, refreshTTL, err := useCase.tokenSvc.GenerateTokenPair(user.ID.String(), user.Email)
	if err != nil {
		return domain.TokenPair{}, err
	}

	if err := useCase.tokens.Store(ctx, user.ID.String(), refreshTokenID, refreshTTL); err != nil {
		return domain.TokenPair{}, fmt.Errorf(errFailedToStoreRefreshTokenMsg, err)
	}

	return domain.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenID,
		UserID:       user.ID.String(),
	}, nil
}
