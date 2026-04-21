# Banking Monorepo

Two Go microservices communicating over mTLS gRPC.

| Service | Port | Docs |
|---------|------|------|
| ms-users | 3002 | http://localhost:3002/docs |
| ms-wallet | 3001 | http://localhost:3001/docs |

---

## Prerequisites

- Docker + Docker Compose
- Go 1.22+
- OpenSSL (for certificate generation)
- `protoc` + plugins (only needed if you change `proto/wallet.proto`)

---

## First-time setup

### 1. Create your environment file

```bash
cp .env.example .env
```

Edit `.env` and set a strong `JWT_SECRET`. The other defaults work for local development.

### 2. Generate TLS certificates

mTLS is required for the gRPC channel between ms-users and ms-wallet. Certificates are **not** committed — generate them once locally:

```bash
make certs
```

This creates `certs/` with:
- `ca.crt` — local CA (shared trust anchor)
- `wallet-service.crt` / `.key` — wallet-service server + client identity
- `users-service.crt` / `.key` — users-service client identity

### 3. Start all services

```bash
make up
```

This builds the Docker images and starts:
- `users-db` (PostgreSQL)
- `redis-users` (Redis for ms-users token store + rate limiting)
- `wallet-db` (MongoDB replica set)
- `redis-wallet` (Redis for ms-wallet idempotency store)
- `wallet-service` (ms-wallet HTTP :3001 + gRPC :50051)
- `users-service` (ms-users HTTP :3002)

Database migrations run automatically on startup inside ms-users.

---

## Day-to-day commands

```bash
make up              # Build images and start all containers (generates certs if missing)
make down            # Stop all containers (preserves volumes)
make clean           # Stop containers, remove volumes and generated certs
```

---

## Development

```bash
make test            # Unit tests for both services (skips integration tests)
make test-integration  # Full test suite including integration tests (requires Docker)
make lint            # go vet on both services
```

---

## Updating the proto contract

If you modify `proto/wallet.proto`, regenerate the Go bindings:

```bash
make proto
```

Requires `protoc-gen-go` and `protoc-gen-go-grpc` in `$GOPATH/bin`:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

---

## API documentation

Swagger UI is served by each service at runtime — no separate tool needed.

| Service | URL |
|---------|-----|
| ms-users | http://localhost:3002/docs |
| ms-wallet | http://localhost:3001/docs |

Raw OpenAPI specs:
- `ms-users/internal/docs/openapi.yaml`
- `ms-wallet/internal/docs/openapi.yaml`

---

## Architecture overview

```
ms-users  (:3002)  ──── JWT auth, user CRUD
     │
     │  mTLS gRPC (:50051)
     ▼
ms-wallet (:3001)  ──── wallet balance, transactions
```

- **ms-users** authenticates users and, on login, enriches the response with the wallet balance by calling ms-wallet over gRPC.
- **ms-wallet** enforces idempotency on transaction creation via a Redis-backed store.
- The gRPC channel uses mutual TLS with CN validation — only a client certificate with `CN=users-service` is accepted.
