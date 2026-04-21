package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"google.golang.org/grpc/credentials"
)

func loadClientTLS(certFile, keyFile, caFile string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert/key: %w", err)
	}
	caBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caBytes) {
		return nil, errors.New("failed to parse CA certificate")
	}
	warnCertExpiry(cert, certFile)
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}), nil
}

func warnCertExpiry(cert tls.Certificate, path string) {
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return
	}
	remaining := time.Until(leaf.NotAfter)
	if remaining < 30*24*time.Hour {
		slog.Warn("TLS certificate expires soon",
			"path", path,
			"expires_at", leaf.NotAfter.Format(time.RFC3339),
			"remaining_hours", int(remaining.Hours()))
	}
}
