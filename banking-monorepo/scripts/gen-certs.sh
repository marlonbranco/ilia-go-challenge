#!/usr/bin/env bash
set -euo pipefail

CERTS_DIR="$(cd "$(dirname "$0")/.." && pwd)/certs"
DAYS=825

mkdir -p "$CERTS_DIR"

echo "Generating CA..."
openssl genrsa -out "$CERTS_DIR/ca.key" 4096
openssl req -new -x509 -key "$CERTS_DIR/ca.key" \
  -out "$CERTS_DIR/ca.crt" \
  -days "$DAYS" \
  -subj "/CN=local-dev-ca"

sign() {
  local name="$1"
  local cn="$2"

  openssl genrsa -out "$CERTS_DIR/${name}.key" 2048

  openssl req -new \
    -key "$CERTS_DIR/${name}.key" \
    -out "$CERTS_DIR/${name}.csr" \
    -subj "/CN=${cn}"

  openssl x509 -req \
    -in "$CERTS_DIR/${name}.csr" \
    -CA "$CERTS_DIR/ca.crt" \
    -CAkey "$CERTS_DIR/ca.key" \
    -CAcreateserial \
    -out "$CERTS_DIR/${name}.crt" \
    -days "$DAYS" \
    -extfile <(printf "subjectAltName=DNS:%s\nextendedKeyUsage=serverAuth,clientAuth" "$cn")

  rm "$CERTS_DIR/${name}.csr"
}

echo "Generating wallet-service cert (CN=wallet-service)..."
sign "wallet-service" "wallet-service"

echo "Generating users-service cert (CN=users-service)..."
sign "users-service" "users-service"

rm -f "$CERTS_DIR/ca.srl"

echo "Done. Certificates written to $CERTS_DIR/"
echo "  ca.crt            — CA certificate (shared)"
echo "  wallet-service.crt / .key — wallet-service server + client identity"
echo "  users-service.crt / .key  — users-service client identity"
