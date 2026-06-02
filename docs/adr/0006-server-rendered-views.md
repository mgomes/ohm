# ADR 0006: Server-rendered views

## Status

Accepted

## Context

Ohm should support server-rendered Go web applications with structure for
layouts, pages, components, forms, errors, and assets without requiring a
separate frontend application.

The framework should also preserve the explicitness and compile-time feedback
that make Go attractive.

## Decision

Ohm will prefer `templ` as the default server-rendered view system.

Generated applications should organize views under:

```text
internal/views/layouts/
internal/views/pages/
internal/views/components/
```

The framework should provide rendering helpers that integrate templ components
with `github.com/go-chi/render`.

The default view conventions should include:

- Application layout.
- Page components.
- Reusable components.
- Error pages.
- Form helpers.
- Flash message rendering.
- Asset path helpers.

View rendering should stay explicit. Handlers should choose the page or
component they render, pass typed data into it, and return either HTML or a
structured response through the same rendering boundary.

Ohm should support JSON rendering as a first-class path through `chi/render`.
HTML views are a default, not the only response type.

## Consequences

Using templ gives Ohm compile-time checking for server-rendered views and keeps
view code close to ordinary Go.

Integrating templ with `chi/render` keeps the framework's response story
consistent for HTML, JSON, redirects, and errors.

Ohm should avoid building a large custom template language. If templ proves to
be the wrong default during implementation, the framework should revisit this
decision before adding compatibility layers.

## Open questions

- Should `html/template` be supported as a secondary option?
- How much form-builder behavior belongs in Ohm versus application code?
- Should asset fingerprinting be part of the first view implementation?
