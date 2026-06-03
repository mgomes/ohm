# ADR 0002: CLI and generators

## Status

Accepted

## Context

Ohm applications should feel complete from the first generated commit. A core
part of that experience is being able to create, boot, inspect, and operate an
application through command-line interfaces with predictable conventions.

There are two related command surfaces:

- The framework CLI, used to create and modify applications.
- The generated application CLI, used to run and operate one application.

These should remain distinct. The framework CLI should not become a hidden
runtime dependency for deployed applications.

## Decision

Ohm will provide a framework CLI named `ohm`.

Both the framework CLI and generated application CLIs use Ohm's small `cli`
package. The shared package keeps command parsing, help text, and IO handling
consistent without making the framework CLI a runtime dependency for generated
applications.

The initial framework CLI should support:

```text
ohm new myapp --db postgres
ohm new myapp --db sqlite
ohm generate handler Posts
ohm generate migration create_posts
ohm generate resource Posts title:string body:text
```

Generated applications will expose their own application binary. The default
commands should include:

```text
myapp server
myapp routes
myapp migrate up
myapp migrate down
myapp migrate status
myapp db seed
```

The generated application CLI should be ordinary Go code in the application,
not a framework-owned runtime command. Applications can add their own commands
without registering them through a global framework plugin system.

The first resource generator writes a routed handler skeleton, route
registration, a migration, and a sqlc query file. It is additive and should not
overwrite application-owned files.

Generators should create boring, idiomatic Go code. They should prefer explicit
files and ordinary package boundaries over reflection, implicit naming rules, or
runtime discovery.

Generator output must be treated as maintainable application code. If a
generator cannot produce code that is clear enough for long-term ownership, the
generator should not exist yet.

## Consequences

Every generated app can be operated consistently in development, CI, and
production without depending on a separate framework command being present at
runtime.

Keeping generators explicit makes generated code straightforward to review,
test, and modify. The cost is that Ohm will need careful generator design and
snapshot tests to prevent drift.

This decision leaves room for richer generators later, including auth, mailers,
and background jobs, but those should be added only when the underlying
framework support is solid.

## Open questions

- Should generators support destructive cleanup, or only additive changes?
