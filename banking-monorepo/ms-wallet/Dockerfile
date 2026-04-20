# syntax=docker/dockerfile:1
FROM golang:1.22 AS builder

WORKDIR /app
COPY go.work go.work.sum ./
COPY wallet-service/go.mod wallet-service/go.sum* ./wallet-service/
COPY users-service/go.mod users-service/go.sum* ./users-service/
COPY pkg/go.mod pkg/go.sum* ./pkg/
COPY proto/go.mod proto/go.sum* ./proto/

RUN go work sync

COPY wallet-service/ ./wallet-service/
COPY pkg/ ./pkg/
COPY proto/ ./proto/

WORKDIR /app/wallet-service
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/wallet-bin .

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/wallet-bin /wallet-bin
EXPOSE 3001
CMD ["/wallet-bin"]