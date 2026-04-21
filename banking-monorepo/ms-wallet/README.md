# ms-wallet

Digital wallet microservice. Stores user transactions and balances in MongoDB. Exposes a JSON HTTP API on port **3001** and a mutual-TLS gRPC interface on port **50051** for internal service communication.

---

## Requirements

| Tool | Version |
|------|---------|
| Go | 1.25+ |
| Docker + Docker Compose | any recent |
| MongoDB | 7+ (replica set required for transactions) |
| Redis | 7+ |

---

## Environment variables

Copy `.env.example` to `.env` and fill in the values.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PORT` | no | `3001` | HTTP server port |
| `MONGO_URI` | **yes** | — | MongoDB connection string. Must point to a replica set: `mongodb://host:27017/?replicaSet=rs0` |
| `MONGO_DB_NAME` | no | `wallet` | Database name |
| `MONGO_COLLECTION_PREFIX` | no | `` | Optional prefix for collection names |
| `JWT_SECRET` | **yes** | — | HMAC secret for verifying external JWTs. Use `ILIACHALLENGE` |
| `REDIS_URL` | **yes** | — | Redis address (`host:port`). Used for idempotency key storage |
| `GRPC_PORT` | no | `:50051` | gRPC listener address |
| `GRPC_CERT_FILE` | no | — | Path to wallet-service TLS certificate. gRPC server starts only when set |
| `GRPC_KEY_FILE` | no | — | Path to wallet-service private key |
| `GRPC_CA_FILE` | no | — | Path to the shared CA certificate (trust anchor) |

---

## Run with Docker Compose (recommended)

The root `docker-compose.yml` starts MongoDB (with replica set), Redis, and the wallet service together with the users service.

```bash
# From the repo root
docker compose up --build
```

To run only the wallet service stack (no users service):

```bash
# From the repo root
docker compose up --build wallet-db redis wallet-service
```

The service will be available at:

- HTTP: `http://localhost:3001`
- gRPC: `localhost:50051` (mTLS, only when cert env vars are set)

---

## Run locally (without Docker)

### 1. Start dependencies

```bash
# MongoDB replica set
docker run -d --name mongo-rs \
  -p 27017:27017 \
  mongo:latest \
  --replSet rs0 --bind_ip_all

# Initialise the replica set (run once)
docker exec mongo-rs mongosh --eval \
  "rs.initiate({_id:'rs0',members:[{_id:0,host:'localhost:27017'}]})"

# Redis
docker run -d --name redis -p 6379:6379 redis:7
```

### 2. Create `.env`

```bash
cp .env.example .env
```

```env
PORT=3001
MONGO_URI=mongodb://localhost:27017/?replicaSet=rs0
MONGO_DB_NAME=wallet
JWT_SECRET=ILIACHALLENGE
REDIS_URL=localhost:6379
```

### 3. Run the service

```bash
# From ms-wallet/
go run ./main.go
```

---

## HTTP API

All routes (except `/health`) require a JWT in the `Authorization: Bearer <token>` header, signed with `JWT_SECRET`.

### Health check

```
GET /health
```

Response: `200 OK` — `OK`

---

### Create transaction

```
POST /transactions
```

**Required headers:**

| Header | Description |
|--------|-------------|
| `Authorization` | `Bearer <jwt>` |
| `Idempotency-Key` | UUID v4. Guarantees at-most-once execution |

**Request body:**

```json
{
  "type": "CREDIT",
  "amount": "100.50",
  "description": "salary deposit"
}
```

| Field | Type | Values |
|-------|------|--------|
| `type` | string | `CREDIT` or `DEBIT` |
| `amount` | string (decimal) | Positive, max 2 decimal places |
| `description` | string | Optional |

**Responses:**

| Status | Meaning |
|--------|---------|
| `201 Created` | Transaction recorded |
| `200 OK` | Idempotent replay — same `Idempotency-Key` seen before, cached result returned |
| `400 Bad Request` | Missing `Idempotency-Key` header or invalid JSON |
| `409 Conflict` | Request with this key is currently in-flight — retry after backoff |
| `422 Unprocessable Entity` | Validation error (invalid amount, precision, type, or insufficient funds) |

**201 response body:**

```json
{
  "success": true,
  "data": {
    "id": "664f1a2b3c4d5e6f7a8b9c0d",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "type": "CREDIT",
    "amount": "100.50",
    "balance_before": "0",
    "balance_after": "100.50",
    "description": "salary deposit",
    "created_at": "2024-05-23T14:30:00Z"
  },
  "request_id": "req-uuid"
}
```

---

### Get balance

```
GET /wallet/balance
```

**Required headers:** `Authorization: Bearer <jwt>`

**Response:**

```json
{
  "success": true,
  "data": { "amount": "100.50" },
  "request_id": "req-uuid"
}
```

---

### List transactions

```
GET /transactions?type=CREDIT&limit=20&page=2
```

**Required headers:** `Authorization: Bearer <jwt>`

**Query parameters:**

| Param | Default | Description |
|-------|---------|-------------|
| `type` | — | Filter by `CREDIT` or `DEBIT` |
| `limit` | `20` | Page size |
| `page` | `1` | Page number (1-based) |
| `offset` | — | Raw offset (alternative to `page`) |

**Response:**

```json
{
  "success": true,
  "data": [ /* array of transaction objects */ ],
  "request_id": "req-uuid"
}
```

---

## Business rules

- **Balance floor:** debit that would make the balance negative is rejected with `ErrInsufficientFunds`.
- **Amount precision:** more than 2 decimal places is rejected with `ErrInvalidPrecision`.
- **Idempotency:** the `Idempotency-Key` header is mandatory on `POST /transactions`. Results are cached in Redis for 24 h; a duplicate request within that window returns the cached response without re-executing.
- **MongoDB transactions:** wallet balance update and transaction insert happen atomically inside a MongoDB session with majority write concern.

---

## gRPC internal interface

Used by `ms-users` to enrich login responses with wallet balance. Secured with mutual TLS — the server only accepts connections whose client certificate CN is `users-service`.

**Service:** `wallet.WalletService` (defined in `proto/wallet.proto`)

| Method | Request | Response |
|--------|---------|----------|
| `GetBalance` | `{ user_id }` | `{ amount: int64 }` |
| `ValidateUser` | `{ user_id }` | `{ is_valid: bool, email: string }` |

### Generating certificates

Certificates are pre-generated in `certs/`. To regenerate:

```bash
cd certs/
bash gen-certs.sh
```

> **Never** commit or ship `ca.key` inside a container image. Only `ca.crt`, `wallet-service.crt`, and `wallet-service.key` should be mounted into the wallet service container.

---

## Running tests

### Unit and middleware tests (no external dependencies)

```bash
# From ms-wallet/
go test ./internal/domain/... ./internal/middleware/... ./internal/usecase/... ./internal/grpc/... -v
```

The gRPC tests generate all TLS certificates in-process using `crypto/ecdsa` — no real cert files required.

### Repository integration tests (requires Docker)

Spins up a real MongoDB replica set via `testcontainers-go`:

```bash
go test ./internal/repository/... -v -timeout 120s
```

### All tests

```bash
go test ./... -timeout 120s
```

### Benchmarks — idempotency middleware

```bash
go test ./internal/middleware/... -bench=. -benchmem -run='^$'
```

Expected output (approximate, on modern hardware):

```
BenchmarkIdempotency_FirstCall-N   ~2000 ns/op   ~6 KB/op   26 allocs/op
BenchmarkIdempotency_Replay-N      ~2200 ns/op   ~7 KB/op   28 allocs/op
```

---

## Project layout

```
ms-wallet/
├── main.go                          # Wires everything, starts HTTP + gRPC servers
├── internal/
│   ├── domain/
│   │   ├── transaction.go           # Transaction and Wallet models; NewTransaction validates business rules
│   │   ├── ports.go                 # WalletRepository interface + sentinel errors
│   │   └── transaction_test.go
│   ├── repository/
│   │   └── mongo_wallet_repository.go  # MongoDB implementation (atomic sessions)
│   ├── usecase/
│   │   ├── transaction_usecase.go   # CreateTransaction, Credit, Debit, GetBalance, ListTransactions
│   │   └── transaction_usecase_test.go
│   ├── middleware/
│   │   ├── idempotency.go           # HTTP middleware: two-phase Redis lock
│   │   ├── idempotency_test.go
│   │   └── redis_store.go           # RedisIdempotencyStore adapter
│   ├── grpc/
│   │   ├── server.go                # WalletServiceServer, CNInterceptor, LoadTLSCredentials
│   │   └── server_test.go           # bufconn + in-process PKI tests
│   └── transport/http/
│       ├── transaction_handler.go   # POST /transactions, GET /transactions, GET /wallet/balance
│       └── helpers.go
```
