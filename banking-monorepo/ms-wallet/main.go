package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"ms-wallet/internal/docs"
	walletgrpc "ms-wallet/internal/grpc"
	walletMiddleware "ms-wallet/internal/middleware"
	"ms-wallet/internal/repository"
	transportHttp "ms-wallet/internal/transport/http"
	"ms-wallet/internal/usecase"

	"pkg/middleware"
	walletpb "proto/wallet"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found, using system environment variables")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisURL})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		slog.Error("redis ping failed", "error", err, "addr", redisURL)
		os.Exit(1)
	}
	defer redisClient.Close()

	dbName := os.Getenv("MONGO_DB_NAME")
	if dbName == "" {
		dbName = "wallet"
	}
	collectionPrefix := os.Getenv("MONGO_COLLECTION_PREFIX")
	walletRepo, err := repository.NewMongoWalletRepository(ctx, mongoURI, dbName, collectionPrefix)
	if err != nil {
		slog.Error("failed to initialise wallet repository", "error", err)
		os.Exit(1)
	}
	defer walletRepo.Close(ctx)

	if certFile := os.Getenv("GRPC_CERT_FILE"); certFile != "" {
		creds, err := walletgrpc.LoadTLSCredentials(
			certFile,
			os.Getenv("GRPC_KEY_FILE"),
			os.Getenv("GRPC_CA_FILE"),
		)
		if err != nil {
			slog.Error("failed to load gRPC TLS credentials", "error", err)
			os.Exit(1)
		}
		grpcAddr := os.Getenv("GRPC_PORT")
		if grpcAddr == "" {
			grpcAddr = ":50051"
		}
		lis, err := net.Listen("tcp", grpcAddr)
		if err != nil {
			slog.Error("failed to open gRPC listener", "error", fmt.Sprintf("gRPC listen %s: %v", grpcAddr, err))
			os.Exit(1)
		}
		grpcSrv := walletgrpc.NewServer(creds)
		walletpb.RegisterWalletServiceServer(grpcSrv, walletgrpc.NewWalletServer(walletRepo))
		go func() {
			slog.Info("starting gRPC server", "addr", lis.Addr())
			if err := grpcSrv.Serve(lis); err != nil {
				slog.Error("gRPC server error", "error", err)
			}
		}()
		defer grpcSrv.GracefulStop()
	} else {
		slog.Warn("GRPC_CERT_FILE not set — gRPC server disabled")
	}

	txUseCase := usecase.NewTransactionUseCase(walletRepo)
	txHandler := transportHttp.NewTransactionHandler(txUseCase)

	idemStore := walletMiddleware.NewRedisIdempotencyStore(redisClient)
	idemMiddleware := walletMiddleware.Idempotency(idemStore)
	jwtMiddle := middleware.JWTMiddleware("JWT_SECRET")

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	txHandler.RegisterRoutes(mux, jwtMiddle, idemMiddleware)
	docs.RegisterRoutes(mux)

	var handler http.Handler = mux
	handler = middleware.RequestIDMiddleware(handler)

	slog.Info("starting ms-wallet HTTP service", "port", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
