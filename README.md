<p align="center">
  <img src="ohm-logo.svg#gh-light-mode-only" alt="Ohm" width="180">
  <img src="ohm-logo-dark.svg#gh-dark-mode-only" alt="Ohm" width="180">
</p>

---

Ohm is a low resistance, Go web framework for building server-rendered web apps with clear
defaults and ordinary Go code.

It gives you the shape of a complete web app without hiding Go:

- A framework CLI for creating apps and generating code.
- An app-owned CLI for serving, routing, migrating, seeding, and replaying.
- Routing behind a small Ohm handler layer.
- `templ` views for pages, layouts, components, forms, and errors.
- Typed config backed by deterministic `.env` loading.
- `sqlc` queries and `goose` migrations instead of an ORM.
- Structured `slog` logging with sensitive-value scrubbing.
- Replayable request snapshots that can become regression tests.

Ohm is for people who want the productive parts of a full-stack framework while
keeping ownership of ordinary Go code.

## Install

Install the latest released CLI:

```sh
go install github.com/mgomes/ohm/cmd/ohm@latest
```

From this checkout, install the local CLI:

```sh
go install ./cmd/ohm
```

Check the installed version:

```sh
ohm version
```

## Start an app

Use SQLite for a local app that doesn't need Postgres:

```sh
ohm new journal --db sqlite --module example.com/journal
cd journal
cp .env.development.example .env.development
cp .env.test.example .env.test
just check
just server
```

Postgres is the default production path:

```sh
ohm new journal --db postgres --module example.com/journal
```

Set `DATABASE_URL` in `.env.development` and `.env.test`, then run the same
checks.

## Generate code

```sh
ohm generate handler Posts
ohm generate migration create_posts
ohm generate resource Posts title:string body:text
```

The app owns generated code. Keep it readable, testable, and changeable.

## Operate the app

Each generated app has its own binary. The framework CLI is not required at
runtime.

```sh
go run ./cmd/journal server
go run ./cmd/journal routes
go run ./cmd/journal migrate up
go run ./cmd/journal migrate status
go run ./cmd/journal db seed
go run ./cmd/journal replay ./tmp/replays/request.json
```

## Documentation

- [Getting started](docs/getting-started.md)
- [Application guide](docs/application-guide.md)
- [Release guide](docs/releasing.md)
- [Architecture decisions](docs/adr)
