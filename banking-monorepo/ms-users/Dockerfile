# syntax=docker/dockerfile:1
FROM golang:1.22 AS builder

WORKDIR /app
COPY go.work go.work.sum ./
COPY wallet-service/go.mod wallet-service/go.sum* ./wallet-service/
COPY users-service/go.mod users-service/go.sum* ./users-service/
COPY pkg/go.mod pkg/go.sum* ./pkg/
COPY proto/go.mod proto/go.sum* ./proto/

RUN go work sync

COPY users-service/ ./users-service/
COPY pkg/ ./pkg/
COPY proto/ ./proto/

WORKDIR /app/users-service
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/users-bin .

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/users-bin /users-bin
EXPOSE 3002
CMD ["/users-bin"]