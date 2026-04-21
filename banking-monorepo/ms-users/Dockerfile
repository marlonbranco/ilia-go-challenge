# syntax=docker/dockerfile:1
FROM golang:1.25 AS builder

WORKDIR /app
COPY go.work go.work.sum ./
COPY ms-wallet/go.mod ms-wallet/go.sum* ./ms-wallet/
COPY ms-users/go.mod ms-users/go.sum* ./ms-users/
COPY pkg/go.mod pkg/go.sum* ./pkg/
COPY proto/go.mod proto/go.sum* ./proto/

RUN go work sync

COPY ms-users/ ./ms-users/
COPY pkg/ ./pkg/
COPY proto/ ./proto/

WORKDIR /app/ms-users
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/users-bin .

FROM alpine:3
WORKDIR /app
COPY --from=builder /app/users-bin ./users-bin
COPY --from=builder /app/ms-users/internal/repository/migrations ./internal/repository/migrations
EXPOSE 3002
CMD ["./users-bin"]
