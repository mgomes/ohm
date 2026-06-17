# Get started

An Ohm app includes commands for booting, routing, migrating, rendering,
testing, and replaying requests.

## Requirements

- Go 1.25 or newer.
- `just` for the generated task runner.
- Postgres if you choose the default database.

Choose SQLite for local tools, smaller apps, and experiments.

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

## Create a SQLite app

Use SQLite for a local app that doesn't need Postgres.

```sh
ohm new journal --db sqlite --module example.com/journal
cd journal
cp .env.development.example .env.development
cp .env.test.example .env.test
just check
just server
```

Open `http://localhost:3000`.

## Create a Postgres app

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

## Generate a resource

Create a resource with SQL, migration, handler, and route wiring:

```sh
ohm generate resource Posts title:string body:text
just check
```

You can also generate one file group at a time:

```sh
ohm generate handler Posts
ohm generate migration create_posts
```

Generators only add files and route wiring. The generated code is normal app
code.

Use `just generate` when you only want to refresh generated sqlc output.

## Useful app commands

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

## Replay a request

Replay snapshots live under `tmp/replays`.

```sh
go run ./cmd/journal replay ./tmp/replays/login-failure.json
go run ./cmd/journal replay --write-expected ./tmp/replays/login-failure.json
ohm generate test-from-replay ./tmp/replays/login-failure.json
```

Replay depends on deterministic boundaries. If a request depends on time,
randomness, external services, database state, or feature flags, record that
boundary in the snapshot. Mark pinned dependencies as `controlled_boundaries`
and leave changing dependencies in `uncontrolled_boundaries` until the replay is
stable enough for a regression test.
