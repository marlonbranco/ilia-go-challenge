package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"ms-users/internal/docs"
	usersgrpc "ms-users/internal/grpc"
	jwtinfra "ms-users/internal/infra/jwt"
	"ms-users/internal/repository"
	transportHttp "ms-users/internal/transport/http"
	"ms-users/internal/usecase"

	"pkg/middleware"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := godotenv.Load(); err != nil {
		slog.Warn("No .env file found, using system environment variables")
	}

	ctx := context.Background()

	port := os.Getenv("PORT")
	if port == "" {
		port = "3002"
	}

	dbURL := os.Getenv("POSTGRES_URL")
	if dbURL == "" {
		slog.Error("POSTGRES_URL environment variable is required")
		os.Exit(1)
	}

	if err := repository.RunMigrations(dbURL, "file://internal/repository/migrations"); err != nil {
		slog.Error("Failed to run migrations", "error", err)
		os.Exit(1)
	}

	userRepo, err := repository.NewPostgresUserRepository(ctx, dbURL)
	if err != nil {
		slog.Error("Failed to initialise user repository", "error", err)
		os.Exit(1)
	}
	defer userRepo.Close()

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		slog.Error("REDIS_URL environment variable is required")
		os.Exit(1)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisURL})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		slog.Error("Failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	tokenStore := repository.NewRedisTokenStore(redisClient)

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		slog.Error("JWT_SECRET environment variable is required")
		os.Exit(1)
	}

	tokenSvc := jwtinfra.NewTokenService(jwtSecret, jwtinfra.DefaultAccessTTL, jwtinfra.DefaultRefreshTTL)
	authUseCase := usecase.NewAuthUseCase(userRepo, tokenStore, tokenSvc)
	userUseCase := usecase.NewUserUseCase(userRepo)

	if certFile := os.Getenv("GRPC_CERT_FILE"); certFile != "" {
		walletAddr := os.Getenv("WALLET_GRPC_ADDR")
		if walletAddr == "" {
			walletAddr = "localhost:50051"
		}
		wc, err := usersgrpc.NewWalletClient(certFile, os.Getenv("GRPC_KEY_FILE"), os.Getenv("GRPC_CA_FILE"), walletAddr)
		if err != nil {
			slog.Error("failed to init wallet gRPC client", "error", err)
			os.Exit(1)
		}
		defer wc.Close()
		authUseCase.WithWalletClient(wc)
	} else {
		slog.Warn("GRPC_CERT_FILE not set — wallet balance enrichment disabled")
	}

	authHandler := transportHttp.NewAuthHandler(authUseCase)

	userHandler := transportHttp.NewUserHandler(userUseCase)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	jwtMiddle := middleware.JWTMiddleware("JWT_SECRET")
	authHandler.RegisterRoutes(mux, jwtMiddle)
	userHandler.RegisterRoutes(mux, jwtMiddle)
	docs.RegisterRoutes(mux)

	rateLimitCounter := repository.NewRedisRateLimitCounter(redisClient)
	loginLimiter := transportHttp.RateLimiter(rateLimitCounter, 5, time.Minute)

	var handler http.Handler = mux
	handler = rateLimitLogin(handler, loginLimiter)
	handler = middleware.RequestIDMiddleware(handler)

	slog.Info("Starting ms-users service", "port", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}

func rateLimitLogin(next http.Handler, limiter func(http.Handler) http.Handler) http.Handler {
	limited := limiter(next)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost && request.URL.Path == "/auth/login" {
			limited.ServeHTTP(response, request)
			return
		}
		next.ServeHTTP(response, request)
	})
}
