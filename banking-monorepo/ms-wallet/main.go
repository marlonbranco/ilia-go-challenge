package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"ms-wallet/internal/repository"
	transportHttp "ms-wallet/internal/transport/http"
	"ms-wallet/internal/usecase"

	"pkg/middleware"

	"github.com/joho/godotenv"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found, using system environment variables")
	}

	ctx := context.Background()

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		slog.Error("MONGO_URI environment variable is required")
		os.Exit(1)
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		slog.Error("JWT_SECRET environment variable is required")
		os.Exit(1)
	}

	walletRepo, err := repository.NewMongoWalletRepository(ctx, mongoURI)
	if err != nil {
		slog.Error("failed to initialise wallet repository", "error", err)
		os.Exit(1)
	}
	defer walletRepo.Close(ctx)

	txUseCase := usecase.NewTransactionUseCase(walletRepo)
	txHandler := transportHttp.NewTransactionHandler(txUseCase)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	jwtMiddle := middleware.JWTMiddleware("JWT_SECRET")
	txHandler.RegisterRoutes(mux, jwtMiddle)

	var handler http.Handler = mux
	handler = middleware.RequestIDMiddleware(handler)

	slog.Info("starting ms-wallet service", "port", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
