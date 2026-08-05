.PHONY: run test build migrate seed db-up db-down db-logs

## Run ------------------------------------------------------------------
run:             ## Run the Go API on :8080 (needs Postgres or DATABASE_URL)
	go run .

## Test & Build ----------------------------------------------------------
test:            ## Run unit tests
	go test ./...

vet:             ## Run go vet
	go vet ./...

build:           ## Compile the binary
	go build -o bin/questlog .

## Database (local) ------------------------------------------------------
db-up:           ## Start local Postgres via Docker
	docker compose up -d db

db-down:         ## Stop local Postgres
	docker compose down

db-logs:         ## Tail Postgres logs
	docker compose logs -f db

migrate:         ## Apply DB migrations (reads DATABASE_URL from env/.env)
	go run ./cmd/migrate

## Helpers ---------------------------------------------------------------
seed:            ## Insert sample games (API defaults to localhost:8080)
	@bash scripts/seed.sh
