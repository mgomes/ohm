# ADR 0001: Framework foundation

## Status

Accepted.

View-system defaults amended by [ADR 0012](0012-html-template-default-views.md)
on 2026-06-07.

## Context

Ohm is a Go web framework intended to provide the same sense of completeness
and good defaults that Ruby on Rails gives to Ruby applications, while staying
idiomatic to Go. It should be a convention layer over strong existing Go
libraries instead of a large hidden runtime or a framework-specific ORM.

The first applications built with Ohm should have a clear default path for:

- HTTP routing and rendering.
- Database access, schema migrations, and generated queries.
- Configuration and process boot.
- Structured logging with safe defaults.
- Server-rendered views.
- Common development tasks.
- Testable, maintainable application structure.

## Decision

Ohm will use these defaults:

- `chi` for HTTP routing.
- Ohm-owned request and response helpers for decoding, rendering, and response
  status handling.
- `sqlc` for typed query generation.
- `slog` for structured logging.
- Built-in log scrubbing for sensitive values in params, headers, errors, and
  panic reports.
- A built-in `.env` reader with typed application configuration.
- A CLI-first boot model where every app can start and operate through its own
  binary.
- A built-in `justfile` convention for common development tasks.
- A first-class server-rendered view system. ADR 0012 later selected the
  standard library `html/template` package as the default.
- `goose` as the default migration tool, with support for both up and down
  migrations.
- Postgres as the default database, using `pgx`.
- SQLite as an explicit generator option for smaller apps and tests.

Ohm should keep `chi` at the routing edge and hide direct route context access
behind framework-level routing, handler, middleware, rendering, and error APIs.
This does not mean replacing Go's HTTP model with implicit behavior. It means
application code should usually speak Ohm while advanced users retain escape
hatches to ordinary `http.Handler`, `http.Request`, and `http.ResponseWriter`.
The tracked `Request.ResponseWriter()` should be used for normal response
writes; `Request.RawResponseWriter()` exposes server- or middleware-specific
writer extensions when an integration needs the original writer.

Ohm will use `handlers` as the HTTP boundary name instead of `controllers`.
The initial application architecture is:

```text
cmd/myapp/          CLI entrypoint
internal/app/       app wiring, config, logger, router, dependencies
internal/handlers/  HTTP handlers, request parsing, response rendering
internal/views/     server-rendered views, templates, layouts, components
internal/services/  business workflows and transactions
internal/db/        db connection, migrations, sqlc generated queries
migrations/         goose migrations
static/             assets
```

The core ownership rules are:

```text
handlers own HTTP
services own workflows
sqlc owns queries
views own rendering
```

Ohm will not include background jobs in the initial framework scope. When jobs
are added, River is the preferred default for Postgres applications because it
keeps Postgres as the primary operational dependency.

## Consequences

This design keeps Ohm close to the Go ecosystem while still providing a coherent
framework experience. Applications can use ordinary Go packages, tooling, and
interfaces, while Ohm provides the conventions that make a new web app feel
complete on day one.

Using `sqlc` means Ohm should not recreate Active Record. Domain models may
exist where they improve clarity, but database access belongs to generated query
packages and explicit service workflows.

Using `handlers` avoids importing object-oriented controller assumptions into
Go. Handlers should stay focused on HTTP concerns: decoding input, invoking
services or queries, and rendering responses.

Postgres-first defaults let Ohm optimize for production web applications.
SQLite remains important, but as an intentional generator choice instead of the
primary runtime assumption.

Skipping jobs initially keeps the first framework surface smaller. Job support
can be added later with clearer requirements around retries, idempotency,
shutdown, observability, and scheduling.

## Initial non-goals

- Build a framework-specific ORM.
- Obscure Go's HTTP model with implicit runtime behavior.
- Include background jobs in the first version.
- Preserve backwards compatibility before the framework has a stable public API.
- Add local hacks or adapters that paper over missing framework design.

## Follow-up ADRs

- CLI command structure and generator behavior.
- Configuration loading and environment validation.
- Logger scrubbing rules and security defaults.
- View system selection and layout/component conventions.
- Database package boundaries, transactions, sqlc layout, and migrations.
- Testing strategy for generated applications and framework internals.
- HTTP abstraction boundaries and escape hatches.
