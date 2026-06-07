package ohm

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestViewStoresFullComponentAndFragments(t *testing.T) {
	full := testHTMLComponent("full")
	posts := Fragment("posts", testHTMLComponent("posts"))
	comments := Fragment("comments", testHTMLComponent("comments"))

	view := View(full, posts, comments)

	if got := renderTestComponent(t, view.Full()); got != "full" {
		t.Errorf("View(full, fragments...).Full().Render(ctx, w) = %q, want %q", got, "full")
	}

	gotPosts, ok := view.Fragment("posts")
	if !ok {
		t.Fatalf("View(full, fragments...).Fragment(%q) ok = false, want true", "posts")
	}
	if gotPosts.Target() != "posts" {
		t.Errorf("View(full, fragments...).Fragment(%q).Target() = %q, want %q", "posts", gotPosts.Target(), "posts")
	}
	if got := renderTestComponent(t, gotPosts.Component()); got != "posts" {
		t.Errorf("View(full, fragments...).Fragment(%q).Component().Render(ctx, w) = %q, want %q", "posts", got, "posts")
	}

	if _, ok := view.Fragment("missing"); ok {
		t.Errorf("View(full, fragments...).Fragment(%q) ok = true, want false", "missing")
	}

	wantTargets := []string{"posts", "comments"}
	if got := view.Targets(); !slices.Equal(got, wantTargets) {
		t.Errorf("View(full, fragments...).Targets() = %v, want %v", got, wantTargets)
	}
}

func TestViewCopiesFragmentSlices(t *testing.T) {
	fragments := []HTMLFragment{
		Fragment("posts", testHTMLComponent("posts")),
	}

	view := View(testHTMLComponent("full"), fragments...)
	fragments[0] = Fragment("comments", testHTMLComponent("comments"))

	got, ok := view.Fragment("posts")
	if !ok {
		t.Fatalf("View(full, fragments...).Fragment(%q) ok = false, want true", "posts")
	}
	if got.Target() != "posts" {
		t.Errorf("View(full, fragments...).Fragment(%q).Target() = %q, want %q", "posts", got.Target(), "posts")
	}

	gotFragments := view.Fragments()
	gotFragments[0] = Fragment("comments", testHTMLComponent("comments"))
	if _, ok := view.Fragment("comments"); ok {
		t.Errorf("View(full, fragments...).Fragment(%q) ok = true after mutating returned slice, want false", "comments")
	}
}

func TestViewSingleFragment(t *testing.T) {
	fragment := Fragment("posts", testHTMLComponent("posts"))
	view := View(testHTMLComponent("full"), fragment)

	got, ok := view.SingleFragment()
	if !ok {
		t.Fatalf("View(full, fragment).SingleFragment() ok = false, want true")
	}
	if got.Target() != "posts" {
		t.Errorf("View(full, fragment).SingleFragment().Target() = %q, want %q", got.Target(), "posts")
	}

	multi := View(testHTMLComponent("full"), fragment, Fragment("comments", testHTMLComponent("comments")))
	if _, ok := multi.SingleFragment(); ok {
		t.Errorf("View(full, fragments...).SingleFragment() ok = true, want false")
	}
}

func testHTMLComponent(text string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, text)
		return err
	})
}

func renderTestComponent(t *testing.T, component templ.Component) string {
	t.Helper()

	var body strings.Builder
	if err := component.Render(context.Background(), &body); err != nil {
		t.Fatalf("component.Render(ctx, w) error = %v, want nil", err)
	}
	return body.String()
}
