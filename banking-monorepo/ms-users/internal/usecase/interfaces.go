package usecase

import (
	"context"

	"ms-users/internal/domain"

	"github.com/google/uuid"
)

type AuthService interface {
	Register(ctx context.Context, req RegisterRequest) (domain.TokenPair, error)
	Login(ctx context.Context, req LoginRequest) (domain.TokenPair, error)
	RefreshToken(ctx context.Context, accessToken, refreshToken string) (domain.TokenPair, error)
	Logout(ctx context.Context, userID, refreshToken string) error
}

type UserService interface {
	FindAll(ctx context.Context) ([]domain.User, error)
	FindByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	Update(ctx context.Context, id uuid.UUID, requestingUserID string, req UserUpdateRequest) (domain.User, error)
	Delete(ctx context.Context, id uuid.UUID, requestingUserID string) error
}
