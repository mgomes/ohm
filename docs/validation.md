# Validations

Ohm validates submitted forms with ordinary Go methods. A form type implements
`Validate(*ohm.Validation)`, handlers run `ohm.Validate`, and views receive the
same structured errors for full-page, fragment, or JSON responses.

Validation is explicit. `req.Decode(&form)` reads the request body. Validation
runs after decode, once the handler has a normal Go value to inspect.

## Define a Form

Keep form tags and validation field names aligned. The field name passed to a
validation helper should match the HTML input name and the `form` tag used by
Ohm's form decoder.

```go
package handlers

import (
	"time"

	"github.com/mgomes/ohm"
)

type PostForm struct {
	Title       string    `form:"title"`
	Body        string    `form:"body"`
	Tags        []string  `form:"tags"`
	PublishedAt time.Time `form:"published_at"`
}

func (f PostForm) Validate(v *ohm.Validation) {
	v.String("title", f.Title).Presence().Length(3, 120)
	v.String("body", f.Body).Presence()
	v.Slice("tags", f.Tags).Min(1)
	v.Time("published_at", f.PublishedAt).Presence()
}
```

Common rules are chainable:

```go
v.String("title", f.Title).Presence().Length(3, 120)
v.Int("count", f.Count).Min(1).Max(20)
v.Bool("accepted_terms", f.AcceptedTerms).Accepted()
v.Time("published_at", f.PublishedAt).Presence()
v.Slice("tags", f.Tags).Min(1)
```

Blank strings are valid unless `Presence` is used. That lets optional text
fields have length rules without also making them required.

## Render Invalid Forms

For server-rendered forms, return `422 Unprocessable Entity` and pass the same
form value plus `ohm.Errors` back to the view.

```go
package handlers

import (
	"net/http"

	"github.com/mgomes/ohm"
	"github.com/mgomes/ohm/htmx"

	"example.com/journal/internal/views/pages"
	"example.com/journal/internal/views/partials"
)

type PostFormView struct {
	Form   PostForm
	Errors ohm.Errors
}

func PostsCreate(req *ohm.Request) error {
	var form PostForm
	if err := req.Decode(&form); err != nil {
		return err
	}

	errs := ohm.Validate(form)
	if errs.Any() {
		view := PostFormView{Form: form, Errors: errs}
		return htmx.Render(req, http.StatusUnprocessableEntity, ohm.View(
			pages.PostsNew(view),
			ohm.Fragment("post-form", partials.PostForm(view)),
		))
	}

	if err := createPost(req.Context(), form); err != nil {
		return err
	}

	req.Redirect(http.StatusSeeOther, "/posts")
	return nil
}
```

Templates can read messages by field name:

```html
<label for="title">Title</label>
<input id="title" name="title" value="{{ .Form.Title }}">
{{ range .Errors.Messages "title" }}
	<p class="field-error">{{ . }}</p>
{{ end }}
```

Generated apps also include an `internal/views/forms` helper. Its field
constructor accepts any value with `Messages(field) []string`, so `ohm.Errors`
can be passed directly.

```go
field := forms.NewField("title", "Title", values, errs)
```

## Custom Rules

Custom validation is just Go. Use conditionals, helper functions, and domain
types, then add structured errors through the same collector.

```go
func (f EventForm) Validate(v *ohm.Validation) {
	v.Time("starts_at", f.StartsAt).Presence()
	v.Time("ends_at", f.EndsAt).Presence()

	if !f.StartsAt.IsZero() && !f.EndsAt.IsZero() && !f.EndsAt.After(f.StartsAt) {
		v.Add("ends_at", "after_start", "must be after the start time")
	}
}
```

Use form-level errors by passing an empty field name:

```go
v.Add("", "unavailable", "this appointment time is no longer available")
```

## Nested Forms

Nested validation uses explicit field prefixes.

```go
type AuthorForm struct {
	Name string `form:"name"`
}

func (f AuthorForm) Validate(v *ohm.Validation) {
	v.String("name", f.Name).Presence()
}

type PostForm struct {
	Author AuthorForm `form:"author"`
}

func (f PostForm) Validate(v *ohm.Validation) {
	v.Nested("author", f.Author)
}
```

An invalid nested author name appears as `author.name`, which matches the form
decoder path for an input named `author.name`.

For literal dots or backslashes in form keys, use the same escaped names that
the form decoder expects in submitted keys, and pass the matching field path to
validation. Ordinary nested form fields should use unescaped dotted paths such
as `author.name`.

## JSON Responses

`ohm.Errors` is structured and JSON-friendly. Use `All` when returning errors
as response data.

```go
errs := ohm.Validate(form)
if errs.Any() {
	req.JSON(http.StatusUnprocessableEntity, map[string]any{
		"errors": errs.All(),
	})
	return nil
}
```

Each error includes:

- `field`: the field path, such as `title` or `author.name`.
- `code`: a stable rule code, such as `required` or `too_short`.
- `message`: the default human-readable message.
- `params`: rule parameters, such as `min`, `max`, `after`, or `before`.

If an API boundary needs an `error`, use `errs.Err()`. It returns `nil` for an
empty collection and returns an error matching `ohm.ErrValidation` when the
collection has failures.

## Keep Services Separate

The core validators are intentionally local to the form value. Rules that need
application state, such as uniqueness, authorization, or foreign-key checks,
belong in a handler or service workflow. Add those failures to the same
collector so views still receive one `ohm.Errors` value.

```go
validation := ohm.NewValidation()
form.Validate(validation)

if posts.TitleTaken(req.Context(), form.Title) {
	validation.Add("title", "taken", "has already been taken")
}

errs := validation.Errors()
```
