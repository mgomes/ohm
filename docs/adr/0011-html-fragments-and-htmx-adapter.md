# ADR 0011: HTML fragments and htmx adapter

## Status

Accepted

## Context

Ohm already treats server-rendered HTML as a first-class application path. ADR
0006 selected `templ` as the preferred view system and established generated
view directories for layouts, pages, and components.

Applications built with Ohm should be able to use progressive enhancement:
ordinary browser requests should receive full HTML documents, while enhanced
requests should be able to receive smaller HTML fragments that update one
region of the page. This is especially important for htmx, where the server
typically responds with HTML and the client swaps that response into a target
element.

The framework needs a convention for this without making every handler
hand-roll request header checks, without duplicating route logic across full-page
and fragment endpoints, and without making the Ohm core permanently shaped by
one client-side library.

Rails-style partials are a useful mental model, but Ohm should not copy Rails'
file naming directly. Go ignores source files whose names begin with `_`, so
leading-underscore partial names would be surprising and fragile in a `templ`
codebase.

## Decision

Ohm will make HTML fragments a core server-rendered view concept, and will add a
separate htmx adapter as a blessed integration path.

The core framework owns the generic model:

- A full HTML response is a `templ.Component` intended to render a complete page
  or layout-wrapped screen.
- An HTML fragment is a `templ.Component` intended to update a named page region.
- Handlers should be able to declare a full response and one or more named
  fragments from the same view model.

Client-specific adapters own request negotiation. The htmx adapter should parse
htmx request headers, choose the right fragment when the target is explicit, and
fall back to the full response when a full document is required.

The generated application layout should grow a dedicated partials directory:

```text
internal/views/layouts/
internal/views/pages/
internal/views/partials/
internal/views/components/
```

`pages` remain full screens. `partials` are route-addressable fragments with
stable target semantics. `components` remain smaller reusable pieces that may be
used by pages or partials but do not, by themselves, imply a route or swap
target.

The htmx adapter should follow conservative response selection:

- Normal browser requests render the full response.
- htmx history-restore requests render the full response.
- htmx requests with a matching explicit target render that target's fragment.
- Ambiguous boosted requests render the full response unless the handler opts
  into a single-fragment fallback.
- Missing or unknown targets return an application error rather than silently
  rendering the wrong fragment.

The htmx adapter may also expose helpers for htmx response headers such as
client-side redirects, refreshes, retargeting, URL replacement, and event
triggers. Those helpers belong in the adapter package, not on `ohm.Request`.

## Non-goals

- Make Ohm an htmx-only framework.
- Add htmx-specific methods to `ohm.Request`.
- Replace `templ` with a custom template language.
- Copy Rails' leading-underscore partial file naming.
- Require separate routes for every fragment.

## Consequences

Ohm gets a stronger server-rendered HTML story while keeping core concepts
portable. htmx becomes a polished default lane for enhanced HTML interactions,
but the same full-response-plus-fragments model can support other adapters or
application-specific negotiation later.

Handlers stay explicit: they choose the full page and the valid fragments, pass
typed data into both, and return through Ohm's response boundary. This preserves
the compile-time feedback and directness from ADR 0006.

Direct navigation remains reliable. The framework does not assume that every
enhanced request wants a fragment, which avoids returning partial HTML during
history restoration or broad boosted navigation.

The framework now owes clear docs and generator support. Generated applications
should demonstrate the pattern with a realistic page and fragment pair, including
validation-error responses for forms and a target-aware htmx request.

Fragment target names become part of an application's public HTML contract.
Renaming a target can break htmx interactions even if Go code still compiles, so
tests should cover request negotiation and rendered target markup.

The extra distinction between pages, partials, and components adds structure.
That structure is useful for medium-sized server-rendered applications, but tiny
applications should still be able to call `Request.HTML` directly without using
the fragment API.

## Alternatives considered

### Keep only pages and components

Ohm could tell applications to use existing components as partials. This avoids a
new concept, but it leaves every handler to decide how htmx headers map to
responses and provides no generator or documentation support for progressive
enhancement.

### Put htmx directly in the core request API

Methods such as `Request.IsHtmx` or `Request.HTMLForHtmx` would be convenient for
the first adapter, but they would make htmx a permanent core framework concern.
Ohm should bless htmx without turning the root package into an htmx package.

### Use separate fragment endpoints

Separate endpoints can be useful for some cases, but making them the default
duplicates route logic and weakens direct navigation. A handler should usually be
able to render either the full page or a valid fragment from the same loaded
data.

## References

- [ADR 0006: Server-rendered views](0006-server-rendered-views.md).
- [htmx documentation](https://htmx.org/docs/).
- [htmx reference](https://htmx.org/reference/).

## Open questions

- What should the concrete core API names be for full responses and named
  fragments?
- Should the htmx adapter live in `github.com/mgomes/ohm/htmx`, or should all
  adapters live under an `adapter` namespace?
- Should generated resource views include htmx attributes by default, or should
  htmx examples live only in documentation until the adapter API settles?
- Should fragment negotiation support only target ids, or also named regions that
  map to selectors?
