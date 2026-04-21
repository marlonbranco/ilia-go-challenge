.PHONY: certs proto up down build test lint clean

certs:
	@bash scripts/gen-certs.sh

proto:
	@PATH="$$PATH:$$HOME/go/bin" protoc \
		--go_out=proto/wallet \
		--go_opt=paths=source_relative \
		--go-grpc_out=proto/wallet \
		--go-grpc_opt=paths=source_relative \
		--proto_path=proto \
		proto/wallet.proto

up: certs
	@test -f .env || { echo "ERROR: .env not found. Run: cp .env.example .env  (then set a strong JWT_SECRET)"; exit 1; }
	docker compose up --build -d

down:
	docker compose down

build:
	docker compose build

test:
	cd ms-users  && go test -short ./...
	cd ms-wallet && go test -short ./...

test-integration:
	cd ms-users  && go test ./...
	cd ms-wallet && go test ./...

lint:
	cd ms-users  && go vet ./...
	cd ms-wallet && go vet ./...

clean:
	docker compose down -v --remove-orphans
	rm -rf certs/
