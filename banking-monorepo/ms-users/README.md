# ms-users

REST microservice for user management and authentication. Built with Go, PostgreSQL, and Redis.

## Requirements

- Go 1.22+
- Docker (for running dependencies or integration tests)

---

## Running the application

### 1. Start dependencies

From the monorepo root, start PostgreSQL and Redis:

```bash
# PostgreSQL only
docker compose -f docker-compose.yml up -d

# Redis + full stack
docker compose -f docker-compose-2.yml up -d users-db redis
```

### 2. Configure environment

Copy the example file and fill in the values:

```bash
cp .env.example .env
```

| Variable       | Description                                  | Example                                                       |
|----------------|----------------------------------------------|---------------------------------------------------------------|
| `PORT`         | HTTP port (defaults to `3002`)               | `3002`                                                        |
| `POSTGRES_URL` | PostgreSQL connection string                 | `postgres://user:password@localhost:5432/usersdb?sslmode=disable` |
| `REDIS_URL`    | Redis address (`host:port`)                  | `localhost:6379`                                              |
| `JWT_SECRET`   | Secret key used to sign JWT access tokens    | `some-strong-secret`                                          |

### 3. Run

```bash
go run .
```

The service will apply database migrations automatically on startup and listen on the configured port.

---

## Running the tests

### Unit and usecase tests (no external dependencies)

```bash
go test ./internal/domain/... ./internal/usecase/... ./internal/transport/... -v
```

### Integration tests (require Docker)

The repository tests spin up a real PostgreSQL container via [testcontainers-go](https://golang.testcontainers.org/). Docker must be running.

```bash
go test ./internal/repository/... -v
```

### All tests

```bash
go test ./... -v
```

### Skip integration tests (fast mode)

```bash
go test ./... -v -short
```

> Integration tests check for `-short` and are skipped automatically when the flag is set — unit/usecase tests always run.

---

## API endpoints

| Method | Path             | Auth required | Description              |
|--------|------------------|---------------|--------------------------|
| POST   | `/auth/register` | No            | Register a new user      |
| POST   | `/auth/login`    | No            | Login, returns token pair |
| POST   | `/auth/refresh`  | No            | Rotate access/refresh tokens |
| POST   | `/auth/logout`   | JWT           | Invalidate refresh token |
| GET    | `/users`         | JWT           | List all users           |
| GET    | `/users/{id}`    | JWT           | Get user by ID           |
| PATCH  | `/users/{id}`    | JWT           | Update user (own only)   |
| DELETE | `/users/{id}`    | JWT           | Delete user (own only)   |
| GET    | `/health`        | No            | Health check             |
