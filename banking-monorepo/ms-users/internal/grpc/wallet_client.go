package grpc

import (
	"context"
	"fmt"
	"net"
	"time"

	"ms-users/internal/domain"
	walletpb "proto/wallet"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

var retryDelays = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}

type grpcWalletClient struct {
	conn   *grpc.ClientConn
	client walletpb.WalletServiceClient
}

func NewWalletClient(certFile, keyFile, caFile, addr string) (domain.WalletClient, error) {
	creds, err := loadClientTLS(certFile, keyFile, caFile)
	if err != nil {
		return nil, fmt.Errorf("wallet client TLS: %w", err)
	}
	return dial(addr, creds)
}

func NewWalletClientFromDialer(dialer func(ctx context.Context, addr string) (net.Conn, error), creds credentials.TransportCredentials) (domain.WalletClient, error) {
	return dial("passthrough:///bufnet", creds, grpc.WithContextDialer(dialer))
}

func dial(target string, creds credentials.TransportCredentials, extra ...grpc.DialOption) (domain.WalletClient, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
	}
	opts = append(opts, extra...)

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpc.NewClient %s: %w", target, err)
	}
	return &grpcWalletClient{conn: conn, client: walletpb.NewWalletServiceClient(conn)}, nil
}

func (c *grpcWalletClient) GetBalance(ctx context.Context, userID string) (int64, error) {
	for i := 0; ; i++ {
		resp, err := c.client.GetBalance(ctx, &walletpb.GetBalanceRequest{UserId: userID})
		if err == nil {
			d, err := decimal.NewFromString(resp.Amount)
			if err != nil {
				return 0, fmt.Errorf("invalid balance amount %q: %w", resp.Amount, err)
			}
			return d.IntPart(), nil
		}
		if !isRetryable(err) || i >= len(retryDelays) {
			return 0, err
		}
		select {
		case <-time.After(retryDelays[i]):
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

func (c *grpcWalletClient) ValidateUser(ctx context.Context, userID string) (bool, error) {
	for i := 0; ; i++ {
		resp, err := c.client.ValidateUser(ctx, &walletpb.ValidateUserRequest{UserId: userID})
		if err == nil {
			return resp.IsValid, nil
		}
		if !isRetryable(err) || i >= len(retryDelays) {
			return false, err
		}
		select {
		case <-time.After(retryDelays[i]):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

func (c *grpcWalletClient) Close() error {
	return c.conn.Close()
}

func isRetryable(err error) bool {
	s, ok := status.FromError(err)
	if !ok {
		return false
	}
	return s.Code() == codes.Unavailable || s.Code() == codes.DeadlineExceeded
}
