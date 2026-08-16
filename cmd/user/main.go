// Command user manages Questlog accounts. Registration is closed: new
// accounts are created here and passwords are reset here, not through
// the web UI.
//
//	go run ./cmd/user create me@example.com               # password read from stdin
//	go run ./cmd/user create me@example.com -p s3cret
//	echo s3cret | go run ./cmd/user create me@example.com
//	go run ./cmd/user set-password me@example.com         # reset password from stdin
//
// The first account created adopts the games that predate accounts
// (rows with user_id NULL) into its collection.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/leoarkiteto/questlog-api/internal/auth"
	"github.com/leoarkiteto/questlog-api/internal/config"
	"github.com/leoarkiteto/questlog-api/internal/password"
	"github.com/leoarkiteto/questlog-api/internal/repo"
)

func main() {
	_ = config.LoadDotEnv(".env")

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "create":
		createCmd(os.Args[2:])
	case "set-password":
		setPasswordCmd(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

// parseUserArgs extracts <email> and an optional -p/--password value
// from a command's arguments. On -h/--help, or when email is missing,
// it prints usage and exits.
func parseUserArgs(args []string) (email, password string) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-p" || args[i] == "--password":
			if i+1 < len(args) {
				i++
				password = args[i]
			}
		case strings.HasPrefix(args[i], "-p="):
			password = strings.TrimPrefix(args[i], "-p=")
		case args[i] == "-h" || args[i] == "--help":
			usage()
			os.Exit(0)
		case !strings.HasPrefix(args[i], "-") && email == "":
			email = args[i]
		}
	}

	email = strings.TrimSpace(email)
	if email == "" {
		usage()
		os.Exit(2)
	}
	return email, password
}

func createCmd(args []string) {
	email, password := parseUserArgs(args)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gamelog:gamelog@localhost:5432/gamelog"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := repo.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()

	// The account tables must exist before inserting (make migrate runs
	// the same embedded migrations).
	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	svc := auth.New(store, store, mustPasswordService())

	pass := password
	if pass == "" {
		pass, err = readPassword()
		if err != nil {
			log.Fatalf("password: %v", err)
		}
	}

	firstUser, err := store.UserCount(ctx)
	if err != nil {
		log.Fatalf("count users: %v", err)
	}

	u, err := svc.Register(ctx, email, pass)
	if errors.Is(err, auth.ErrEmailTaken) {
		log.Fatalf("account %q already exists", auth.NormalizeEmail(email))
	}
	if err != nil {
		log.Fatalf("create account: %v", err)
	}

	if firstUser == 0 {
		adopted, err := store.AdoptOrphanGames(ctx, u.ID)
		if err != nil {
			log.Fatalf("adopt pre-existing games: %v", err)
		}
		if adopted > 0 {
			log.Printf("adopted %d pre-existing game(s) into %s", adopted, u.Email)
		}
	}

	log.Printf("created account %s (id %d)", u.Email, u.ID)
}

// setPasswordCmd replaces an existing account's password. This is the
// only supported way to reset a password: registration is closed and
// the web UI has no change-password page.
func setPasswordCmd(args []string) {
	email, password := parseUserArgs(args)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gamelog:gamelog@localhost:5432/gamelog"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := repo.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	svc := auth.New(store, store, mustPasswordService())

	pass := password
	if pass == "" {
		pass, err = readPassword()
		if err != nil {
			log.Fatalf("password: %v", err)
		}
	}

	if err := svc.SetPassword(ctx, email, pass); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Fatalf("no account found for %q", auth.NormalizeEmail(email))
		}
		log.Fatalf("set password: %v", err)
	}

	log.Printf("updated password for %s", auth.NormalizeEmail(email))
}

// mustPasswordService builds the Argon2id + pepper hasher from
// PASSWORD_PEPPER, failing fast when the secret is missing or too short
// (the CLI and the server must share the same pepper).
func mustPasswordService() *password.Service {
	pw, err := password.New(os.Getenv("PASSWORD_PEPPER"))
	if err != nil {
		log.Fatalf("password: %v (set PASSWORD_PEPPER, e.g. openssl rand -hex 32)", err)
	}
	return pw
}

// readPassword reads one line from stdin. It is not echoed-hidden
// (that would need golang.org/x/term); pipe the password instead:
//
//	echo 's3cret' | go run ./cmd/user create me@example.com
func readPassword() (string, error) {
	fmt.Print("Password: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  go run ./cmd/user create <email> [-p <password>]
  go run ./cmd/user set-password <email> [-p <password>]

create registers a new account. set-password replaces an existing
account's password. When -p is omitted the password is read from stdin,
so it can be piped:

  echo 's3cret' | go run ./cmd/user create me@example.com
  echo 'n3w-s3cret' | go run ./cmd/user set-password me@example.com

The first account created adopts any games that predate accounts.
Passwords must be 8-1024 characters and must use the same PASSWORD_PEPPER
as the server.`)
}
