# Questlog

GOTTH monolith for the Questlog personal game tracker — Go + Templ + Tailwind CSS + HTMX, backed by PostgreSQL. It renders every page server-side and serves its own static assets; there is no separate frontend.

## Stack

| Layer          | Tech                                      |
| -------------- | ----------------------------------------- |
| Runtime        | Go 1.25 (net/http, pgx/v5)                |
| Templates      | Templ (`.templ` → generated Go)           |
| Styling        | Tailwind CSS v4 (compiled)                |
| Interactivity  | HTMX 2 (catalog search/auto-fill, delete) |
| Database       | PostgreSQL (Neon in production)           |
| Hosting        | Render (Docker)                           |

## Quick start

```bash
# 1. Postgres (Docker) — skip if you have a database already:
make db-up

# 2. Configure:
cp .env.example .env
# edit .env → DATABASE_URL (local: postgres://gamelog:gamelog@localhost:5432/gamelog)

# 3. Run the server on :8080 (applies migrations automatically):
make run

# optional: fill the database with sample games
make seed
```

Open http://localhost:8080.

The compiled Tailwind CSS (`static/css/styles.css`) and generated Templ Go code (`internal/web/*_templ.go`) are committed, so you don't need Node or the `templ` CLI just to run or build the app.

## Routes

```
GET    /                                 dashboard (featured hero + status rows)
GET    /library?filter=&platform=&sort=  grid with server-side filter/sort
GET    /search?q=                        search your collection
GET    /games/new                        add a game
POST   /games                            create a game
GET    /games/{id}                       game detail
GET    /games/{id}/edit                  edit form
POST   /games/{id}                       update a game
POST   /games/{id}/delete                delete (HTMX → redirects home)
POST   /games/{id}/enrich                fetch cover/metadata from the catalog
GET    /partials/catalog/search          HTMX: Steam/IGDB suggestions
GET    /partials/catalog/app/{source}/{appid}  HTMX: auto-fill form fields
GET    /health                           health check (Render)
```

## Configuration

| Env var                | Default                                      | Description            |
| ---------------------- | -------------------------------------------- | ---------------------- |
| `DATABASE_URL`         | `postgres://gamelog:gamelog@localhost:5432/gamelog` | Postgres connection |
| `PORT`                 | `8080`                                       | HTTP listen port       |
| `STEAM_API_KEY`        | *(optional)*                                 | Steam Web API key      |
| `IGDB_CLIENT_ID`       | *(optional)*                                 | Twitch app Client-ID   |
| `IGDB_CLIENT_SECRET`   | *(optional)*                                 | Twitch app Client Secret |

Secrets live in `.env` (gitignored). Copy `.env.example` and fill it in.

## Deploy to Render

1. Push this repo to GitHub.
2. On [Render](https://render.com), create a new **Web Service** connected to the repo (or use the included `render.yaml` Blueprint).
   - **Environment:** Docker
   - **Health Check Path:** `/health`
3. Add environment variables: `DATABASE_URL` (Neon connection string), and optionally `STEAM_API_KEY`, `IGDB_CLIENT_ID`, `IGDB_CLIENT_SECRET`.
4. Render auto-deploys on every push to `main`.

The `render.yaml` Blueprint defines the service (`questlog`) and health check declaratively.

## Database (Neon)

In production the database is hosted on [Neon](https://neon.tech). Set `DATABASE_URL` to your Neon connection string (includes `sslmode=require`). Migrations run automatically on startup (embedded via `go:embed`). For local dev use `docker-compose.yml` (Postgres 17).

## Development

Generated artifacts are committed, but regenerate them after editing templates/styles:

```bash
make templ        # templ generate  (needs: go install github.com/a-h/templ/cmd/templ@v0.3.1020)
make css          # recompile Tailwind (needs Node; uses npx @tailwindcss/cli@4.1.17)
make css-watch    # watch Tailwind
make dev          # css + go run
```

## Commands

- `make run` — start the server
- `make dev` — rebuild CSS then run
- `make test` — run tests
- `make vet` — go vet
- `make build` — regenerate templates + CSS, then compile `bin/questlog`
- `make migrate` — apply DB migrations (`go run ./cmd/migrate`)
- `make seed` — insert sample games (`go run ./cmd/seed`)
- `make db-up/down/logs` — manage local Postgres
