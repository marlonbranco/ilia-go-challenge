package usecase

import (
	"context"
	"fmt"

	"ms-users/internal/domain"

	"github.com/google/uuid"
)

type UserUseCase struct {
	userRepository domain.UserRepository
}


func NewUserUseCase(userRepository domain.UserRepository) *UserUseCase {
	return &UserUseCase{
		userRepository: userRepository,
	}
}

func (useCase *UserUseCase) FindAll(ctx context.Context) ([]domain.User, error) {
	return useCase.userRepository.FindAll(ctx)
}

func (useCase *UserUseCase) FindByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return useCase.userRepository.FindByID(ctx, id)
}

type UserUpdateRequest struct {
	FirstName string
	LastName  string
	Email     string
	Password  string
}

func (useCase *UserUseCase) Update(ctx context.Context, id uuid.UUID, requestingUserID string, req UserUpdateRequest) (domain.User, error) {
	if id.String() != requestingUserID {
		return domain.User{}, fmt.Errorf(ErrUnauthorizedUpdateMsg)
	}

	user, err := useCase.userRepository.FindByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}

	if req.FirstName != "" {
		user.FirstName = req.FirstName
	}
	if req.LastName != "" {
		user.LastName = req.LastName
	}
	if req.Email != "" {
		user.Email = req.Email
	}

	if req.Password != "" {
		tempUser, err := domain.NewUser(user.FirstName, user.LastName, user.Email, req.Password)
		if err != nil {
			return domain.User{}, err
		}
		user.PasswordHash = tempUser.PasswordHash
	}

	if err := useCase.userRepository.Update(ctx, user); err != nil {
		return domain.User{}, err
	}

	return user, nil
}

func (useCase *UserUseCase) Delete(ctx context.Context, id uuid.UUID, requestingUserID string) error {
	if id.String() != requestingUserID {
		return fmt.Errorf(ErrUnauthorizedDeleteMsg)
	}

	return useCase.userRepository.Delete(ctx, id)
}
