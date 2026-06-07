# ADR 0012: Use html/template for default views

## Status

Accepted on 2026-06-07

## Context

Ohm needs a server-rendered HTML path that feels like ordinary HTML and ordinary
Go. ADR 0006 selected `templ` because compile-time checked component functions
looked attractive for generated applications.

During implementation, the `templ` syntax made common page templates feel like
HTML and Go control flow interleaved in the same file. That is a poor default
for applications where most view files should read as HTML with small template
actions. Ohm should also avoid making a third-party template engine part of its
core response contract when the standard library already provides contextual
escaping for HTML.

The framework still needs a generic rendering boundary so applications can
adapt other view engines when they want them.

## Decision

Ohm will use the standard library `html/template` package as the default
server-rendered view system.

The root package will expose an Ohm-owned `HTML` interface instead of exposing a
third-party component type. `Request.HTML`, `RenderHTML`, `ohm.View`, and
`ohm.Fragment` accept that interface. `ohm.HTMLTemplate` adapts a named
`html/template` template into the interface.

Generated applications will keep markup in embedded `.html` files and provide
small typed Go constructors for pages, partials, and components.

## Consequences

View files are closer to plain HTML. A partial can be called from a template
with the standard `{{ template "partials/home" . }}` action, and handlers still
receive typed Go functions such as `pages.Home(title)`.

Ohm's core rendering contract is engine-neutral. Applications can use
`html/template` by default, write an `HTMLFunc`, or adapt another renderer
without changing the htmx adapter or response boundary.

Generated apps no longer need a view code-generation step or a `templ`
dependency. `just generate` only refreshes generated database query code.

The tradeoff is that `html/template` does not give compile-time checking for
template names and fields. Generated apps should parse templates at startup and
cover page, partial, and target-aware htmx rendering with tests.

Layout composition now has to be an explicit app convention. Generated apps use
a small `views.Page` helper that renders a trusted body template and wraps it in
the application layout.

## Alternatives considered

### Keep templ as the default

`templ` gives strong generated-code checking, but its syntax makes common loops,
conditionals, and component calls feel like Go embedded into HTML. That is not
the default experience Ohm wants for server-rendered applications.

### Support templ and html/template equally in the core

Ohm could expose adapters for both, but a split default would make docs,
generators, and examples harder to follow. The core `HTML` interface keeps the
door open without making two view systems first-class.

### Build an Ohm-specific template language

A custom language could be tailored to Ohm, but it would add a large maintenance
burden and duplicate the standard library's escaping work.

## References

- [ADR 0006: Server-rendered views](0006-server-rendered-views.md).
- [ADR 0011: HTML fragments and htmx adapter](0011-html-fragments-and-htmx-adapter.md).
