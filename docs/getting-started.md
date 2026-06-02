# Getting Started

Ohm gives you a generated Go app that already knows how to boot, route,
migrate, render, test, and replay requests.

## Requirements

- Go 1.25 or newer.
- `just` for the generated task runner.
- Postgres if you choose the default database.

SQLite is available for local tools, smaller apps, and fast experiments.

## Install Ohm

Install the latest release:

```sh
go install github.com/mgomes/ohm/cmd/ohm@latest
```

Install from a local checkout:

```sh
go install ./cmd/ohm
```

Check the CLI:

```sh
ohm version
ohm help
```

## Create a SQLite App

Use SQLite when you want the fastest local start.

```sh
ohm new journal --db sqlite --module example.com/journal
cd journal
cp .env.development.example .env.development
cp .env.test.example .env.test
just check
just server
```

Open `http://localhost:8080`.

## Create a Postgres App

Postgres is the default.

```sh
ohm new journal --module example.com/journal
cd journal
cp .env.development.example .env.development
cp .env.test.example .env.test
```

Edit the copied env files and set `DATABASE_URL`.

```sh
just migrate-up
just check
just server
```

Tests that need Postgres require `DATABASE_URL`. They skip instead of silently
falling back to SQLite.

## Generate a Resource

Create a resource with SQL, migration, handler, and route wiring:

```sh
ohm generate resource Posts title:string body:text
just generate
just check
```

Smaller generators are available too:

```sh
ohm generate handler Posts
ohm generate migration create_posts
```

Generators only add files and route wiring. The generated code is normal app
code.

## Useful App Commands

Run commands through the generated app binary:

```sh
go run ./cmd/journal server
go run ./cmd/journal routes
go run ./cmd/journal migrate up
go run ./cmd/journal migrate down
go run ./cmd/journal migrate reset
go run ./cmd/journal migrate status
go run ./cmd/journal db seed
```

The generated `justfile` wraps those commands:

```sh
just routes
just migrate-status
just db-seed
just check
```

## Replay a Request

Replay snapshots live under `tmp/replays`.

```sh
go run ./cmd/journal replay ./tmp/replays/login-failure.json
go run ./cmd/journal replay --write-expected ./tmp/replays/login-failure.json
ohm generate test-from-replay ./tmp/replays/login-failure.json
```

Do not treat replay as magic. If a request depends on uncontrolled time,
randomness, external services, or database state, make that boundary explicit
before turning the replay into a regression test.
