# ADR 0003: Configuration and Environment Loading

## Status

Accepted

## Context

Ohm applications need a simple configuration story that works locally,
under test, and in production. The framework should provide good defaults while
remaining explicit about where configuration comes from.

The framework should include a built-in `.env` reader. The goal is not to
emulate a shell. The goal is deterministic local configuration with clear
validation errors.

## Decision

Ohm will provide a small built-in `.env` parser and typed configuration loading
system.

The `.env` parser will support:

- `KEY=value` pairs.
- Blank lines.
- Comments beginning with `#`.
- Single-quoted and double-quoted values.
- Escaped newlines inside quoted values.

The parser will not initially support shell execution, command substitution,
or implicit environment variable expansion.

Configuration precedence will be deterministic. Existing process environment
variables have highest priority and should not be overwritten by `.env` files.
The default file loading order is:

```text
.env
.env.<environment>
.env.local
.env.<environment>.local
process environment
```

The environment name should default to `development` and be controlled by
`OHM_ENV`.

Applications should define typed configuration structs. Boot should fail early
with clear errors when required settings are missing or malformed.

Configuration loading should be usable by all application commands, including
`server`, `migrate`, `routes`, tests, and custom app commands.

## Consequences

Ohm applications get a Rails-like local configuration experience without
depending on external `.env` packages or shell-specific behavior.

Typed configuration keeps application boot failures close to startup instead of
allowing missing values to fail later inside handlers or services.

Avoiding shell expansion makes the parser smaller, safer, and easier to test.
If expansion becomes necessary later, it should be added deliberately with an
ADR and a precise compatibility contract.

## Open Questions

- Should Ohm reserve additional environment names beyond `development`, `test`,
  and `production`?
- Should generated apps include `.env.example` by default?
- Should secrets be represented by a dedicated type that redacts itself in
  logs and error messages?
