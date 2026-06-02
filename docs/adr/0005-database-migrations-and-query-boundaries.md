# ADR 0005: Database, migrations, and query boundaries

## Status

Accepted

## Context

Ohm should have a database story that feels integrated without hiding SQL or
inventing an ORM. The framework should make the production path obvious while
leaving room for smaller applications and tests.

`sqlc` already provides typed query generation. That means Ohm does not need an
Active Record style model layer to make database access productive.

## Decision

Ohm will default to Postgres using `pgx`.

Generated applications may opt into SQLite at creation time:

```text
ohm new myapp --db sqlite
```

Ohm will use `sqlc` for query generation and `goose` for migrations. Migrations
must support both up and down directions.

The default layout is:

```text
internal/db/        connection management and sqlc generated package
migrations/         goose migration files
queries/            sqlc query source files, if kept separate from internal/db
```

The exact `sqlc` source and output paths may be refined during implementation,
but generated applications should clearly separate:

- Hand-written connection and transaction code.
- Hand-written SQL query files.
- Generated query code.
- Migration files.

Ohm will not provide a framework ORM.

Domain types may exist when they clarify business concepts, but database access
belongs to sqlc-generated query packages and explicit service workflows.

Ohm should provide a transaction helper that makes the common path concise
without hiding transaction ownership. Services should own multi-query workflows
that need a transaction.

Generated application CLIs should include migration commands:

```text
myapp migrate up
myapp migrate down
myapp migrate status
```

Generated justfiles should include common database tasks for migration, sqlc
generation, reset, and test setup.

## Consequences

Ohm keeps database access explicit and type-safe while still feeling integrated.

Postgres-first defaults let the framework optimize for production web
applications. SQLite remains available for smaller apps, local tools, and
possibly tests, but it should not distort the Postgres design.

The framework will need clear abstractions around database drivers because
Postgres and SQLite have different connection, migration, and sqlc
configuration needs.

## Open questions

- Should test databases default to Postgres, SQLite, or follow the selected app
  database?
- Should Ohm generate seed support in the first version?
- What transaction helper shape best balances explicitness and ergonomics?
