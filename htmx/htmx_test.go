package htmx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mgomes/ohm"
	"github.com/mgomes/ohm/htmx"
)

func TestRenderUsesFullViewForNormalRequest(t *testing.T) {
	app := newViewApp(t, ohm.View(
		testComponent("full"),
		ohm.Fragment("posts", testComponent("fragment")),
	))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != "full" {
		t.Errorf("htmx.Render(normal request) body = %q, want %q", got, "full")
	}
}

func TestRenderSetsVaryHeaders(t *testing.T) {
	app := ohm.New()
	app.Get("/", func(req *ohm.Request) error {
		req.ResponseWriter().Header().Set("Vary", "Accept, HX-Request")
		return htmx.Render(req, http.StatusOK, ohm.View(
			testComponent("full"),
			ohm.Fragment("posts", testComponent("fragment")),
		))
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	assertVary(t, response.Header(), "Accept", htmx.HeaderRequest, htmx.HeaderTarget, htmx.HeaderHistoryRestoreRequest)
}

func TestRenderUsesFragmentForMatchingTarget(t *testing.T) {
	app := newViewApp(t, ohm.View(
		testComponent("full"),
		ohm.Fragment("posts", testComponent("fragment")),
	))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(htmx.HeaderRequest, "true")
	request.Header.Set(htmx.HeaderTarget, "posts")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != "fragment" {
		t.Errorf("htmx.Render(targeted request) body = %q, want %q", got, "fragment")
	}
}

func TestRenderUsesFullViewForHistoryRestore(t *testing.T) {
	app := newViewApp(t, ohm.View(
		testComponent("full"),
		ohm.Fragment("posts", testComponent("fragment")),
	))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(htmx.HeaderRequest, "true")
	request.Header.Set(htmx.HeaderHistoryRestoreRequest, "true")
	request.Header.Set(htmx.HeaderTarget, "posts")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != "full" {
		t.Errorf("htmx.Render(history restore request) body = %q, want %q", got, "full")
	}
}

func TestRenderUsesFullViewForTargetlessRequestByDefault(t *testing.T) {
	app := newViewApp(t, ohm.View(
		testComponent("full"),
		ohm.Fragment("posts", testComponent("fragment")),
	))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(htmx.HeaderRequest, "true")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != "full" {
		t.Errorf("htmx.Render(targetless request) body = %q, want %q", got, "full")
	}
}

func TestRenderUsesSingleFragmentFallbackWhenEnabled(t *testing.T) {
	app := newViewApp(t, ohm.View(
		testComponent("full"),
		ohm.Fragment("posts", testComponent("fragment")),
	), htmx.WithSingleFragmentFallback())

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(htmx.HeaderRequest, "true")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != "fragment" {
		t.Errorf("htmx.Render(targetless request, fallback) body = %q, want %q", got, "fragment")
	}
}

func TestRenderRejectsUnknownTarget(t *testing.T) {
	var gotErr error
	app := ohm.New(ohm.WithErrorHandler(func(req *ohm.Request, err error) {
		gotErr = err
		status, message := ohm.ErrorResponse(err)
		req.PlainText(status, message)
	}))
	app.Get("/", func(req *ohm.Request) error {
		return htmx.Render(req, http.StatusOK, ohm.View(
			testComponent("full"),
			ohm.Fragment("posts", testComponent("fragment")),
		))
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(htmx.HeaderRequest, "true")
	request.Header.Set(htmx.HeaderTarget, "comments")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusBadRequest)
	}
	if got := response.Body.String(); got != "unknown htmx target" {
		t.Errorf("htmx.Render(unknown target) body = %q, want public error message", got)
	}
	assertVary(t, response.Header(), htmx.HeaderRequest, htmx.HeaderTarget, htmx.HeaderHistoryRestoreRequest)
	if !errors.Is(gotErr, htmx.ErrUnknownTarget) {
		t.Errorf("htmx.Render(unknown target) error = %v, want %v", gotErr, htmx.ErrUnknownTarget)
	}
	var targetErr *htmx.UnknownTargetError
	if !errors.As(gotErr, &targetErr) {
		t.Fatalf("htmx.Render(unknown target) error = %v, want *htmx.UnknownTargetError", gotErr)
	}
	if targetErr.Target != "comments" {
		t.Errorf("UnknownTargetError.Target = %q, want %q", targetErr.Target, "comments")
	}
	if strings.Join(targetErr.KnownTargets, ",") != "posts" {
		t.Errorf("UnknownTargetError.KnownTargets = %v, want [posts]", targetErr.KnownTargets)
	}
}

func TestParseRequestReadsHTMXHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/posts", nil)
	request.Header.Set(htmx.HeaderRequest, "TRUE")
	request.Header.Set(htmx.HeaderBoosted, "true")
	request.Header.Set(htmx.HeaderHistoryRestoreRequest, "true")
	request.Header.Set(htmx.HeaderCurrentURL, "https://example.test/posts")
	request.Header.Set(htmx.HeaderPrompt, "accepted")
	request.Header.Set(htmx.HeaderTarget, "posts")
	request.Header.Set(htmx.HeaderTrigger, "submit")
	request.Header.Set(htmx.HeaderTriggerName, "save")

	got := htmx.ParseRequest(request)

	if !got.IsRequest() {
		t.Errorf("ParseRequest(request).IsRequest() = false, want true")
	}
	if !got.IsBoosted() {
		t.Errorf("ParseRequest(request).IsBoosted() = false, want true")
	}
	if !got.IsHistoryRestore() {
		t.Errorf("ParseRequest(request).IsHistoryRestore() = false, want true")
	}
	if got.CurrentURL() != "https://example.test/posts" {
		t.Errorf("ParseRequest(request).CurrentURL() = %q, want %q", got.CurrentURL(), "https://example.test/posts")
	}
	if got.Prompt() != "accepted" {
		t.Errorf("ParseRequest(request).Prompt() = %q, want %q", got.Prompt(), "accepted")
	}
	if got.Target() != "posts" {
		t.Errorf("ParseRequest(request).Target() = %q, want %q", got.Target(), "posts")
	}
	if got.Trigger() != "submit" {
		t.Errorf("ParseRequest(request).Trigger() = %q, want %q", got.Trigger(), "submit")
	}
	if got.TriggerName() != "save" {
		t.Errorf("ParseRequest(request).TriggerName() = %q, want %q", got.TriggerName(), "save")
	}
}

func TestResponseHeaderHelpers(t *testing.T) {
	response := httptest.NewRecorder()

	htmx.SetLocation(response, "/posts/1")
	htmx.SetPushURL(response, "/posts")
	htmx.SetRedirect(response, "/login")
	htmx.SetRefresh(response)
	htmx.SetReplaceURL(response, "/posts/2")
	htmx.SetReselect(response, "#posts")
	htmx.SetReswap(response, "outerHTML")
	htmx.SetRetarget(response, "#flash")
	htmx.SetTrigger(response, "posts:saved")
	htmx.SetTriggerAfterSettle(response, "posts:settled")
	htmx.SetTriggerAfterSwap(response, "posts:swapped")

	tests := []struct {
		header string
		want   string
	}{
		{header: htmx.HeaderLocation, want: "/posts/1"},
		{header: htmx.HeaderPushURL, want: "/posts"},
		{header: htmx.HeaderRedirect, want: "/login"},
		{header: htmx.HeaderRefresh, want: "true"},
		{header: htmx.HeaderReplaceURL, want: "/posts/2"},
		{header: htmx.HeaderReselect, want: "#posts"},
		{header: htmx.HeaderReswap, want: "outerHTML"},
		{header: htmx.HeaderRetarget, want: "#flash"},
		{header: htmx.HeaderTrigger, want: "posts:saved"},
		{header: htmx.HeaderTriggerAfterSettle, want: "posts:settled"},
		{header: htmx.HeaderTriggerAfterSwap, want: "posts:swapped"},
	}
	for _, tt := range tests {
		if got := response.Header().Get(tt.header); got != tt.want {
			t.Errorf("response header %s = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func newViewApp(t *testing.T, view ohm.HTMLView, opts ...htmx.Option) *ohm.App {
	t.Helper()

	app := ohm.New()
	app.Get("/", func(req *ohm.Request) error {
		return htmx.Render(req, http.StatusOK, view, opts...)
	})
	return app
}

func testComponent(text string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, text)
		return err
	})
}

func assertVary(t *testing.T, header http.Header, wants ...string) {
	t.Helper()

	counts := make(map[string]int)
	for _, value := range header.Values("Vary") {
		for part := range strings.SplitSeq(value, ",") {
			name := http.CanonicalHeaderKey(strings.TrimSpace(part))
			if name == "" {
				continue
			}
			counts[name]++
		}
	}

	for _, want := range wants {
		name := http.CanonicalHeaderKey(want)
		if counts[name] != 1 {
			t.Errorf("Vary header count for %s = %d, want 1 in %v", name, counts[name], header.Values("Vary"))
		}
	}
}
