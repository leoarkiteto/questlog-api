# REASONIX.md

Guidance for AI coding agents working in this repository. Read this before making changes.

## What this is

Questlog is a **personal game tracker** built as a GOTTH monolith:

| Layer         | Tech                                                    |
| ------------- | ------------------------------------------------------- |
| Runtime       | Go 1.25+ (`net/http`, no framework)                     |
| Templates     | Templ (`.templ` → generated `*_templ.go`)               |
| Styling       | Tailwind CSS v4 (compiled to `static/css/styles.css`)   |
| Interactivity | HTMX 2 (catalog search/auto-fill, delete, redirects)    |
| Database      | PostgreSQL via `jackc/pgx/v5` (Neon in production)      |
| Hosting       | Render (Docker)                                         |

Everything is **server-rendered**. There is no SPA, no API layer for the UI, and no separate frontend project. The browser receives complete HTML pages and small HTMX fragments.

## Project layout

The repo follows the [golang-standards/project-layout](https://github.com/golang-standards/project-layout) conventions.

```
cmd/                       # entrypoint binaries (one package per binary)
  migrate/                 #   apply DB migrations, then exit
  seed/                    #   insert sample games (into a user's collection)
  user/                    #   closed registration: create accounts from the CLI
internal/                  # private application code (not importable externally)
  auth/                    #   sessions, CSRF, login flow (closed registration)
  catalog/                 #   merged Steam + IGDB + HLTB service
  config/                  #   .env loading
  hltb/                    #   HowLongToBeat client (time-to-beat)
  igdb/                    #   IGDB client (Twitch app credentials)
  model/                   #   Game/User structs + Status enum + validation
  password/                #   Argon2id + pepper password hashing (PHC format)
  repo/                    #   Postgres store, migrations, queries
  steam/                   #   Steam Web API client
  web/                     #   HTTP handlers, views, Templ components
    css/input.css          #   Tailwind input
static/                    # embedded assets (served under /static/)
  css/styles.css           #   compiled Tailwind (committed)
  js/htmx.min.js           #   vendored HTMX 2 (committed)
  js/app.js                #   tiny UI-chrome helper (see CSS-over-JS rule)
  questlog.png             #   logo
main.go                    # server bootstrap (embeds static/, wires deps)
.env.example               # template for local secrets
docker-compose.yml         # local Postgres 17
Dockerfile                 # multi-stage Go build → alpine
render.yaml                # Render Blueprint (service + health check)
```

### Package map

- **`internal/web`** — the whole UI. `handlers.go` defines the routes and logic; `*.templ` files define the markup; `views.go`/`helpers.go`/`meta.go` hold pure presentation helpers; `render.go` writes Templ components as responses. `icons.go` stores inline SVG icon paths (Lucide for UI, Simple Icons for platform brands). Auth middleware (`requireAuth`, `csrfProtect`) gates every route except `/login`, `/health` and `/static/`; the signed-in session lives in the request context.
- **`internal/auth`** — closed registration and sessions. `Service` mints opaque 32-byte session tokens (only SHA-256 hashes are stored) with per-session CSRF tokens, delegates password hashing to `internal/password`, and transparently upgrades legacy hash formats on login. No `/register` page: accounts are created via `cmd/user`.
- **`internal/password`** — password hashing with **Argon2id + pepper** (`PASSWORD_PEPPER`). The pepper is keyed into the password via HMAC-SHA256 before Argon2id and never stored; hashes use the PHC format `$argon2id$v=19$m=65536,t=1,p=4$<salt>$<key>` (OWASP defaults, configurable via `password.WithParams`). `Verify` re-derives the key from the hash's embedded parameters and compares in constant time (`crypto/subtle`); legacy bcrypt hashes are accepted and flagged for in-place upgrade.
- **`internal/repo`** — all SQL lives here. `Store` wraps a `pgxpool.Pool`. Migrations are embedded from `migrations/*.sql` and applied automatically on startup. Every game query is scoped by `user_id`.
- **`internal/model`** — the `Game` struct, the `Status` enum (`wishlist`, `purchased`, `playing`, `played`, `dropped`), and `Validate()`.
- **`internal/catalog`** — one interface over Steam (primary) + IGDB (fallback for non-Steam titles, only when Twitch credentials are set) + HowLongToBeat (time-to-beat enrichment).

## Routes

```
GET    /login                                      sign-in page (public)
POST   /login                                      authenticate (public)
POST   /logout                                     end session
GET    /profile                                    user profile (account info + per-status stats)
GET    /                                            dashboard (hero + status rows)
GET    /library?filter=&platform=&sort=&page=   filtered/sorted grid, 24/page + HTMX "load more" sentinel
GET    /search?q=                                   search the collection
GET    /games/new                                   add form
POST   /games                                       create
GET    /games/{id}                                  detail
GET    /games/{id}/edit                             edit form
POST   /games/{id}                                  update
POST   /games/{id}/delete                           delete (HTMX → HX-Redirect home)
POST   /games/{id}/enrich                           fetch cover/metadata from catalog
GET    /partials/catalog/search                     HTMX: Steam/IGDB suggestions
GET    /partials/catalog/app/{source}/{appid}       HTMX: auto-fill form fields
GET    /health                                      health check (Render, public)
```

Everything except `/login`, `/health` and `/static/` requires a session; unauthenticated visitors are sent to `/login?next=<path>`. State-changing requests must carry a CSRF token: the per-session token (from the DB) for authenticated forms/HTMX posts, or the anonymous `questlog_csrf` cookie for the login form.

Routes are registered with Go 1.22+ method patterns (`GET /games/{id}`, `POST /games`, …) on a single `http.ServeMux`.

## Frontend conventions (read before touching UI)

### CSS over JavaScript

Interactivity must be expressed with **CSS + HTMX first**. Do not reach for JavaScript unless HTMX cannot express the behavior declaratively.

- HTMX is the only mechanism for data-driven updates: catalog search suggestions, form auto-fill, delete-and-redirect. These are plain `hx-*` attributes in `.templ` files and HTMX partial handlers in `handlers.go`.
- `static/js/app.js` is intentionally tiny and only covers pure UI chrome HTMX can't do: the library filter slide-over (`qlOpenFilter`/`qlCloseFilter`/`setFilter`), clearing the rating stars (`qlClearRating`), and closing the filter on `Escape`. Keep it that way — do not grow it into an app framework.
- Prefer CSS-only patterns: `peer-checked:` for the status radio pills, `focus-within:` for the search field, `group-hover:` for cards, inline SVG icons with `currentColor` so color comes from Tailwind classes.

### AlpineJS

**Do not add AlpineJS (or any other JS framework) as part of normal work.** It is only acceptable if a feature genuinely cannot be done with CSS + HTMX and the extra dependency is justified. If you think AlpineJS is required, stop and confirm with the user first — the default is to solve it with CSS/HTMX.

### Templ workflow

- Edit the `.templ` files, **never** the generated `*_templ.go` files directly.
- Generated `*_templ.go` and compiled `static/css/styles.css` are **committed**, so `go build`/`go run` work without Node or the `templ` CLI.
- After editing templates or Tailwind, regenerate:

  ```bash
  make templ        # templ generate (go install github.com/a-h/templ/cmd/templ@v0.3.1020)
  make css          # npx @tailwindcss/cli@4.1.17 → static/css/styles.css
  ```

- Tailwind v4 content sources are declared in `internal/web/css/input.css` via `@source` (`.templ`, `.go`, `static/js/*.js`). When adding new templates/Go files that use Tailwind classes, confirm the `@source` globs already cover them.

## Commands (Makefile)

```bash
make run        # start server on :8080 (auto-applies migrations)
make dev        # rebuild CSS, then run
make test       # go test ./...
make vet        # go vet ./...
make build      # templ + css + compile bin/questlog
make migrate    # go run ./cmd/migrate
make seed       # insert sample games (default: first account; pass USER=email@x.com)
make user       # create an account: make user EMAIL=me@x.com (closed registration)
make db-up      # docker compose up -d db (local Postgres 17)
make db-down    # docker compose down
make db-logs    # docker compose logs -f db
make templ      # regenerate *_templ.go
make css        # recompile Tailwind
make css-watch  # watch Tailwind
```

First-time setup for a fresh database: `make db-up`, then create the owner
(`make user EMAIL=you@example.com`, or `echo 's3cret' | go run ./cmd/user create you@example.com`),
then optionally `make seed` to insert sample games into that account.

## Configuration

Env vars (`.env` is gitignored; **existing shell env vars always win** over `.env`):

| Env var              | Default                                                     | Notes                                   |
| -------------------- | ----------------------------------------------------------- | --------------------------------------- |
| `DATABASE_URL`       | `postgres://gamelog:gamelog@localhost:5432/gamelog`         | Postgres connection (Neon in prod)      |
| `PORT`               | `8080`                                                      | HTTP listen port                        |
| `SESSION_MAX_AGE`    | `720h` (30 days)                                            | Session lifetime as a Go duration       |
| `PASSWORD_PEPPER`    | **required**                                                | Secret keyed into password hashes (Argon2id + pepper); must match between server and `cmd/user` |
| `STEAM_API_KEY`      | *(optional)*                                                | Steam Web API key (not needed for search/appdetails) |
| `IGDB_CLIENT_ID`     | *(optional)*                                                | Twitch app Client-ID; enables IGDB fallback |
| `IGDB_CLIENT_SECRET` | *(optional)*                                                | Twitch app Client Secret                |

`internal/config` loads `.env` with a tiny hand-rolled parser (no third-party dotenv lib). `internal/igdb` reads `IGDB_BASE_URL`/`IGDB_TOKEN_URL` for test overrides.

## Database & migrations

- Migrations are SQL files in `internal/repo/migrations/`, embedded via `go:embed` and applied **in filename order** on startup (and by `make migrate`). They are tracked in a `schema_migrations` table.
- **Migrations are additive and immutable** — never edit an already-applied migration; add a new numbered file (`NNNN_name.sql`) instead.
- `internal/repo` sets `QueryExecModeCacheDescribe` on the pool so it works behind pgbouncer/Neon's pooled connections — keep that setting.
- **Uniqueness rule:** a game may appear only once in **each user's** collection, keyed by normalized title OR (non-null) Steam app id. This is enforced both in Go (`repo.normalizeTitle`, `findDuplicate`, `ErrDuplicate`) and at the DB level (migration `0007_games_user.sql`'s per-user unique indexes, which replaced the global ones from 0005).
  - `normalizeTitle` (Go) and the SQL expression in migration 0005 **must stay in sync**: `lower(btrim(regexp_replace(title, '[[:space:]]+', ' ', 'g')))`.
- **Accounts & sessions** (migration `0006_users_sessions.sql`): `users` (email unique in lowercase, bcrypt `password_hash`) and `sessions` (SHA-256 hash of the 32-byte token, per-session CSRF token, expiry). `games.user_id` is a nullable FK so pre-account rows survive; the **first user created via `cmd/user` adopts the orphan rows** (`AdoptOrphanGames`).

## Testing

- `go test ./...` runs the unit tests. Most are pure unit tests (`repo`, `auth`, `catalog`, `igdb`, `hltb`, `model`, `config`) that do **not** require a database or network — they use table-driven tests and stubbed clients/fakes.
- The CI workflow (`.github/workflows/ci.yml`) runs `go vet ./...`, `go test ./...`, and `go build` on every push/PR to `main`, then triggers a Render deploy hook on `main`.

## Gotchas

- **Don't edit generated files.** `internal/web/*_templ.go` and `static/css/styles.css` are build artifacts; regenerate them via `make templ` / `make css`.
- **Keep handlers thin and presentation helpers in the `web` package** (`views.go`, `helpers.go`, `meta.go`) so `.templ` files stay declarative.
- `render()` sets headers and calls `WriteHeader` before rendering; render errors can only be logged, not turned into a different status. Validate inputs before rendering.
- Deleting responds differently to HTMX (`HX-Redirect` + `204`) vs. full-page requests (`303` redirect) — check `HX-Request` header.
- The status radio pills and rating stars use `sr-only` inputs + `peer-checked:`/`checked` so they work without JS.
- **Auth:** registration is closed — no `/register` route; accounts are created with `make user`/`cmd/user`. The session cookie is `HttpOnly` + `SameSite=Lax` (+ `Secure` behind TLS), backed by the Postgres `sessions` table. Every state-changing request needs a CSRF token: the per-session token from the DB (rendered into forms / `hx-vals`), or the anonymous `questlog_csrf` cookie on `/login`. Keep `requireAuth` + `csrfProtect` wrapping the mux when adding routes.
- **Passwords:** `PASSWORD_PEPPER` is required everywhere hashing happens (server + `cmd/user`) and must be identical across processes — rotating it invalidates every password. Hashes are Argon2id PHC strings (`$argon2id$v=19$...`); legacy bcrypt hashes verify and are upgraded in place on login.
- Steam cover URLs use the fixed `library_600x900.jpg` portrait asset (2:3 card ratio); IGDB covers are rewritten to `/t_cover_big_2x/`.
- CI/CD deploys to Render only from `main`; the deploy hook is gated on `RENDER_DEPLOY_HOOK` being set.
