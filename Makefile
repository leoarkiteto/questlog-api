.PHONY: run dev test vet build migrate seed db-up db-down db-logs templ css css-watch

## Run ------------------------------------------------------------------
run:             ## Run the Go server on :8080 (needs Postgres or DATABASE_URL)
	go run .

dev: css         ## Run locally, rebuilding CSS first
	go run .

## Test & Build ----------------------------------------------------------
test:            ## Run unit tests
	go test ./...

vet:             ## Run go vet
	go vet ./...

build: templ css ## Regenerate templates + CSS, then compile the binary
	go build -o bin/questlog .

## Codegen ---------------------------------------------------------------
templ:           ## Generate Go code from .templ files
	templ generate

css:             ## Compile Tailwind CSS into static/css/styles.css
	npx --yes @tailwindcss/cli@4.1.17 -i internal/web/css/input.css -o static/css/styles.css --minify

css-watch:       ## Rebuild CSS on change
	npx --yes @tailwindcss/cli@4.1.17 -i internal/web/css/input.css -o static/css/styles.css --watch

## Database (local) ------------------------------------------------------
db-up:           ## Start local Postgres via Docker
	docker compose up -d db

db-down:         ## Stop local Postgres
	docker compose down

db-logs:         ## Tail Postgres logs
	docker compose logs -f db

migrate:         ## Apply DB migrations (reads DATABASE_URL from env/.env)
	go run ./cmd/migrate

seed:            ## Insert sample games
	go run ./cmd/seed
