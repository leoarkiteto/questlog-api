# Questlog API

Go backend for the Questlog personal game tracker. REST API backed by PostgreSQL.

## Stack

| Layer    | Tech                              |
| -------- | --------------------------------- |
| Runtime  | Go 1.23 (net/http, pgx/v5)       |
| Database | PostgreSQL (Neon in production)   |
| Hosting  | Render (Docker)                   |

## Quick start

```bash
# 1. Postgres (Docker) — skip if you have a database already:
make db-up

# 2. Configure:
cp .env.example .env
# edit .env → set DATABASE_URL (local: postgres://gamelog@localhost:5432/gamelog)

# 3. Run the API on :8080 (applies migrations automatically):
make run

# optional: fill the database with sample games
make seed
```

## API

```
GET    /api/health
GET    /api/games            ?status=wishlist|purchased|playing|played|dropped
POST   /api/games
GET    /api/games/{id}
PUT    /api/games/{id}
DELETE /api/games/{id}
GET    /api/catalog/search   ?q=term          (merged Steam + IGDB search)
GET    /api/catalog/app/{source}/{id}         (source: steam | igdb)
```

## Configuration

| Env var                | Default                                      | Description            |
| ---------------------- | -------------------------------------------- | ---------------------- |
| `DATABASE_URL`         | `postgres://gamelog@localhost:5432/gamelog`  | Postgres connection    |
| `PORT`                 | `8080`                                       | HTTP listen port       |
| `STEAM_API_KEY`        | *(optional)*                                 | Steam Web API key      |
| `IGDB_CLIENT_ID`       | *(optional)*                                 | Twitch app Client-ID   |
| `IGDB_CLIENT_SECRET`   | *(optional)*                                 | Twitch app Client Secret |

Secrets live in `.env` (gitignored). Copy `.env.example` and fill it in.

## Deploy to Render

1. Push this repo to GitHub.
2. On [Render](https://render.com), create a new **Web Service** connected to the repo.
   - **Environment:** Docker
   - **Health Check Path:** `/api/health`
3. Add environment variables: `DATABASE_URL` (Neon connection string), `STEAM_API_KEY`, `IGDB_CLIENT_ID`, `IGDB_CLIENT_SECRET`.
4. Render auto-deploys on every push to `main`.

Alternatively, use `render.yaml` (Blueprint) for Infrastructure as Code.

## Database (Neon)

In production, the database is hosted on [Neon](https://neon.tech). Set `DATABASE_URL` to your Neon connection string (includes `sslmode=require`). Migrations run automatically on startup.

For local dev, use the `docker-compose.yml` (Postgres 17 in Docker).

## Commands

- `make run` — start the API
- `make test` — run tests
- `make build` — compile binary
- `make migrate` — apply DB migrations
- `make seed` — insert sample games
- `make db-up/down/logs` — manage local Postgres
