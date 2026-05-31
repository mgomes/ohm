# ADR 0008: Defer Background Jobs

## Status

Accepted

## Context

Background jobs are important for a Rails-like web framework, but they add a
large amount of surface area: retries, idempotency, scheduling, queue
priorities, observability, shutdown behavior, operational dashboards, and dead
job handling.

Ohm's first version should focus on the request-response application core:
routing, rendering, configuration, logging, database access, migrations, views,
testing, and generators.

## Decision

Ohm will not include background jobs in the initial framework scope.

When built-in jobs are added, River is the preferred default for Postgres
applications. River matches Ohm's Postgres-first direction and avoids making
Redis or another queue service a default operational dependency.

SQLite applications will not receive a background job default until Ohm has a
clear design that does not compromise the Postgres path or require a weak
compatibility abstraction.

Future job support should include an ADR before implementation. That ADR should
cover:

- Job declaration and registration.
- Enqueue APIs.
- Transactional enqueue behavior.
- Retry policy.
- Idempotency expectations.
- Queue naming.
- Scheduled jobs.
- Shutdown and worker lifecycle.
- Logging, metrics, and tracing.
- Testing helpers.

## Consequences

Deferring jobs keeps the first version of Ohm smaller and lets the core web
framework settle before adding long-running worker behavior.

Choosing River as the likely future path keeps the framework aligned with its
Postgres-first defaults, but this ADR does not commit Ohm to a job API before
the real requirements are designed.

Applications that need jobs before Ohm has built-in support can integrate River
directly in application code.
