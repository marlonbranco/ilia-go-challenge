package usecase_test

import (
	"context"
	"strings"
	"testing"
	"github.com/google/uuid"

	"ms-users/internal/domain"
	"ms-users/internal/usecase"
)

func TestUserUseCaseFindAll(test *testing.T) {
	repo := newFakeUserRepo()
	newUserUseCase := usecase.NewUserUseCase(repo)

	user, _ := domain.NewUser("John", "Doe", "john@example.com", "password")
	repo.Create(context.Background(), user)

	users, err := newUserUseCase.FindAll(context.Background())
	if err != nil {
		test.Fatalf(errUnexpected, err)
	}

	if len(users) != 1 {
		test.Errorf("expected 1 user, got %d", len(users))
	}
}

func TestUserUseCaseUpdate(test *testing.T) {
	repo := newFakeUserRepo()
	newUserUseCase := usecase.NewUserUseCase(repo)

	user, _ := domain.NewUser("Original", "Name", "original@example.com", "password")
	user, _ = repo.Create(context.Background(), user)

	test.Run("updates successfully when ID matches token", func(test *testing.T) {
		req := usecase.UserUpdateRequest{
			FirstName: "Updated",
		}
		
		updated, err := newUserUseCase.Update(context.Background(), user.ID, user.ID.String(), req)
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if updated.FirstName != "Updated" {
			test.Errorf("expected name to be Updated, got %s", updated.FirstName)
		}
	})

	test.Run("fails when ID does not match", func(test *testing.T) {
		req := usecase.UserUpdateRequest{
			FirstName: "Hacked",
		}
		
		_, err := newUserUseCase.Update(context.Background(), user.ID, uuid.New().String(), req)
		if err == nil || !strings.Contains(err.Error(), usecase.ErrUnauthorizedUpdateMsg) {
			test.Fatalf("expected unauthorized error, got %v", err)
		}
	})
}

func TestUserUseCaseDelete(test *testing.T) {
	repo := newFakeUserRepo()
	newUserUseCase := usecase.NewUserUseCase(repo)

	user, _ := domain.NewUser("Delete", "Me", "delete@example.com", "password")
	user, _ = repo.Create(context.Background(), user)

	test.Run("fails when ID does not match", func(test *testing.T) {
		err := newUserUseCase.Delete(context.Background(), user.ID, uuid.New().String())
		if err == nil || !strings.Contains(err.Error(), usecase.ErrUnauthorizedDeleteMsg) {
			test.Fatalf("expected unauthorized error, got %v", err)
		}
	})

	test.Run("succeeds when ID matches", func(test *testing.T) {
		err := newUserUseCase.Delete(context.Background(), user.ID, user.ID.String())
		if err != nil {
			test.Fatalf("expected no error, got %v", err)
		}

		_, err = repo.FindByID(context.Background(), user.ID)
		if err == nil {
			test.Fatalf("expected user to be deleted/not found")
		}
	})
}
