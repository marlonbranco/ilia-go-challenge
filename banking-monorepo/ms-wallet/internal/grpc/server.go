package grpc

import (
	"context"

	"ms-wallet/internal/domain"
	walletpb "proto/wallet"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const allowedClientCN = "users-service"

type WalletServer struct {
	walletpb.UnimplementedWalletServiceServer
	repo domain.WalletRepository
}

func NewWalletServer(repo domain.WalletRepository) *WalletServer {
	return &WalletServer{repo: repo}
}

func (walletServer *WalletServer) GetBalance(ctx context.Context, request *walletpb.GetBalanceRequest) (*walletpb.BalanceResponse, error) {
	balance, err := walletServer.repo.GetBalance(ctx, request.UserId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get balance: %v", err)
	}
	return &walletpb.BalanceResponse{Amount: balance.String()}, nil
}

func (walletServer *WalletServer) ValidateUser(ctx context.Context, request *walletpb.ValidateUserRequest) (*walletpb.ValidateUserResponse, error) {
	_, err := walletServer.repo.GetOrCreateWallet(ctx, request.UserId)
	if err != nil {
		return &walletpb.ValidateUserResponse{IsValid: false}, nil
	}
	return &walletpb.ValidateUserResponse{IsValid: true}, nil
}

func CNInterceptor(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	clientPeer, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no peer info in context")
	}
	tlsInfo, ok := clientPeer.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "connection is not TLS")
	}
	if len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return nil, status.Error(codes.Unauthenticated, "no verified certificate chain")
	}
	commonName := tlsInfo.State.VerifiedChains[0][0].Subject.CommonName
	if commonName != allowedClientCN {
		return nil, status.Errorf(codes.PermissionDenied, "client CN %q is not permitted", commonName)
	}
	return handler(ctx, request)
}

func NewServer(creds credentials.TransportCredentials) *grpc.Server {
	return grpc.NewServer(
		grpc.Creds(creds),
		grpc.UnaryInterceptor(CNInterceptor),
	)
}
