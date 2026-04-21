package grpc_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ms-users/internal/domain"
	usersgrpc "ms-users/internal/grpc"
	jwtinfra "ms-users/internal/infra/jwt"
	transporthttp "ms-users/internal/transport/http"
	"ms-users/internal/usecase"

	walletpb "proto/wallet"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1 << 20

type testPKI struct {
	caPool     *x509.CertPool
	serverCert tls.Certificate
	clientCert tls.Certificate
}

func generatePKI(t *testing.T) *testPKI {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true,
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	caCert, _ := x509.ParseCertificate(caDER)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	var serial int64 = 2
	sign := func(cn string) tls.Certificate {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
			DNSNames:    []string{cn},
		}
		serial++
		certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
		keyDER, _ := x509.MarshalECPrivateKey(key)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
		c, _ := tls.X509KeyPair(certPEM, keyPEM)
		return c
	}
	return &testPKI{caPool: pool, serverCert: sign("wallet-service"), clientCert: sign("users-service")}
}

type mockWalletGRPC struct {
	walletpb.UnimplementedWalletServiceServer
	mu        sync.Mutex
	balance   int64
	callCount int
	failTimes int
	errCode   codes.Code
}

func (m *mockWalletGRPC) GetBalance(_ context.Context, _ *walletpb.GetBalanceRequest) (*walletpb.BalanceResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.callCount <= m.failTimes {
		return nil, status.Error(m.errCode, "temporary error")
	}
	return &walletpb.BalanceResponse{Amount: fmt.Sprintf("%d", m.balance)}, nil
}

func (m *mockWalletGRPC) ValidateUser(_ context.Context, _ *walletpb.ValidateUserRequest) (*walletpb.ValidateUserResponse, error) {
	return &walletpb.ValidateUserResponse{IsValid: true}, nil
}

func launchMockWalletServer(t *testing.T, pki *testPKI, mock *mockWalletGRPC) *bufconn.Listener {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pki.serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pki.caPool,
		MinVersion:   tls.VersionTLS13,
	})
	srv := grpc.NewServer(grpc.Creds(creds))
	walletpb.RegisterWalletServiceServer(srv, mock)
	t.Cleanup(srv.Stop)
	go srv.Serve(lis)
	return lis
}

func newTestClient(t *testing.T, pki *testPKI, lis *bufconn.Listener) domain.WalletClient {
	t.Helper()
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pki.clientCert},
		RootCAs:      pki.caPool,
		ServerName:   "wallet-service",
		MinVersion:   tls.VersionTLS13,
	})
	c, err := usersgrpc.NewWalletClientFromDialer(
		func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) },
		creds,
	)
	if err != nil {
		t.Fatalf("NewWalletClientFromDialer: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestWalletClientGetBalance(t *testing.T) {
	pki := generatePKI(t)
	mock := &mockWalletGRPC{balance: 750}
	lis := launchMockWalletServer(t, pki, mock)
	client := newTestClient(t, pki, lis)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	bal, err := client.GetBalance(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if bal != 750 {
		t.Errorf("want 750, got %d", bal)
	}
}

func TestWalletClientValidateUser(t *testing.T) {
	pki := generatePKI(t)
	mock := &mockWalletGRPC{}
	lis := launchMockWalletServer(t, pki, mock)
	client := newTestClient(t, pki, lis)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	valid, err := client.ValidateUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("ValidateUser: %v", err)
	}
	if !valid {
		t.Error("expected is_valid=true")
	}
}

func TestWalletClientRetryOnUnavailable(t *testing.T) {
	pki := generatePKI(t)

	mock := &mockWalletGRPC{balance: 100, failTimes: 1, errCode: codes.Unavailable}
	lis := launchMockWalletServer(t, pki, mock)
	client := newTestClient(t, pki, lis)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bal, err := client.GetBalance(ctx, "user-retry")
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if bal != 100 {
		t.Errorf("want 100, got %d", bal)
	}
	mock.mu.Lock()
	calls := mock.callCount
	mock.mu.Unlock()
	if calls != 2 {
		t.Errorf("expected 2 calls (1 fail + 1 success), got %d", calls)
	}
}

func TestWalletClientNoRetryOnPermissionDenied(t *testing.T) {
	pki := generatePKI(t)

	mock := &mockWalletGRPC{failTimes: 3, errCode: codes.PermissionDenied}
	lis := launchMockWalletServer(t, pki, mock)
	client := newTestClient(t, pki, lis)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.GetBalance(ctx, "user-x")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	mock.mu.Lock()
	calls := mock.callCount
	mock.mu.Unlock()
	if calls != 1 {
		t.Errorf("expected exactly 1 call (no retry on PermissionDenied), got %d", calls)
	}
}

type fakeUserRepo struct {
	mu    sync.Mutex
	users map[string]domain.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{users: make(map[string]domain.User)}
}

func (r *fakeUserRepo) Create(_ context.Context, user domain.User) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.users[user.Email]; exists {
		return domain.User{}, domain.ErrDuplicate
	}
	r.users[user.Email] = user
	return user, nil
}
func (r *fakeUserRepo) FindByEmail(_ context.Context, email string) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[email]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}
func (r *fakeUserRepo) FindAll(_ context.Context) ([]domain.User, error) { return nil, nil }
func (r *fakeUserRepo) FindByID(_ context.Context, _ uuid.UUID) (domain.User, error) {
	return domain.User{}, errors.New("not implemented")
}
func (r *fakeUserRepo) Update(_ context.Context, _ domain.User) error { return nil }
func (r *fakeUserRepo) Delete(_ context.Context, _ uuid.UUID) error   { return nil }

type fakeTokenStore struct {
	mu     sync.Mutex
	tokens map[string]bool
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{tokens: make(map[string]bool)}
}

func (s *fakeTokenStore) Store(_ context.Context, userID, tokenID string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[userID+":"+tokenID] = true
	return nil
}
func (s *fakeTokenStore) Exists(_ context.Context, userID, tokenID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens[userID+":"+tokenID], nil
}
func (s *fakeTokenStore) Delete(_ context.Context, userID, tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, userID+":"+tokenID)
	return nil
}

type walletClientOK struct{ balance int64 }

func (w *walletClientOK) GetBalance(_ context.Context, _ string) (int64, error) {
	return w.balance, nil
}
func (w *walletClientOK) ValidateUser(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (w *walletClientOK) Close() error { return nil }

type walletClientFail struct{}

func (w *walletClientFail) GetBalance(_ context.Context, _ string) (int64, error) {
	return 0, status.Error(codes.Unavailable, "wallet service unavailable")
}
func (w *walletClientFail) ValidateUser(_ context.Context, _ string) (bool, error) {
	return false, status.Error(codes.Unavailable, "wallet service unavailable")
}
func (w *walletClientFail) Close() error { return nil }

func buildAuthHandler(walletClient domain.WalletClient) (http.Handler, error) {
	userRepo := newFakeUserRepo()
	tokenStore := newFakeTokenStore()
	tokenSvc := jwtinfra.NewTokenService("test-secret", jwtinfra.DefaultAccessTTL, jwtinfra.DefaultRefreshTTL)
	authUC := usecase.NewAuthUseCase(userRepo, tokenStore, tokenSvc).WithWalletClient(walletClient)

	ctx := context.Background()
	if _, err := authUC.Register(ctx, usecase.RegisterRequest{FirstName: "Test", LastName: "User", Email: "test@example.com", Password: "password123"}); err != nil {
		return nil, err
	}

	handler := transporthttp.NewAuthHandler(authUC)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })
	return mux, nil
}

func TestLoginEnrichedWithBalance(t *testing.T) {
	mux, err := buildAuthHandler(&walletClientOK{balance: 500})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	body := strings.NewReader(`{"email":"test@example.com","password":"password123"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			AccessToken string `json:"access_token"`
			Balance     *int64 `json:"balance"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Balance == nil {
		t.Fatal("expected non-nil balance in login response")
	}
	if *resp.Data.Balance != 500 {
		t.Errorf("want balance=500, got %d", *resp.Data.Balance)
	}
}

func TestLoginWalletUnavailableStillSucceeds(t *testing.T) {
	mux, err := buildAuthHandler(&walletClientFail{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	body := strings.NewReader(`{"email":"test@example.com","password":"password123"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login must succeed even when wallet down; got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			AccessToken string `json:"access_token"`
			Balance     *int64 `json:"balance"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.Data.Balance != nil {
		t.Errorf("expected balance=null when wallet down, got %d", *resp.Data.Balance)
	}
}
