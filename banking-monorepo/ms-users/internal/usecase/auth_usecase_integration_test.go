package usecase_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"ms-users/internal/domain"
	"ms-users/internal/repository"
	"ms-users/internal/usecase"

	"github.com/redis/go-redis/v9"
	testContainers "github.com/testcontainers/testcontainers-go"
	tcPostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcRedis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

const integrationJWTSecret = "ILIACHALLENGE"

func setupIntegration(test *testing.T) (*usecase.AuthUseCase, func()) {
	test.Helper()
	ctx := context.Background()

	pgContainer, err := tcPostgres.RunContainer(ctx,
		testContainers.WithImage("postgres:15-alpine"),
		tcPostgres.WithDatabase("users_test"),
		tcPostgres.WithUsername("user"),
		tcPostgres.WithPassword("password"),
		testContainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		test.Fatalf("failed to start postgres container: %v", err)
	}

	pgConnStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		test.Fatalf("failed to get pg connection string: %v", err)
	}

	migrationsDir, err := filepath.Abs("../repository/migrations")
	if err != nil {
		test.Fatalf("failed to resolve migrations path: %v", err)
	}

	userRepo, err := repository.NewPostgresUserRepository(ctx, pgConnStr, "file://"+migrationsDir)
	if err != nil {
		test.Fatalf("failed to create user repository: %v", err)
	}

	redisContainer, err := tcRedis.RunContainer(ctx,
		testContainers.WithImage("redis:7-alpine"),
		testContainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").
				WithStartupTimeout(15*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start redis container: %v", err)
	}

	redisEndpoint, err := redisContainer.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("failed to get redis endpoint: %v", err)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisEndpoint})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("failed to ping redis: %v", err)
	}

	tokenStore := repository.NewRedisTokenStore(redisClient)

	useCase := usecase.NewAuthUseCase(userRepo, tokenStore, integrationJWTSecret)

	cleanup := func() {
		userRepo.Close()
		redisClient.Close()
		_ = pgContainer.Terminate(ctx)
		_ = redisContainer.Terminate(ctx)
	}

	return useCase, cleanup
}

func TestAuthIntegrationFullLifeCycle(test *testing.T) {
	if testing.Short() {
		test.Skip("skipping integration test in short mode")
	}

	useCase, cleanup := setupIntegration(test)
	test.Cleanup(cleanup)

	ctx := context.Background()
	email := "integration@example.com"
	password := "strongpassword123"

	test.Run("register", func(test *testing.T) {
		pair, err := useCase.Register(ctx, email, password)
		if err != nil {
			test.Fatalf("Register failed: %v", err)
		}
		if pair.AccessToken == "" {
			test.Error("expected non-empty access token")
		}
		if pair.RefreshToken == "" {
			test.Error("expected non-empty refresh token")
		}
	})

	test.Run("duplicate register", func(test *testing.T) {
		_, err := useCase.Register(ctx, email, password)
		if !errors.Is(err, domain.ErrDuplicate) {
			test.Fatalf("expected ErrDuplicate, got %v", err)
		}
	})

	var loginPair domain.TokenPair
	test.Run("login", func(test *testing.T) {
		var err error
		loginPair, err = useCase.Login(ctx, email, password)
		if err != nil {
			test.Fatalf("Login failed: %v", err)
		}
		assertTokenPair(test, loginPair, email)
	})

	test.Run("login wrong password", func(test *testing.T) {
		_, err := useCase.Login(ctx, email, "wrongpassword")
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			test.Fatalf("expected ErrInvalidCredentials, got %v", err)
		}
	})

	var refreshedPair domain.TokenPair
	test.Run("refresh token", func(test *testing.T) {
		var err error
		refreshedPair, err = useCase.RefreshToken(ctx, loginPair.AccessToken, loginPair.RefreshToken)
		if err != nil {
			test.Fatalf("RefreshToken failed: %v", err)
		}
		assertTokenPair(test, refreshedPair, email)

		if refreshedPair.RefreshToken == loginPair.RefreshToken {
			test.Error("expected different refresh token after rotation")
		}
	})

	test.Run("old refresh token invalid after rotation", func(test *testing.T) {
		_, err := useCase.RefreshToken(ctx, loginPair.AccessToken, loginPair.RefreshToken)
		if !errors.Is(err, domain.ErrInvalidToken) {
			test.Fatalf("expected ErrInvalidToken, got %v", err)
		}
	})

	test.Run("logout", func(test *testing.T) {
		claims := parseAccessToken(t, refreshedPair.AccessToken)
		userID := claims["sub"].(string)

		err := useCase.Logout(ctx, userID, refreshedPair.RefreshToken)
		if err != nil {
			test.Fatalf("Logout failed: %v", err)
		}
	})

	test.Run("refresh after logout fails", func(t *testing.T) {
		_, err := useCase.RefreshToken(ctx, refreshedPair.AccessToken, refreshedPair.RefreshToken)
		if !errors.Is(err, domain.ErrInvalidToken) {
			test.Fatalf("expected ErrInvalidToken after logout, got %v", err)
		}
	})
}
