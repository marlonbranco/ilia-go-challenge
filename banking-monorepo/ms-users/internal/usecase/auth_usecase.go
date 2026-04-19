package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ms-users/internal/domain"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	defaultAccessTTL				= 15 * time.Minute
	defaultRefreshTTL 				= 7 * 24 * time.Hour

	errLoginFailedMsg               = "login failed: %w"
	errInvalidAccessTokenMsg 		= "invalid access token: %w"
	errInvalidClaimsMsg             = "invalid claims: %w"
	errMissingSubClaimMsg           = "missing sub claim: %w"
	errRefreshTokenNotFoundMsg      = "refresh token not found: %w"
	errInvalidUserIDMsg             = "invalid user id: %w"
	errFailedToSignAccessTokenMsg   = "failed to sign access token: %w"
	errFailedToStoreRefreshTokenMsg = "failed to store refresh token: %w"
)

type AuthUseCase struct {
	repository domain.UserRepository
	tokens     domain.TokenStore
	jwtSecret  []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewAuthUseCase(repo domain.UserRepository, tokens domain.TokenStore, jwtSecret string) *AuthUseCase {
	return &AuthUseCase{
		repository: repo,
		tokens:     tokens,
		jwtSecret:  []byte(jwtSecret),
		accessTTL:  defaultAccessTTL,
		refreshTTL: defaultRefreshTTL,
	}
}

func (useCase *AuthUseCase) Register(ctx context.Context, firstName, lastName, email, password string) (domain.TokenPair, error) {
	user, err := domain.NewUser(firstName, lastName, email, password)
	if err != nil {
		return domain.TokenPair{}, err
	}

	user, err = useCase.repository.Create(ctx, user)
	if err != nil {
		return domain.TokenPair{}, err
	}

	return useCase.generateTokenPair(ctx, user)
}

func (useCase *AuthUseCase) Login(ctx context.Context, email, password string) (domain.TokenPair, error) {
	user, err := useCase.repository.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.TokenPair{}, fmt.Errorf(errLoginFailedMsg, domain.ErrInvalidCredentials)
		}
		return domain.TokenPair{}, err
	}

	if !user.CheckPassword(password) {
		return domain.TokenPair{}, fmt.Errorf(errLoginFailedMsg, domain.ErrInvalidCredentials)
	}

	return useCase.generateTokenPair(ctx, user)
}

func (useCase *AuthUseCase) RefreshToken(ctx context.Context, accessToken, refreshToken string) (domain.TokenPair, error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, err := parser.Parse(accessToken, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf(ErrUnexpectedSigningMethodMsg, t.Header["alg"])
		}
		return useCase.jwtSecret, nil
	})
	if err != nil {
		return domain.TokenPair{}, fmt.Errorf(errInvalidAccessTokenMsg, domain.ErrInvalidToken)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return domain.TokenPair{}, fmt.Errorf(errInvalidClaimsMsg, domain.ErrInvalidToken)
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		return domain.TokenPair{}, fmt.Errorf(errMissingSubClaimMsg, domain.ErrInvalidToken)
	}

	email, _ := claims["email"].(string)

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
	now := time.Now().UTC()
	jti := uuid.New().String()

	claims := jwt.MapClaims{
		"sub":   user.ID.String(),
		"email": user.Email,
		"iat":   now.Unix(),
		"exp":   now.Add(useCase.accessTTL).Unix(),
		"jti":   jti,
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedAccess, err := accessToken.SignedString(useCase.jwtSecret)
	if err != nil {
		return domain.TokenPair{}, fmt.Errorf(errFailedToSignAccessTokenMsg, err)
	}

	refreshTokenID := uuid.New().String()

	if err := useCase.tokens.Store(ctx, user.ID.String(), refreshTokenID, useCase.refreshTTL); err != nil {
		return domain.TokenPair{}, fmt.Errorf(errFailedToStoreRefreshTokenMsg, err)
	}

	return domain.TokenPair{
		AccessToken:  signedAccess,
		RefreshToken: refreshTokenID,
	}, nil
}
