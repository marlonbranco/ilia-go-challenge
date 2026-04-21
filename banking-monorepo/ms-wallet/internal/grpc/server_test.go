package grpc_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"

	"ms-wallet/internal/domain"
	walletgrpc "ms-wallet/internal/grpc"

	walletpb "proto/wallet"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1 << 20

type testPKI struct {
	caPool      *x509.CertPool
	serverCert  tls.Certificate
	validClient tls.Certificate
	rogueClient tls.Certificate
	wrongCN     tls.Certificate
}

func generatePKI(test *testing.T) *testPKI {
	test.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		test.Fatalf("ca key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		test.Fatalf("create ca cert: %v", err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	var serial int64 = 2
	sign := func(cn string, usages []x509.ExtKeyUsage) tls.Certificate {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: cn},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			ExtKeyUsage:  usages,
			DNSNames:     []string{cn},
		}
		serial++
		certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
		keyDER, _ := x509.MarshalECPrivateKey(key)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
		c, _ := tls.X509KeyPair(certPEM, keyPEM)
		return c
	}

	serverCert := sign("wallet-service", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth})
	validClient := sign("users-service", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth})
	wrongCN := sign("attacker-service", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth})

	rogueKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rogueTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(999),
		Subject:      pkix.Name{CommonName: "evil-service"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	rogueDER, _ := x509.CreateCertificate(rand.Reader, rogueTmpl, rogueTmpl, &rogueKey.PublicKey, rogueKey)
	roguePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rogueDER})
	rogueKeyDER, _ := x509.MarshalECPrivateKey(rogueKey)
	rogueKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: rogueKeyDER})
	rogueClient, _ := tls.X509KeyPair(roguePEM, rogueKeyPEM)

	return &testPKI{
		caPool:      pool,
		serverCert:  serverCert,
		validClient: validClient,
		rogueClient: rogueClient,
		wrongCN:     wrongCN,
	}
}

func launchServer(test *testing.T, pki *testPKI, repo domain.WalletRepository) *bufconn.Listener {
	test.Helper()
	listener := bufconn.Listen(bufSize)
	serverTLS := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pki.serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pki.caPool,
		MinVersion:   tls.VersionTLS13,
	})
	srv := walletgrpc.NewServer(serverTLS)
	walletpb.RegisterWalletServiceServer(srv, walletgrpc.NewWalletServer(repo))
	test.Cleanup(srv.Stop)
	go srv.Serve(listener)
	return listener
}

func dialBufconn(test *testing.T, listener *bufconn.Listener, clientCert tls.Certificate, rootCAs *x509.CertPool) walletpb.WalletServiceClient {
	test.Helper()
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      rootCAs,
		ServerName:   "wallet-service",
		MinVersion:   tls.VersionTLS13,
	})
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(creds),
	)
	if err != nil {
		test.Fatalf("grpc.NewClient: %v", err)
	}
	test.Cleanup(func() { conn.Close() })
	return walletpb.NewWalletServiceClient(conn)
}

type mockRepo struct {
	balances map[string]decimal.Decimal
}

func (m *mockRepo) GetOrCreateWallet(_ context.Context, userID string) (domain.Wallet, error) {
	bal := m.balances[userID]
	return domain.Wallet{UserID: userID, Balance: bal}, nil
}

func (m *mockRepo) CreateTransaction(_ context.Context, tx domain.Transaction) (domain.Transaction, error) {
	return tx, nil
}

func (m *mockRepo) GetBalance(_ context.Context, userID string) (decimal.Decimal, error) {
	if bal, ok := m.balances[userID]; ok {
		return bal, nil
	}
	return decimal.Zero, nil
}

func (m *mockRepo) ListTransactions(_ context.Context, _ string, _ domain.ListFilter) ([]domain.Transaction, error) {
	return nil, nil
}

func (m *mockRepo) FindByIdempotencyKey(_ context.Context, _ string) (domain.Transaction, error) {
	return domain.Transaction{}, domain.ErrNotFound
}

func TestGetBalanceValidClient(test *testing.T) {
	pki := generatePKI(test)
	repo := &mockRepo{balances: map[string]decimal.Decimal{
		"user-1": decimal.NewFromInt(250),
	}}
	listener := launchServer(test, pki, repo)
	client := dialBufconn(test, listener, pki.validClient, pki.caPool)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.GetBalance(ctx, &walletpb.GetBalanceRequest{UserId: "user-1"})
	if err != nil {
		test.Fatalf("GetBalance: %v", err)
	}
	if resp.Amount != "250" {
		test.Errorf("expected \"250\", got %q", resp.Amount)
	}
}

func TestGetBalanceUnknownUserReturnsZero(test *testing.T) {
	pki := generatePKI(test)
	repo := &mockRepo{balances: map[string]decimal.Decimal{}}
	listener := launchServer(test, pki, repo)
	client := dialBufconn(test, listener, pki.validClient, pki.caPool)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.GetBalance(ctx, &walletpb.GetBalanceRequest{UserId: "ghost"})
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}
	if resp.Amount != "0" {
		test.Errorf("expected \"0\", got %q", resp.Amount)
	}
}

func TestValidateUserExistingUser(test *testing.T) {
	pki := generatePKI(test)
	repo := &mockRepo{balances: map[string]decimal.Decimal{"user-x": decimal.NewFromInt(10)}}
	listener := launchServer(test, pki, repo)
	client := dialBufconn(test, listener, pki.validClient, pki.caPool)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.ValidateUser(ctx, &walletpb.ValidateUserRequest{UserId: "user-x"})
	if err != nil {
		test.Fatalf("ValidateUser: %v", err)
	}
	if !resp.IsValid {
		test.Error("expected is_valid=true")
	}
}

func TestValidateUserNewUserReturnsValid(test *testing.T) {
	pki := generatePKI(test)
	repo := &mockRepo{balances: map[string]decimal.Decimal{}}
	listener := launchServer(test, pki, repo)
	client := dialBufconn(test, listener, pki.validClient, pki.caPool)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.ValidateUser(ctx, &walletpb.ValidateUserRequest{UserId: "brand-new"})
	if err != nil {
		test.Fatalf("ValidateUser: %v", err)
	}
	if !resp.IsValid {
		test.Error("expected is_valid=true for new user (wallet created on demand)")
	}
}

func TestRogueClientTLSRejected(test *testing.T) {
	pki := generatePKI(test)
	repo := &mockRepo{balances: map[string]decimal.Decimal{}}
	listener := launchServer(test, pki, repo)

	client := dialBufconn(test, listener, pki.rogueClient, pki.caPool)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.GetBalance(ctx, &walletpb.GetBalanceRequest{UserId: "user-1"})
	if err == nil {
		test.Fatal("expected TLS rejection for self-signed client cert, got nil")
	}

}

func TestWrongCNPermissionDenied(test *testing.T) {
	pki := generatePKI(test)
	repo := &mockRepo{balances: map[string]decimal.Decimal{}}
	listener := launchServer(test, pki, repo)

	client := dialBufconn(test, listener, pki.wrongCN, pki.caPool)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.GetBalance(ctx, &walletpb.GetBalanceRequest{UserId: "user-1"})
	if err == nil {
		test.Fatal("expected PermissionDenied, got nil")
	}
	currentStatus, ok := status.FromError(err)
	if !ok {
		test.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if currentStatus.Code() != codes.PermissionDenied {
		test.Errorf("expected PermissionDenied, got %s", currentStatus.Code())
	}
}

type errRepo struct{ mockRepo }

func (_ *errRepo) GetBalance(_ context.Context, _ string) (decimal.Decimal, error) {
	return decimal.Zero, errors.New("db unavailable")
}

func TestGetBalanceRepoErrorReturnsInternal(test *testing.T) {
	pki := generatePKI(test)
	repo := &errRepo{}
	listener := launchServer(test, pki, repo)
	client := dialBufconn(test, listener, pki.validClient, pki.caPool)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.GetBalance(ctx, &walletpb.GetBalanceRequest{UserId: "user-1"})
	if err == nil {
		test.Fatal("expected error, got nil")
	}
	currentStatus, _ := status.FromError(err)
	if currentStatus.Code() != codes.Internal {
		test.Errorf("expected Internal, got %s", currentStatus.Code())
	}
}
