# ADR 0013: Add Go-native form validations

## Status

Accepted on 2026-06-07

## Context

Ohm now owns request form decoding for `application/x-www-form-urlencoded`
payloads. That gives the framework enough of the HTML form lifecycle to consider
owning validation as well: decode a submitted form, validate it, and render a
server-side HTML response with field errors when the submission is invalid.

Rails-style validation declarations such as `validates :title, presence: true`
are a useful benchmark because they make common form rules easy to see near the
form object. Ohm should provide that same locality and completeness without
copying Ruby's runtime model into Go.

The validation API needs to support server-rendered HTML applications first.
Handlers should be able to return `422 Unprocessable Entity` with the submitted
form values and structured field errors, including htmx fragment responses from
ADR 0011. The same validation errors should also be usable for JSON responses
when an application exposes API endpoints.

Ohm also exists alongside Vibescript, which could eventually provide a concise
declarative authoring syntax. That should influence the shape of the Go API, but
Ohm should not depend on Vibescript at runtime and plain Go applications should
not feel like second-class users.

## Decision

Ohm will add a Go-native validation runtime centered on a `Validate` method and
structured validation errors. Vibescript may later generate Go that uses this
runtime, but Vibescript will not be part of Ohm's validation runtime contract.

The core validation protocol will be explicit Go:

```go
type PostForm struct {
	Title string `form:"title"`
	Body  string `form:"body"`
}

func (f PostForm) Validate(v *ohm.Validation) {
	v.String("title", f.Title).Presence().Length(3, 120)
	v.String("body", f.Body).Presence()
}
```

Handlers validate after decoding:

```go
var form PostForm
if err := req.Decode(&form); err != nil {
	return err
}

errors := ohm.Validate(form)
if errors.Any() {
	return htmx.Render(req, http.StatusUnprocessableEntity, ohm.Fragment(
		"form",
		views.PostForm(form, errors),
	))
}
```

The root package will define a validation interface:

```go
type Validatable interface {
	Validate(*Validation)
}
```

`ohm.Validate` will accept values that implement this interface and return an
`Errors` collection. `Errors` will be structured data, not just display strings.
Each error will carry at least:

- The field path, such as `title`, `author.name`, or `tags.0`.
- A stable code, such as `required`, `too_short`, `too_long`, or `invalid`.
- A human-oriented default message.
- Typed or stringified parameters for message rendering, such as `min` and
  `max`.

Field paths should align with Ohm's form-decoding path conventions so HTML input
names, submitted values, and validation errors can be joined without adapter
logic in every application. Literal dots and backslashes in field names remain
escaped in submitted form keys; validation APIs should expose predictable field
paths for ordinary form names and should document how escaped names are matched.

The validation builder should expose typed field helpers for common rules:

```go
v.String("title", f.Title).Presence().Length(3, 120)
v.Int("count", f.Count).Min(1)
v.Bool("published", f.Published)
v.Time("published_at", f.PublishedAt).Presence()
v.Slice("tags", f.Tags).Min(1)
```

It should also expose a low-level way to add custom field and form-level errors
without requiring closures for common validation rules:

```go
if f.End.Before(f.Start) {
	v.Add("end", "after_start", "must be after the start time")
}
```

Custom validation is not an escape hatch outside the framework model. It is a
normal part of the model because `Validate` is a normal Go method. Applications
can use ordinary conditionals, helper functions, service results, and
domain-specific types inside that method, then report failures through the same
structured `Validation` value as built-in rules.

Nested forms should compose through explicit field prefixes:

```go
func (f PostForm) Validate(v *ohm.Validation) {
	v.Nested("author", f.Author)
}
```

The validation package should remain independent from database access. Rules
that require application state, such as uniqueness, authorization, or checking a
foreign key, belong in handler or service workflows and can add explicit errors
to the same `Validation` value.

Vibescript integration is a future authoring option, not part of this decision.
If Vibescript grows validation syntax, it should generate ordinary Go structs
and `Validate(*ohm.Validation)` methods:

```text
form PostForm {
  title string form:"title"
  body  string form:"body"

  validates title, presence: true, length: { min: 3, max: 120 }
  validates body, presence: true
}
```

The generated Go must be indistinguishable from handwritten Go at the Ohm
runtime boundary.

## Non-goals

- Add an ORM-style model validation layer.
- Make validation run implicitly as part of every decode operation.
- Make struct tags the primary validation API.
- Define Vibescript validation syntax in this ADR.
- Add client-side JavaScript validation.
- Add database-backed rules, such as uniqueness, to the core validation runtime.

## Consequences

Common form validation becomes a first-class Ohm workflow. Applications can keep
form definitions, form tags, and validation rules close together while staying in
ordinary Go. Server-rendered HTML handlers get a consistent path for invalid
submissions and can use the same `Errors` value in full-page and fragment
responses.

The API remains generator-friendly. Vibescript, the Ohm CLI, or another tool can
generate `Validate` methods later without changing the runtime contract or
making Ohm depend on a source language.

Structured errors make views and APIs easier to build. Templates can ask whether
`title` has an error, show the default message, or render errors by stable code.
JSON responses can return the same field and code information without parsing
display text.

Custom rules stay boring. Instead of learning callback hooks or a validation tag
language for unusual cases, application authors write ordinary Go in
`Validate`. The framework only standardizes how errors are collected and exposed
after that logic runs.

The tradeoff is more Go verbosity than Rails. A handwritten `Validate` method is
clear and type-checkable, but it is not as compact as `validates :title,
presence: true`. That verbosity is acceptable for the runtime because generated
syntax can be layered on top later.

Explicit field names can drift from struct tags. The framework will need clear
examples and tests that show how form tags, validation field paths, and template
field names stay aligned. If this becomes too repetitive in real applications,
the generator should reduce repetition rather than moving validation into
stringly typed struct tags.

Ohm will owe good default messages and a path for overriding them. The first
version can use simple English messages, but the error shape should not block
later customization or localization.

## Alternatives considered

### Use validation struct tags

Tags such as `validate:"presence,length:min=3,max=120"` are compact, but they
become a small string language. They are hard to type-check, awkward for
cross-field rules, and likely to grow special cases for custom messages,
conditional rules, and database-backed validation.

### Make Vibescript the validation source of truth

Vibescript could provide the cleanest Rails-like authoring experience, but
making it the first validation path would make plain Go users wait for a
compiler integration and would blur Ohm's runtime contract. Ohm should define
the Go runtime first and let Vibescript generate into it later.

### Validate automatically during request binding

`Request.Bind` could decode and validate in one call, but that hides control
flow from handlers. Applications often need to normalize data, load related
records, or add service-level errors after decoding. Keeping validation explicit
makes that sequencing visible.

### Adopt an external validation library

An external package could reduce implementation work, but Ohm's validation
surface needs to align with form decoding, server-rendered HTML, htmx fragments,
and generated applications. Owning the small runtime keeps the public contract
coherent and avoids exposing another package's validation model as framework API.

## References

- [ADR 0001: Framework foundation](0001-framework-foundation.md).
- [ADR 0011: HTML fragments and htmx adapter](0011-html-fragments-and-htmx-adapter.md).
- [ADR 0012: Use html/template for default views](0012-html-template-default-views.md).
- [HTML fragments and htmx guide](../html-fragments.md).
