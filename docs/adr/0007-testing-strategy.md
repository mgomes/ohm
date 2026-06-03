# ADR 0007: Testing strategy

## Status

Accepted

## Context

Ohm should make generated applications testable from the first day. The
framework itself also needs strong tests because generators, scrubbing,
configuration loading, migrations, and rendering all create long-term
maintenance risk if they drift.

The testing approach should support closed-loop verification. Developers should
be able to run a small set of commands and know whether the generated app still
boots, routes, migrates, renders, and handles requests correctly.

## Decision

Ohm will use Go's standard `testing` package as the default test runner.

Generated applications should include:

- HTTP integration test helpers built on `httptest`.
- Route smoke tests.
- Configuration loading tests.
- Migration tests.
- Database test helpers.
- View rendering smoke tests.

Framework internals should include:

- Golden or snapshot tests for generator output.
- Parser tests for `.env` behavior.
- Scrubber tests for nested `slog` attributes and request data.
- Migration command tests.
- CLI command tests.
- Generated-app smoke tests that build and exercise a real generated app.

The generated `justfile` should provide:

```text
just test
just test-unit
just test-integration
just check
```

Tests that require a database should make that requirement explicit. They
should not silently fall back to a different database engine unless the test is
specifically written to validate that behavior.

Generated view tests should use rendered HTML assertions rather than golden
files in the first version. This keeps each test close to the behavior it
protects and avoids broad fixture churn while the generated view structure is
still settling.

## Consequences

Ohm's default application structure will be shaped by testability. App wiring,
handlers, services, configuration, database connections, and views need to be
constructable in tests without starting the full production process.

Generator tests will become important because generated code is part of the
framework's public experience.

Database tests may require more setup than pure unit tests, but this is the
right tradeoff for a framework that makes database-backed web applications its
main path.

## Open questions

- Should generated apps include a default Postgres test container workflow?
- Should Ohm provide fixture helpers, factory helpers, both, or neither?
