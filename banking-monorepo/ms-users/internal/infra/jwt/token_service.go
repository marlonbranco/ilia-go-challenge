package jwt

import (
	"fmt"
	"time"

	"ms-users/internal/domain"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	DefaultAccessTTL  = 15 * time.Minute
	DefaultRefreshTTL = 7 * 24 * time.Hour
)

type TokenService struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokenService(secret string, accessTTL, refreshTTL time.Duration) *TokenService {
	return &TokenService{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (s *TokenService) GenerateTokenPair(userID, email string) (accessToken, refreshTokenID string, refreshTTL time.Duration, err error) {
	now := time.Now().UTC()
	claims := gojwt.MapClaims{
		"sub":   userID,
		"email": email,
		"iat":   now.Unix(),
		"exp":   now.Add(s.accessTTL).Unix(),
		"jti":   uuid.New().String(),
	}

	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to sign access token: %w", err)
	}

	return signed, uuid.New().String(), s.refreshTTL, nil
}

func (s *TokenService) ParseUnvalidated(tokenStr string) (userID, email string, err error) {
	parser := gojwt.NewParser(gojwt.WithoutClaimsValidation())
	token, err := parser.Parse(tokenStr, func(t *gojwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return "", "", fmt.Errorf("invalid access token: %w", domain.ErrInvalidToken)
	}

	claims, ok := token.Claims.(gojwt.MapClaims)
	if !ok {
		return "", "", fmt.Errorf("invalid claims: %w", domain.ErrInvalidToken)
	}

	uid, ok := claims["sub"].(string)
	if !ok || uid == "" {
		return "", "", fmt.Errorf("missing sub claim: %w", domain.ErrInvalidToken)
	}

	emailVal, _ := claims["email"].(string)
	return uid, emailVal, nil
}
