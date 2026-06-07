# HTML fragments and htmx

Ohm supports server-rendered HTML fragments as a first-class view path. Use a
full page for normal browser navigation and named fragments for enhanced
interactions that update one page region.

The core Ohm API is client-neutral:

- `ohm.View` declares the full page and its valid fragments.
- `ohm.Fragment` names a page region and the component that renders it.
- `Request.HTML` still renders any single `templ.Component` directly.

The `github.com/mgomes/ohm/htmx` package is the blessed htmx adapter. It reads
htmx request headers, selects a matching fragment when the request target is
explicit, and keeps full-page responses for normal navigation and history
restoration.

htmx works well with this model because htmx requests normally expect HTML
responses, not JSON, and htmx sends request headers such as `HX-Request`,
`HX-Target`, and `HX-History-Restore-Request`. See the official
[htmx documentation](https://htmx.org/docs/) and
[htmx reference](https://htmx.org/reference/).

## View directories

Generated applications organize server-rendered views like this:

```text
internal/views/layouts/     full document wrappers
internal/views/pages/       route-level full pages
internal/views/partials/    route-addressable fragments
internal/views/components/  smaller reusable view pieces
internal/views/forms/       form helpers
internal/views/assets/      asset path helpers
```

Use `pages` for full screens and `partials` for regions that htmx can swap.
Use `components` for reusable pieces with no route or target meaning.

Do not copy Rails' leading-underscore partial names. Go ignores source files
whose names begin with `_`, so names such as `_form.templ` will not behave like
ordinary Go source. Use names such as `post_form.templ` or `posts_list.templ`.

## Render a full page and a fragment

A page should usually reuse its partial so both response paths share markup.

```templ
// internal/views/pages/home.templ
package pages

import (
	"example.com/journal/internal/views/layouts"
	"example.com/journal/internal/views/partials"
)

templ Home(title string) {
	@layouts.Application(title) {
		@partials.Home(title)
	}
}
```

```templ
// internal/views/partials/home.templ
package partials

templ Home(title string) {
	<section id="home">
		<h1>{ "Welcome to " + title }</h1>
	</section>
}
```

The handler declares both the full page and the fragment. The target name should
match the element id that htmx will target, without a leading `#`.

```go
package handlers

import (
	"net/http"

	"github.com/mgomes/ohm"
	"github.com/mgomes/ohm/htmx"

	"example.com/journal/internal/views/pages"
	"example.com/journal/internal/views/partials"
)

func Home(req *ohm.Request) error {
	title := "Journal"
	return htmx.Render(req, http.StatusOK, ohm.View(
		pages.Home(title),
		ohm.Fragment("home", partials.Home(title)),
	))
}
```

```html
<button hx-get="/" hx-target="#home" hx-swap="outerHTML">
  Refresh
</button>
```

htmx sends `HX-Target: home` for that request, and Ohm renders the fragment
declared as `ohm.Fragment("home", partials.Home(title))`.

## How htmx rendering chooses a response

`htmx.Render` is intentionally conservative:

- Normal browser requests render the full page.
- htmx history-restore requests render the full page.
- htmx requests with a matching `HX-Target` render that target's fragment.
- Targetless htmx requests render the full page by default.
- Unknown htmx targets return a safe `400 Bad Request` application error.

This keeps direct navigation, browser history, and boosted links from receiving
partial HTML by accident.

If a route has exactly one fragment and a targetless htmx request should render
it, opt in explicitly:

```go
return htmx.Render(req, http.StatusOK, ohm.View(
	pages.Home(title),
	ohm.Fragment("home", partials.Home(title)),
), htmx.WithSingleFragmentFallback())
```

Use the fallback sparingly. Prefer explicit htmx targets when the page has more
than one replaceable region or when the route may be used by boosted navigation.

## Multiple fragments

A single route can declare multiple valid fragments from the same loaded data.

```go
func PostsShow(req *ohm.Request) error {
	id := req.Param("id")
	view := loadPostView(req.Context(), id)

	return htmx.Render(req, http.StatusOK, ohm.View(
		pages.PostsShow(view),
		ohm.Fragment("comments", partials.Comments(view.Comments)),
		ohm.Fragment("activity", partials.Activity(view.Activity)),
	))
}
```

An htmx request for target `comments` gets only the comments fragment. A request
for target `activity` gets only the activity fragment. A normal request gets the
full page.

## Forms and validation errors

For forms, keep the full-page and partial paths together. On validation failure,
return the same status for both paths and let `htmx.Render` select the right
component.

```go
func PostsCreate(req *ohm.Request) error {
	var form PostForm
	if err := req.Bind(&form); err != nil {
		view := PostFormView{Form: form, Errors: validationErrors(err)}
		return htmx.Render(req, http.StatusUnprocessableEntity, ohm.View(
			pages.PostsNew(view),
			ohm.Fragment("post-form", partials.PostForm(view)),
		))
	}

	if err := createPost(req.Context(), form); err != nil {
		return err
	}

	htmx.SetRedirect(req.ResponseWriter(), "/posts")
	req.NoContent()
	return nil
}
```

The full-page response preserves non-JavaScript form behavior. The fragment
response lets htmx replace only the form target.

## Response headers

Use the htmx adapter for htmx response headers instead of setting string
headers throughout handlers.

```go
func PostsDelete(req *ohm.Request) error {
	if err := deletePost(req.Context(), req.Param("id")); err != nil {
		return err
	}

	htmx.SetTrigger(req.ResponseWriter(), "posts:deleted")
	req.NoContent()
	return nil
}
```

Available helpers include:

- `htmx.SetLocation`
- `htmx.SetPushURL`
- `htmx.SetRedirect`
- `htmx.SetRefresh`
- `htmx.SetReplaceURL`
- `htmx.SetReselect`
- `htmx.SetReswap`
- `htmx.SetRetarget`
- `htmx.SetTrigger`
- `htmx.SetTriggerAfterSettle`
- `htmx.SetTriggerAfterSwap`

Keep these helpers in handlers. The view should render HTML; the handler owns
HTTP response behavior.

## Testing

Test both the normal path and the htmx target path for routes that support
fragments.

```go
func TestHomeRendersFullPage(t *testing.T) {
	app := ohm.New()
	Register(app)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("Home(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "<!doctype html>") {
		t.Errorf("Home(%s %s) body = %q, want full page", request.Method, request.URL.Path, response.Body.String())
	}
}
```

```go
func TestHomeRendersHTMXFragment(t *testing.T) {
	app := ohm.New()
	Register(app)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(htmx.HeaderRequest, "true")
	request.Header.Set(htmx.HeaderTarget, "home")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("Home(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `<section id="home">`) {
		t.Errorf("Home(%s %s) body = %q, want home fragment", request.Method, request.URL.Path, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "<!doctype html>") {
		t.Errorf("Home(%s %s) body = %q, want fragment without document layout", request.Method, request.URL.Path, response.Body.String())
	}
}
```

Also test unknown targets when a route has multiple fragments. That verifies the
public target contract instead of only checking that the handler returns some
HTML.

## Common mistakes

### Returning fragments for every htmx request

Do not use `HX-Request` alone as a blanket "return partial" switch. History
restoration and boosted navigation can also arrive as htmx requests. Let
`htmx.Render` apply the conservative selection policy.

### Duplicating page and fragment data loading

Do not add a second route that reloads the same data only to render a fragment.
Load the view model once and declare the full page plus its fragments from that
same data.

### Treating components as route targets

Components can be reused anywhere. Partials carry route and target meaning. Put
htmx replaceable regions in `internal/views/partials` so target names stay easy
to find and test.

### Renaming targets casually

Fragment target names are part of the public HTML contract. Renaming `"comments"`
to `"post-comments"` can break existing markup even when the Go compiler is
happy. Update htmx attributes and tests together.
