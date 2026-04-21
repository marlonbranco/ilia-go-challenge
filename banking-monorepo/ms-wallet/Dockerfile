# syntax=docker/dockerfile:1
FROM golang:1.25 AS builder

WORKDIR /app
COPY go.work go.work.sum ./
COPY ms-wallet/go.mod ms-wallet/go.sum* ./ms-wallet/
COPY ms-users/go.mod ms-users/go.sum* ./ms-users/
COPY pkg/go.mod pkg/go.sum* ./pkg/
COPY proto/go.mod proto/go.sum* ./proto/

RUN go work sync

COPY ms-wallet/ ./ms-wallet/
COPY pkg/ ./pkg/
COPY proto/ ./proto/

WORKDIR /app/ms-wallet
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/wallet-bin .

FROM alpine:3
COPY --from=builder /app/wallet-bin /wallet-bin
EXPOSE 3001 50051
CMD ["/wallet-bin"]
