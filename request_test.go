package ohm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRequestBindDecodesJSONAndRunsBinders(t *testing.T) {
	app := New()
	app.Post("/login", func(req *Request) error {
		var payload bindPayload
		if err := req.Bind(&payload); err != nil {
			return err
		}
		req.JSON(http.StatusOK, payload)
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"name":"ada","child":{"value":"nested"}}`))
	request.Header.Set("Content-Type", "application/json")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}

	var got bindPayload
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got.Name != "ada" {
		t.Errorf("Request.Bind(payload).Name = %q, want %q", got.Name, "ada")
	}
	if !got.Bound {
		t.Errorf("Request.Bind(payload).Bound = false, want true")
	}
	if got.Child == nil {
		t.Fatalf("Request.Bind(payload).Child = nil, want child")
	}
	if got.Child.Value != "nested" {
		t.Errorf("Request.Bind(payload).Child.Value = %q, want %q", got.Child.Value, "nested")
	}
	if !got.Child.Bound {
		t.Errorf("Request.Bind(payload).Child.Bound = false, want true")
	}
}

func TestRequestDecodeDecodesForm(t *testing.T) {
	app := New()
	app.Post("/posts", func(req *Request) error {
		var payload formPayload
		if err := req.Decode(&payload); err != nil {
			return err
		}
		req.JSON(http.StatusOK, payload)
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(strings.Join([]string{
		"title=hello",
		"published=on",
		"count=12",
		"tags=go",
		"tags=html",
		"page=3",
		"author.name=ada",
		"prefs.theme=dark",
		"prefs.locale=en-US",
		"published_at=2026-06-07T12:30",
		"time_only=12:30:15.123",
	}, "&")))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}

	var got formPayload
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got.Title != "hello" {
		t.Errorf("Request.Decode(form).Title = %q, want %q", got.Title, "hello")
	}
	if !got.Published {
		t.Errorf("Request.Decode(form).Published = false, want true")
	}
	if got.Count != 12 {
		t.Errorf("Request.Decode(form).Count = %d, want %d", got.Count, 12)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "go" || got.Tags[1] != "html" {
		t.Errorf("Request.Decode(form).Tags = %#v, want %#v", got.Tags, []string{"go", "html"})
	}
	if got.Page == nil {
		t.Fatalf("Request.Decode(form).Page = nil, want pointer")
	}
	if *got.Page != 3 {
		t.Errorf("Request.Decode(form).Page = %d, want %d", *got.Page, 3)
	}
	if got.Author.Name != "ada" {
		t.Errorf("Request.Decode(form).Author.Name = %q, want %q", got.Author.Name, "ada")
	}
	if got.Preferences["theme"] != "dark" {
		t.Errorf("Request.Decode(form).Preferences[theme] = %q, want %q", got.Preferences["theme"], "dark")
	}
	if got.Preferences["locale"] != "en-US" {
		t.Errorf("Request.Decode(form).Preferences[locale] = %q, want %q", got.Preferences["locale"], "en-US")
	}
	wantPublishedAt := time.Date(2026, 6, 7, 12, 30, 0, 0, time.UTC)
	if !got.PublishedAt.Equal(wantPublishedAt) {
		t.Errorf("Request.Decode(form).PublishedAt = %v, want %v", got.PublishedAt, wantPublishedAt)
	}
	wantTimeOnly := time.Date(0, 1, 1, 12, 30, 15, 123000000, time.UTC)
	if !got.TimeOnly.Equal(wantTimeOnly) {
		t.Errorf("Request.Decode(form).TimeOnly = %v, want %v", got.TimeOnly, wantTimeOnly)
	}
	if got.Ignored != "" {
		t.Errorf("Request.Decode(form).Ignored = %q, want empty", got.Ignored)
	}
}

func TestRequestDecodeReadsFormBodyForAnyMethod(t *testing.T) {
	app := New()
	app.Delete("/posts/1", func(req *Request) error {
		var payload formPayload
		if err := req.Decode(&payload); err != nil {
			return err
		}
		req.JSON(http.StatusOK, payload)
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/posts/1", strings.NewReader("title=removed"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}

	var got formPayload
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got.Title != "removed" {
		t.Errorf("Request.Decode(form).Title = %q, want %q", got.Title, "removed")
	}
}

func TestRequestDecodeRejectsOversizedFormBodyAsClientError(t *testing.T) {
	app := New()
	app.Post("/posts", func(req *Request) error {
		var payload formPayload
		return req.Decode(&payload)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(strings.Repeat("a", int(maxFormBodyBytes)+1)))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusRequestEntityTooLarge)
	}
	if got := response.Body.String(); got != http.StatusText(http.StatusRequestEntityTooLarge) {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, got, http.StatusText(http.StatusRequestEntityTooLarge))
	}
}

func TestRequestRenderRunsNestedRenderersAndUsesStatus(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		return req.Render(&renderPayload{Child: &renderChild{}})
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusAccepted)
	}

	var got renderPayload
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got.Message != "parent" {
		t.Errorf("Request.Render(payload).Message = %q, want %q", got.Message, "parent")
	}
	if got.Child == nil {
		t.Fatalf("Request.Render(payload).Child = nil, want child")
	}
	if got.Child.Message != "child" {
		t.Errorf("Request.Render(payload).Child.Message = %q, want %q", got.Child.Message, "child")
	}
}

func TestSetStatusDoesNotReplaceRequestContext(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		raw := req.HTTPRequest()
		ctx := raw.Context()
		SetStatus(raw, http.StatusAccepted)
		if raw.Context() != ctx {
			t.Errorf("SetStatus replaced request context, want stable context")
		}
		return req.Render(&renderPayload{Child: &renderChild{}})
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusAccepted)
	}
}

func TestSetStatusFromMiddlewareAppliesToRender(t *testing.T) {
	app := New()
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			SetStatus(r, http.StatusAccepted)
			next.ServeHTTP(w, r)
		})
	})
	app.Get("/render", func(req *Request) error {
		return req.Render(&renderChild{})
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusAccepted)
	}
}

func TestSetStatusFromOuterMiddlewareAppliesToMountedAppRender(t *testing.T) {
	inner := New()
	inner.Get("/render", func(req *Request) error {
		return req.Render(&statusPayload{})
	})

	outer := New()
	outer.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			SetStatus(r, http.StatusAccepted)
			next.ServeHTTP(w, r)
		})
	})
	outer.GetHTTP("/render", inner.HTTPHandler())

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render", nil)

	outer.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("mounted App.HTTPHandler().ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusAccepted)
	}
}

func TestSetStatusBeforeHTTPHandlerAppliesToRender(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		return req.Render(&statusPayload{})
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetStatus(r, http.StatusCreated)
		app.HTTPHandler().ServeHTTP(w, r)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("wrapped App.HTTPHandler().ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusCreated)
	}
	if _, ok := pendingResponseStatusByRequest.Load(request); ok {
		t.Fatalf("pending response status request entry leaked after handler returned")
	}
}

func TestSetStatusBeforeHTTPHandlerSurvivesRequestContextCopy(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		return req.Render(&statusPayload{})
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetStatus(r, http.StatusCreated)
		ctx := context.WithValue(r.Context(), statusContextKey{}, "copied")
		app.HTTPHandler().ServeHTTP(w, r.WithContext(ctx))
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render", nil)
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	request = request.WithContext(ctx)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("wrapped App.HTTPHandler().ServeHTTP(%s %s with copied context) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusCreated)
	}
	if _, ok := pendingResponseStatusByRequest.Load(request); ok {
		t.Fatalf("pending response status request entry leaked after handler returned")
	}
	if _, ok := pendingResponseStatusBySharedKey.Load(pendingResponseStatusSharedKeyFor(request)); ok {
		t.Fatalf("pending response status shared entry leaked after handler returned")
	}
}

func TestSetStatusBeforeHTTPHandlerSurvivesBackgroundRequestContextCopy(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		return req.Render(&statusPayload{})
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetStatus(r, http.StatusCreated)
		ctx := context.WithValue(r.Context(), statusContextKey{}, "copied")
		app.HTTPHandler().ServeHTTP(w, r.WithContext(ctx))
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render", nil)
	request.Header = nil
	if request.Context().Done() != nil {
		t.Fatalf("httptest.NewRequest(%s, %q, nil).Context().Done() is non-nil, want nil", http.MethodGet, "/render")
	}
	sharedKey := pendingResponseStatusSharedKeyFor(request)
	if sharedKey.isZero() {
		t.Fatalf("pendingResponseStatusSharedKeyFor(request) = 0, want non-zero")
	}

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("wrapped App.HTTPHandler().ServeHTTP(%s %s with copied background context) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusCreated)
	}
	if _, ok := pendingResponseStatusByRequest.Load(request); ok {
		t.Fatalf("pending response status request entry leaked after handler returned")
	}
	if _, ok := pendingResponseStatusBySharedKey.Load(sharedKey); ok {
		t.Fatalf("pending response status shared entry leaked after handler returned")
	}
}

func TestSetStatusBeforeHTTPHandlerSurvivesRequestClone(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		return req.Render(&statusPayload{})
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetStatus(r, http.StatusCreated)
		app.HTTPHandler().ServeHTTP(w, r.Clone(r.Context()))
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("wrapped App.HTTPHandler().ServeHTTP(%s %s cloned request) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusCreated)
	}
	if _, ok := pendingResponseStatusByRequest.Load(request); ok {
		t.Fatalf("pending response status request entry leaked after cloned request returned")
	}
	for _, key := range pendingResponseStatusCloneKeysFor(request) {
		if _, ok := pendingResponseStatusByCloneKey.Load(key); ok {
			t.Fatalf("pending response status clone entry leaked after cloned request returned")
		}
	}
}

func TestSetStatusBeforeHTTPHandlerSurvivesRewrittenRequestClone(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		return req.Render(&statusPayload{})
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetStatus(r, http.StatusCreated)
		http.StripPrefix("/prefix", app.HTTPHandler()).ServeHTTP(w, r)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/prefix/render", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("wrapped App.HTTPHandler().ServeHTTP(%s %s stripped request) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusCreated)
	}
	for _, key := range pendingResponseStatusCloneKeysFor(request) {
		if _, ok := pendingResponseStatusByCloneKey.Load(key); ok {
			t.Fatalf("pending response status clone entry leaked after stripped request returned")
		}
	}
}

func TestSetStatusBeforeHTTPHandlerClearsAmbiguousRequestCloneStatuses(t *testing.T) {
	first := httptest.NewRequest(http.MethodGet, "/render", nil)
	second := httptest.NewRequest(http.MethodGet, "/render", nil)

	SetStatus(first, http.StatusCreated)
	SetStatus(second, http.StatusAccepted)

	if status, ok := takePendingResponseStatus(first.Clone(first.Context())); ok {
		t.Fatalf("takePendingResponseStatus(cloned ambiguous request) = (%d, true), want no status", status)
	}
	if _, ok := pendingResponseStatusByRequest.Load(first); ok {
		t.Fatalf("pending response status request entry leaked for first ambiguous request")
	}
	if _, ok := pendingResponseStatusByRequest.Load(second); ok {
		t.Fatalf("pending response status request entry leaked for second ambiguous request")
	}
	for _, key := range pendingResponseStatusCloneKeysFor(first) {
		if _, ok := pendingResponseStatusByCloneKey.Load(key); ok {
			t.Fatalf("pending response status clone entry leaked for ambiguous requests")
		}
	}
}

func TestSetStatusWithCanceledContextCleansPendingResponseStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/render", nil).WithContext(ctx)
	sharedKey := pendingResponseStatusSharedKeyFor(request)
	cloneKeys := pendingResponseStatusCloneKeysFor(request)

	SetStatus(request, http.StatusCreated)

	deadline := time.Now().Add(time.Second)
	for {
		_, requestPending := pendingResponseStatusByRequest.Load(request)
		_, sharedPending := pendingResponseStatusBySharedKey.Load(sharedKey)
		clonePending := false
		for _, key := range cloneKeys {
			if _, ok := pendingResponseStatusByCloneKey.Load(key); ok {
				clonePending = true
				break
			}
		}
		if !requestPending && !sharedPending && !clonePending {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending response status entries were not cleaned after canceled context")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSetStatusBeforeHTTPHandlerSeparatesSharedCancellableContextRequests(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		return req.Render(&statusPayload{})
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := httptest.NewRequest(http.MethodGet, "/render?request=first", nil).WithContext(ctx)
	second := httptest.NewRequest(http.MethodGet, "/render?request=second", nil).WithContext(ctx)

	SetStatus(first, http.StatusCreated)
	SetStatus(second, http.StatusAccepted)

	firstResponse := httptest.NewRecorder()
	firstCtx := context.WithValue(first.Context(), statusContextKey{}, "first")
	app.HTTPHandler().ServeHTTP(firstResponse, first.WithContext(firstCtx))
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("App.HTTPHandler().ServeHTTP(%s %s with shared context) status = %d, want %d", first.Method, first.URL.String(), firstResponse.Code, http.StatusCreated)
	}

	secondResponse := httptest.NewRecorder()
	secondCtx := context.WithValue(second.Context(), statusContextKey{}, "second")
	app.HTTPHandler().ServeHTTP(secondResponse, second.WithContext(secondCtx))
	if secondResponse.Code != http.StatusAccepted {
		t.Fatalf("App.HTTPHandler().ServeHTTP(%s %s with shared context) status = %d, want %d", second.Method, second.URL.String(), secondResponse.Code, http.StatusAccepted)
	}
	if _, ok := pendingResponseStatusByRequest.Load(first); ok {
		t.Fatalf("pending response status request entry leaked for first request")
	}
	if _, ok := pendingResponseStatusByRequest.Load(second); ok {
		t.Fatalf("pending response status request entry leaked for second request")
	}
	if _, ok := pendingResponseStatusBySharedKey.Load(pendingResponseStatusSharedKeyFor(first)); ok {
		t.Fatalf("pending response status shared entry leaked for first request")
	}
	if _, ok := pendingResponseStatusBySharedKey.Load(pendingResponseStatusSharedKeyFor(second)); ok {
		t.Fatalf("pending response status shared entry leaked for second request")
	}
}

func TestSetStatusBeforeHTTPHandlerSeparatesSharedCancellableClonedRequests(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		return req.Render(&statusPayload{})
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := httptest.NewRequest(http.MethodGet, "/render?request=first", nil).WithContext(ctx)
	second := httptest.NewRequest(http.MethodGet, "/render?request=second", nil).WithContext(ctx)

	SetStatus(first, http.StatusCreated)
	SetStatus(second, http.StatusAccepted)

	firstResponse := httptest.NewRecorder()
	firstCtx := context.WithValue(first.Context(), statusContextKey{}, "first")
	app.HTTPHandler().ServeHTTP(firstResponse, first.Clone(firstCtx))
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("App.HTTPHandler().ServeHTTP(%s %s cloned with shared context) status = %d, want %d", first.Method, first.URL.String(), firstResponse.Code, http.StatusCreated)
	}

	secondResponse := httptest.NewRecorder()
	secondCtx := context.WithValue(second.Context(), statusContextKey{}, "second")
	app.HTTPHandler().ServeHTTP(secondResponse, second.Clone(secondCtx))
	if secondResponse.Code != http.StatusAccepted {
		t.Fatalf("App.HTTPHandler().ServeHTTP(%s %s cloned with shared context) status = %d, want %d", second.Method, second.URL.String(), secondResponse.Code, http.StatusAccepted)
	}
	for _, key := range pendingResponseStatusCloneKeysFor(first) {
		if _, ok := pendingResponseStatusByCloneKey.Load(key); ok {
			t.Fatalf("pending response status clone entry leaked for first request")
		}
	}
	for _, key := range pendingResponseStatusCloneKeysFor(second) {
		if _, ok := pendingResponseStatusByCloneKey.Load(key); ok {
			t.Fatalf("pending response status clone entry leaked for second request")
		}
	}
}

func TestSetStatusFromInternalSubrequestDoesNotAffectOuterRender(t *testing.T) {
	app := New()
	app.Get("/inner", func(req *Request) error {
		SetStatus(req.HTTPRequest(), http.StatusCreated)
		return req.Render(&statusPayload{})
	})
	app.Get("/outer", func(req *Request) error {
		innerRequest := req.HTTPRequest().Clone(req.HTTPRequest().Context())
		innerRequest.URL.Path = "/inner"
		innerRequest.RequestURI = "/inner"
		innerResponse := httptest.NewRecorder()

		app.ServeHTTP(innerResponse, innerRequest)
		if innerResponse.Code != http.StatusCreated {
			t.Errorf("App.ServeHTTP(%s %s cloned from outer request) status = %d, want %d", innerRequest.Method, innerRequest.URL.Path, innerResponse.Code, http.StatusCreated)
		}

		return req.Render(&statusPayload{})
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/outer", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
}

func TestSetStatusDoesNotRaceWithRequestContextReaders(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		raw := req.HTTPRequest()
		done := make(chan struct{})
		ready := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			close(ready)
			for {
				select {
				case <-done:
					return
				default:
					_ = raw.Context()
				}
			}
		})
		<-ready

		for range 1000 {
			SetStatus(raw, http.StatusAccepted)
		}
		close(done)
		wg.Wait()

		return req.Render(&renderPayload{Child: &renderChild{}})
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusAccepted)
	}
}

func TestCachedImplementingFieldIndexesCachesExportedImplementers(t *testing.T) {
	type payload struct {
		Name   string
		Child  *renderChild
		Nested Renderer
		hidden *renderChild
	}

	var cache sync.Map
	typ := reflect.TypeOf(payload{})

	got := cachedImplementingFieldIndexes(&cache, typ, rendererType)
	want := []int{1, 2}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cachedImplementingFieldIndexes(cache, %v, rendererType) = %v, want %v", typ, got, want)
	}
	key := implementingFieldIndexCacheKey{typ: typ, iface: rendererType}
	if _, ok := cache.Load(key); !ok {
		t.Fatalf("cachedImplementingFieldIndexes(cache, %v, rendererType) cache hit = false, want true", typ)
	}

	got = cachedImplementingFieldIndexes(&cache, typ, rendererType)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cachedImplementingFieldIndexes(cache, %v, rendererType) cached = %v, want %v", typ, got, want)
	}
}

func TestAcceptedContentTypeUsesFirstAcceptFieldWithoutAllocating(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/render", nil)
	request.Header.Set("Accept", "application/xml; charset=utf-8, application/json")

	if got := acceptedContentType(request); got != contentTypeXML {
		t.Fatalf("acceptedContentType(request) = %v, want %v", got, contentTypeXML)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		_ = acceptedContentType(request)
	})
	if allocs != 0 {
		t.Errorf("acceptedContentType(request) allocations = %v, want 0", allocs)
	}
}

func TestParseContentTypeUsesMediaTypeBeforeParameters(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want contentType
	}{
		{name: "json parameters", raw: "APPLICATION/JSON; charset=utf-8", want: contentTypeJSON},
		{name: "xml malformed parameter", raw: "application/xml; charset", want: contentTypeXML},
		{name: "xhtml parameters", raw: "application/xhtml+xml; profile=compact", want: contentTypeHTML},
		{name: "form parameters", raw: "application/x-www-form-urlencoded; charset=UTF-8", want: contentTypeForm},
		{name: "unicode folded json", raw: "application/j\u017fon", want: contentTypeUnknown},
		{name: "unicode folded javascript", raw: "text/java\u017fcript", want: contentTypeUnknown},
		{name: "unknown", raw: "application/octet-stream; charset=utf-8", want: contentTypeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseContentType(tt.raw); got != tt.want {
				t.Errorf("parseContentType(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestRouteHelpersExposeMatchedRoute(t *testing.T) {
	app := New()
	app.Get("/posts/{id}", func(req *Request) error {
		req.JSON(http.StatusOK, map[string]any{
			"param":   RouteParam(req.HTTPRequest(), "id"),
			"pattern": RoutePattern(req.HTTPRequest()),
			"params":  RouteParams(req.HTTPRequest()),
		})
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/posts/42", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}

	var got struct {
		Param   string            `json:"param"`
		Pattern string            `json:"pattern"`
		Params  map[string]string `json:"params"`
	}
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got.Param != "42" {
		t.Errorf("RouteParam(request, id) = %q, want %q", got.Param, "42")
	}
	if got.Pattern != "/posts/{id}" {
		t.Errorf("RoutePattern(request) = %q, want %q", got.Pattern, "/posts/{id}")
	}
	if got.Params["id"] != "42" {
		t.Errorf("RouteParams(request)[id] = %q, want %q", got.Params["id"], "42")
	}
}

func TestRequestJSONDoesNotExposeMarshalErrors(t *testing.T) {
	app := New()
	app.Get("/bad-json", func(req *Request) error {
		req.JSON(http.StatusOK, map[string]any{"bad": func() {}})
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/bad-json", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusInternalServerError)
	}
	if got := response.Body.String(); got != http.StatusText(http.StatusInternalServerError)+"\n" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want safe internal server error", request.Method, request.URL.Path, got)
	}
	if strings.Contains(response.Body.String(), "unsupported type") {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want no marshal internals", request.Method, request.URL.Path, response.Body.String())
	}
}

func TestRequestRenderXMLDoesNotExposeMarshalErrors(t *testing.T) {
	app := New()
	app.Get("/bad-xml", func(req *Request) error {
		return req.Render(xmlMarshalErrorPayload{Bad: func() {}})
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/bad-xml", nil)
	request.Header.Set("Accept", "application/xml")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusInternalServerError)
	}
	if got := response.Body.String(); got != http.StatusText(http.StatusInternalServerError)+"\n" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want safe internal server error", request.Method, request.URL.Path, got)
	}
	if strings.Contains(response.Body.String(), "unsupported type") {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want no marshal internals", request.Method, request.URL.Path, response.Body.String())
	}
}

type bindPayload struct {
	Name  string     `json:"name"`
	Bound bool       `json:"bound"`
	Child *bindChild `json:"child,omitempty"`
}

func (p *bindPayload) Bind(*http.Request) error {
	p.Bound = true
	return nil
}

type bindChild struct {
	Value string `json:"value"`
	Bound bool   `json:"bound"`
}

func (c *bindChild) Bind(*http.Request) error {
	c.Bound = true
	return nil
}

type formPayload struct {
	Title       string            `form:"title" json:"title"`
	Published   bool              `form:"published" json:"published"`
	Count       int               `form:"count" json:"count"`
	Tags        []string          `form:"tags" json:"tags"`
	Page        *int              `form:"page" json:"page"`
	Author      author            `form:"author" json:"author"`
	Preferences map[string]string `form:"prefs" json:"prefs"`
	PublishedAt time.Time         `form:"published_at" json:"published_at"`
	TimeOnly    time.Time         `form:"time_only" json:"time_only"`
	Ignored     string            `form:"-" json:"ignored"`
}

type author struct {
	Name string `form:"name" json:"name"`
}

type renderPayload struct {
	Message string       `json:"message"`
	Child   *renderChild `json:"child,omitempty"`
}

func (p *renderPayload) Render(_ http.ResponseWriter, r *http.Request) error {
	SetStatus(r, http.StatusAccepted)
	p.Message = "parent"
	return nil
}

type renderChild struct {
	Message string `json:"message"`
}

func (c *renderChild) Render(http.ResponseWriter, *http.Request) error {
	c.Message = "child"
	return nil
}

type statusContextKey struct{}

type statusPayload struct {
	Message string `json:"message"`
}

func (p *statusPayload) Render(http.ResponseWriter, *http.Request) error {
	p.Message = "status"
	return nil
}

type xmlMarshalErrorPayload struct {
	Bad func()
}

func (p xmlMarshalErrorPayload) Render(http.ResponseWriter, *http.Request) error {
	return nil
}
