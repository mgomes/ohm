# ADR 0004: Structured Logging and Scrubbing

## Status

Accepted

## Context

Ohm should standardize on Go's `slog` package and provide secure defaults for
web applications. A framework logger should be useful in development and
production without accidentally recording passwords, tokens, cookies, or other
sensitive request data.

Rails provides parameter filtering as a default safety mechanism. Ohm should
provide a similar guarantee for structured Go logs.

## Decision

Ohm will use `slog` as the standard logging API.

The framework will provide a scrubbing `slog.Handler` wrapper. The wrapper will
recursively inspect attributes, groups, maps, request metadata, and framework
error payloads before they are written by the underlying handler.

The default scrubbed key list will include at least:

```text
password
passwd
pwd
token
secret
api_key
apikey
authorization
cookie
set-cookie
csrf
session
```

Matching should be case-insensitive and should work across common naming
styles, including snake case, kebab case, and camel case.

Request logging middleware should include:

- Request id.
- Method.
- Path.
- Route pattern when available.
- Status.
- Duration.
- Remote address.
- User agent.
- Content length when available.

Request logging should avoid logging full headers, cookies, request bodies, or
query params by default. When applications opt into logging params or headers,
the same scrubber must be applied.

Panic reports, framework error renderers, and development error pages must use
the same scrubbing policy before displaying or logging request data.

Applications may extend the scrub list through configuration. Applications may
also mark explicit values as sensitive so they are always redacted regardless of
attribute key.

## Consequences

The framework can make structured logging the default without making sensitive
data leaks the default.

Implementing scrubbing at the `slog.Handler` layer protects application logs
even when code logs through ordinary `slog` calls. Middleware and error
renderers still need to avoid capturing unnecessary data in the first place.

The scrubber must be heavily tested because it becomes part of Ohm's security
boundary.

## Open Questions

- What redaction string should Ohm use?
- Should Ohm support value-pattern scrubbing, or only key-based and explicit
  sensitive-value scrubbing?
- Should email addresses be scrubbed by default or left visible for operational
  debugging?
