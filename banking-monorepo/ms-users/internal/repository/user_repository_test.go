package repository_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"ms-users/internal/domain"
	userRepository "ms-users/internal/repository"

	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	testPassword               = "password"
	testDuplicateEmail         = "duplicate@example.com"
	testNewHash                = "newhash"
	errTestFailedToCreateMsg   = "failed to create user: %v"
	errTestExpectedNoErrorMsg  = "expected no error, got %v"
	errTestExpectedNotFoundMsg = "expected ErrNotFound, got %v"
)

func setupTestDB(test *testing.T) (*userRepository.PostgresUserRepository, func()) {
	ctx := context.Background()
	
	dbName := "users_test"
	dbUser := "user"
	dbPassword := testPassword
	
	pgContainer, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(5*time.Second),
		),
	)
	if err != nil {
		test.Fatalf("failed to start postgres container: %v", err)
	}

	connectionUrl, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		test.Fatalf("failed to get connection string: %v", err)
	}

	migrationsDir, err := filepath.Abs("migrations")
	if err != nil {
		test.Fatalf("failed to get absolute path for migrations: %v", err)
	}

	repository, err := userRepository.NewPostgresUserRepository(ctx, connectionUrl, "file://"+migrationsDir)
	if err != nil {
		test.Fatalf("failed to create repository: %v", err)
	}

	cleanup := func() {
		repository.Close()
		if err := pgContainer.Terminate(ctx); err != nil {
			test.Fatalf("failed to terminate postgres container: %v", err)
		}
	}

	return repository, cleanup
}

func TestPostgresUserRepositoryCreate(test *testing.T) {
	repository, cleanup := setupTestDB(test)
	test.Cleanup(cleanup)

	ctx := context.Background()

	test.Run("creates a user successfully", func(test *testing.T) {
		user, err := domain.NewUser("First", "Last", "test1@example.com", testPassword)
		if err != nil {
			test.Fatalf("failed to create domain user: %v", err)
		}

		createdUser, err := repository.Create(ctx, user)
		if err != nil {
			test.Fatalf(errTestFailedToCreateMsg, err)
		}

		if createdUser.ID != user.ID {
			test.Errorf("expected ID %s, got %s", user.ID, createdUser.ID)
		}

		if createdUser.Email != user.Email {
			test.Errorf("expected email %s, got %s", user.Email, createdUser.Email)
		}
	})

	test.Run("duplicate email returns ErrDuplicate", func(test *testing.T) {
		user1, _ := domain.NewUser("First", "Last", testDuplicateEmail, testPassword)
		_, err := repository.Create(ctx, user1)
		if err != nil {
			test.Fatalf("expected first insert to succeed, got: %v", err)
		}

		user2, _ := domain.NewUser("First", "Last", testDuplicateEmail, testPassword)
		_, err = repository.Create(ctx, user2)
		if !errors.Is(err, domain.ErrDuplicate) {
			test.Errorf("expected ErrDuplicate, got %v", err)
		}
	})
}

func TestPostgresUserRepositoryFindByEmail(test *testing.T) {
	repository, cleanup := setupTestDB(test)
	test.Cleanup(cleanup)

	ctx := context.Background()

	test.Run("finds existing user by email", func(test *testing.T) {
		user, _ := domain.NewUser("First", "Last", "findbyemail@example.com", testPassword)
		createdUser, err := repository.Create(ctx, user)
		if err != nil {
			test.Fatalf(errTestFailedToCreateMsg, err)
		}

		foundUser, err := repository.FindByEmail(ctx, user.Email)
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		if foundUser.ID != createdUser.ID {
			test.Errorf("expected ID %s, got %s", createdUser.ID, foundUser.ID)
		}
	})

	test.Run("non-existent email returns ErrNotFound", func(test *testing.T) {
		_, err := repository.FindByEmail(ctx, "nonexistent@example.com")
		if !errors.Is(err, domain.ErrNotFound) {
			test.Errorf(errTestExpectedNotFoundMsg, err)
		}
	})
}

func TestPostgresUserRepositoryFindByID(test *testing.T) {
	repository, cleanup := setupTestDB(test)
	test.Cleanup(cleanup)

	ctx := context.Background()

	test.Run("finds existing user by id", func(test *testing.T) {
		user, _ := domain.NewUser("First", "Last", "findbyid@example.com", testPassword)
		createdUser, err := repository.Create(ctx, user)
		if err != nil {
			test.Fatalf(errTestFailedToCreateMsg, err)
		}

		foundUser, err := repository.FindByID(ctx, createdUser.ID)
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		if foundUser.Email != createdUser.Email {
			test.Errorf("expected Email %s, got %s", createdUser.Email, foundUser.Email)
		}
	})

	test.Run("non-existent id returns ErrNotFound", func(test *testing.T) {
		_, err := repository.FindByID(ctx, uuid.New())
		if !errors.Is(err, domain.ErrNotFound) {
			test.Errorf(errTestExpectedNotFoundMsg, err)
		}
	})
}

func TestPostgresUserRepositoryUpdate(test *testing.T) {
	repository, cleanup := setupTestDB(test)
	test.Cleanup(cleanup)

	ctx := context.Background()

	test.Run("updates user successfully", func(test *testing.T) {
		user, _ := domain.NewUser("First", "Last", "update@example.com", testPassword)
		createdUser, err := repository.Create(ctx, user)
		if err != nil {
			test.Fatalf(errTestFailedToCreateMsg, err)
		}

		createdUser.PasswordHash = testNewHash
		time.Sleep(1 * time.Millisecond)

		err = repository.Update(ctx, createdUser)
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		updatedUser, err := repository.FindByID(ctx, createdUser.ID)
		if err != nil {
			test.Fatalf("failed to find updated user: %v", err)
		}

		if updatedUser.PasswordHash != testNewHash {
			test.Errorf("expected new hash, got %s", updatedUser.PasswordHash)
		}

		if !updatedUser.UpdatedAt.After(createdUser.UpdatedAt) {
			test.Errorf("expected updated at to be after created at")
		}
	})

	test.Run("updating non-existent user returns ErrNotFound", func(test *testing.T) {
		user, _ := domain.NewUser("First", "Last", "notfound@example.com", testPassword)
		err := repository.Update(ctx, user)
		if !errors.Is(err, domain.ErrNotFound) {
			test.Errorf(errTestExpectedNotFoundMsg, err)
		}
	})
}

func TestPostgresUserRepositoryFindAll(test *testing.T) {
	repository, cleanup := setupTestDB(test)
	test.Cleanup(cleanup)

	ctx := context.Background()

	test.Run("finds all users excluding deleted", func(test *testing.T) {
		user1, _ := domain.NewUser("First", "Last", "findall1@example.com", testPassword)
		repository.Create(ctx, user1)
		
		user2, _ := domain.NewUser("Second", "Last", "findall2@example.com", testPassword)
		createdUser2, _ := repository.Create(ctx, user2)

		repository.Delete(ctx, createdUser2.ID)

		users, err := repository.FindAll(ctx)
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		foundUser1 := false
		foundUser2 := false
		for _, user := range users {
			if user.Email == "findall1@example.com" { foundUser1 = true }
			if user.Email == "findall2@example.com" { foundUser2 = true }
		}

		if !foundUser1 {
			test.Errorf("expected to find active user1")
		}
		if foundUser2 {
			test.Errorf("expected soft-deleted user2 to be excluded")
		}
	})
}

func TestPostgresUserRepositoryDelete(test *testing.T) {
	repository, cleanup := setupTestDB(test)
	test.Cleanup(cleanup)

	ctx := context.Background()

	test.Run("soft deletes user successfully", func(test *testing.T) {
		user, _ := domain.NewUser("Delete", "Me", "delete@example.com", testPassword)
		createdUser, _ := repository.Create(ctx, user)

		err := repository.Delete(ctx, createdUser.ID)
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		_, err = repository.FindByID(ctx, createdUser.ID)
		if !errors.Is(err, domain.ErrNotFound) {
			test.Errorf(errTestExpectedNotFoundMsg, err)
		}

		err = repository.Delete(ctx, createdUser.ID)
		if err != nil {
			test.Fatalf("expected delete to be idempotent, got: %v", err)
		}
	})

	test.Run("deleting non-existent id returns ErrNotFound", func(test *testing.T) {
		err := repository.Delete(ctx, uuid.New())
		if !errors.Is(err, domain.ErrNotFound) {
			test.Errorf(errTestExpectedNotFoundMsg, err)
		}
	})
}
