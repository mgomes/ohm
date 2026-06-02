# Application guide

Ohm apps are plain Go apps with a few strong conventions.

The generated layout is:

```text
cmd/myapp/          CLI entrypoint
internal/app/       app wiring, logger, router, and dependencies
internal/handlers/  HTTP handlers
internal/views/     templ views, layouts, components, forms, and assets
internal/services/  business workflows
internal/db/        database connection, migrations, seeds, and generated queries
migrations/         goose migration files
queries/            sqlc query files
static/             static assets
tmp/replays/        local replay snapshots
```

The ownership rules are:

```text
handlers own HTTP
services own workflows
sqlc owns queries
views own rendering
```

## Commands

There are two CLIs.

The framework CLI creates and changes apps:

```sh
ohm new myapp --db postgres
ohm new myapp --db sqlite
ohm generate handler Posts
ohm generate migration create_posts
ohm generate resource Posts title:string body:text
ohm generate test-from-replay ./tmp/replays/login.json
```

The generated app CLI operates one app:

```sh
go run ./cmd/myapp server
go run ./cmd/myapp routes
go run ./cmd/myapp migrate up
go run ./cmd/myapp migrate down
go run ./cmd/myapp migrate reset
go run ./cmd/myapp migrate status
go run ./cmd/myapp db seed
go run ./cmd/myapp replay ./tmp/replays/login.json
```

The app CLI is app code. Add your own commands there when your app needs them.

## Handlers

Handlers receive an `*ohm.Request`.

Keep handlers focused on HTTP:

- Parse request input.
- Call a service or query.
- Render a response.
- Return errors to the framework boundary.

Do not put long workflows in handlers. Put those in `internal/services`.

## Views

Ohm uses `templ` for server-rendered HTML.

Generated views live under:

```text
internal/views/layouts/
internal/views/pages/
internal/views/components/
internal/views/forms/
internal/views/assets/
```

Render pages explicitly from handlers. Pass typed data into components. Use JSON
responses when an endpoint should return JSON.

HTML is the default path, not the only path.

## Config

Ohm includes a small `.env` loader and typed config decoder.

File loading is deterministic:

```text
.env
.env.<environment>
.env.local
.env.<environment>.local
process environment
```

`OHM_ENV` selects the environment. It defaults to `development`.

Process environment wins over file values. Boot should fail early when a
required value is missing or malformed.

Use `config.Secret` for values that must not print their raw value in logs or
errors.

## Database

Ohm uses SQL directly.

- Postgres is the default.
- SQLite is an explicit `--db sqlite` choice.
- `goose` owns migrations.
- `sqlc` owns typed query generation.
- Services own multi-query workflows and transactions.

Run migration commands through the app:

```sh
go run ./cmd/myapp migrate up
go run ./cmd/myapp migrate status
```

Regenerate database code through the generated task:

```sh
just sqlc
```

## Logging

Ohm standardizes on `slog`.

Generated apps wrap the JSON slog handler with Ohm's scrubber. The scrubber is
case-insensitive and matches common secret names such as password, token,
authorization, cookie, session, and API key.

Do not log full request bodies, cookies, or headers unless the app has a clear
reason. When you opt into more request data, keep the scrubber in the path.

## Testing

Generated apps use Go's `testing` package.

The generated `justfile` includes:

```sh
just test
just test-unit
just test-integration
just check
```

`just check` regenerates templ and sqlc output, then runs formatting checks,
module tidiness checks, vet, and tests.

Tests that require a database make that requirement explicit.

## Replay

Replay runs a request snapshot through the app handler stack.

Use it to debug a concrete request:

```sh
go run ./cmd/myapp replay ./tmp/replays/login.json
```

Write stable expected output into the snapshot:

```sh
go run ./cmd/myapp replay --write-expected ./tmp/replays/login.json
```

Generate a regression test:

```sh
ohm generate test-from-replay ./tmp/replays/login.json
```

Replay snapshots are local debugging artifacts. Before committing a snapshot,
review it and scrub sensitive values. Snapshots can contain request and
response detail, including expected response data.

Do not generate a replay test while the snapshot records uncontrolled
boundaries such as clock, randomness, external HTTP, email, file writes,
database state, or feature flags. Make the boundary deterministic first.
