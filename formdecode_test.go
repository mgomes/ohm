package ohm

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type formToken string

func (t *formToken) UnmarshalText(text []byte) error {
	*t = formToken("decoded:" + string(text))
	return nil
}

func TestDecodeFormDecodesTopLevelStringMap(t *testing.T) {
	var payload map[string]string
	values := url.Values{
		"title":      []string{"first", "last"},
		"empty":      []string{},
		`foo\.bar`:   []string{"dot"},
		`path\\name`: []string{"slash"},
	}
	err := decodeFormValues(values, &payload)
	if err != nil {
		t.Fatalf("decodeFormValues(%v, map[string]string) error = %v, want nil", values, err)
	}
	if payload["title"] != "last" {
		t.Errorf("decodeFormValues(%v, map[string]string)[title] = %q, want %q", values, payload["title"], "last")
	}
	if payload["empty"] != "" {
		t.Errorf("decodeFormValues(%v, map[string]string)[empty] = %q, want empty", values, payload["empty"])
	}
	if payload["foo.bar"] != "dot" {
		t.Errorf("decodeFormValues(%v, map[string]string)[foo.bar] = %q, want %q", values, payload["foo.bar"], "dot")
	}
	if payload[`path\name`] != "slash" {
		t.Errorf("decodeFormValues(%v, map[string]string)[path\\name] = %q, want %q", values, payload[`path\name`], "slash")
	}
}

func TestDecodeFormDecodesTopLevelStringSliceMap(t *testing.T) {
	var payload map[string][]string
	values := url.Values{
		"tag":      []string{"go", "html"},
		`foo\.bar`: []string{"dot", "again"},
	}
	err := decodeFormValues(values, &payload)
	if err != nil {
		t.Fatalf("decodeFormValues(%v, map[string][]string) error = %v, want nil", values, err)
	}
	if len(payload["tag"]) != 2 || payload["tag"][0] != "go" || payload["tag"][1] != "html" {
		t.Errorf("decodeFormValues(%v, map[string][]string)[tag] = %#v, want %#v", values, payload["tag"], []string{"go", "html"})
	}
	if len(payload["foo.bar"]) != 2 || payload["foo.bar"][0] != "dot" || payload["foo.bar"][1] != "again" {
		t.Errorf("decodeFormValues(%v, map[string][]string)[foo.bar] = %#v, want %#v", values, payload["foo.bar"], []string{"dot", "again"})
	}
}

func TestDecodeFormDecodesSpecialScalarTypes(t *testing.T) {
	var payload struct {
		Link     url.URL    `form:"link"`
		Body     []byte     `form:"body"`
		Token    formToken  `form:"token"`
		TokenPtr *formToken `form:"token_ptr"`
	}
	values := url.Values{
		"link":      []string{"https://example.com/posts?tag=go"},
		"body":      []string{"hello bytes"},
		"token":     []string{"value"},
		"token_ptr": []string{"pointer"},
	}
	err := decodeFormValues(values, &payload)
	if err != nil {
		t.Fatalf("decodeFormValues(%v, payload) error = %v, want nil", values, err)
	}
	if got := payload.Link.String(); got != "https://example.com/posts?tag=go" {
		t.Errorf("decodeFormValues(%v, payload).Link = %q, want %q", values, got, "https://example.com/posts?tag=go")
	}
	if got := string(payload.Body); got != "hello bytes" {
		t.Errorf("decodeFormValues(%v, payload).Body = %q, want %q", values, got, "hello bytes")
	}
	if payload.Token != "decoded:value" {
		t.Errorf("decodeFormValues(%v, payload).Token = %q, want %q", values, payload.Token, "decoded:value")
	}
	if payload.TokenPtr == nil {
		t.Fatalf("decodeFormValues(%v, payload).TokenPtr = nil, want pointer", values)
	}
	if *payload.TokenPtr != "decoded:pointer" {
		t.Errorf("decodeFormValues(%v, payload).TokenPtr = %q, want %q", values, *payload.TokenPtr, "decoded:pointer")
	}
}

func TestDecodeFormDecodesIndexedSlices(t *testing.T) {
	var payload formPayload
	err := decodeFormValues(url.Values{
		"tags.0": []string{"go"},
		"tags.1": []string{"html"},
	}, &payload)
	if err != nil {
		t.Fatalf("decodeFormValues(values, payload) error = %v, want nil", err)
	}
	if len(payload.Tags) != 2 || payload.Tags[0] != "go" || payload.Tags[1] != "html" {
		t.Errorf("decodeFormValues(values, payload).Tags = %#v, want %#v", payload.Tags, []string{"go", "html"})
	}
}

func TestDecodeFormDecodesIndexedArraysAndNestedValues(t *testing.T) {
	var payload struct {
		Codes   [2]string           `form:"codes"`
		Authors []author            `form:"authors"`
		Meta    []map[string]string `form:"meta"`
	}
	values := url.Values{
		"codes.0":        []string{"go"},
		"codes.1":        []string{"html"},
		"authors.0.name": []string{"ada"},
		"authors.1.name": []string{"grace"},
		"meta.0.role":    []string{"admin"},
	}
	err := decodeFormValues(values, &payload)
	if err != nil {
		t.Fatalf("decodeFormValues(%v, payload) error = %v, want nil", values, err)
	}
	if payload.Codes != [2]string{"go", "html"} {
		t.Errorf("decodeFormValues(%v, payload).Codes = %#v, want %#v", values, payload.Codes, [2]string{"go", "html"})
	}
	if len(payload.Authors) != 2 || payload.Authors[0].Name != "ada" || payload.Authors[1].Name != "grace" {
		t.Errorf("decodeFormValues(%v, payload).Authors = %#v, want names ada and grace", values, payload.Authors)
	}
	if len(payload.Meta) != 1 || payload.Meta[0]["role"] != "admin" {
		t.Errorf("decodeFormValues(%v, payload).Meta = %#v, want role admin", values, payload.Meta)
	}
}

func TestDecodeFormPreservesEscapedDottedStructPaths(t *testing.T) {
	var payload struct {
		Group struct {
			Name string `form:"name"`
		} `form:"foo.bar"`
		Path struct {
			Name string `form:"name"`
		} `form:"path\\name"`
	}
	values := url.Values{
		`foo\.bar.name`:   []string{"dot"},
		`path\\name.name`: []string{"slash"},
	}
	err := decodeFormValues(values, &payload)
	if err != nil {
		t.Fatalf("decodeFormValues(%v, payload) error = %v, want nil", values, err)
	}
	if payload.Group.Name != "dot" {
		t.Errorf("decodeFormValues(%v, payload).Group.Name = %q, want %q", values, payload.Group.Name, "dot")
	}
	if payload.Path.Name != "slash" {
		t.Errorf("decodeFormValues(%v, payload).Path.Name = %q, want %q", values, payload.Path.Name, "slash")
	}
}

func TestDecodeFormPreservesEscapedDottedMapKeys(t *testing.T) {
	var payload struct {
		Preferences map[string]string `form:"prefs"`
	}
	values := url.Values{
		`prefs.foo\.bar`:   []string{"dot"},
		`prefs.path\\name`: []string{"slash"},
	}
	err := decodeFormValues(values, &payload)
	if err != nil {
		t.Fatalf("decodeFormValues(%v, payload) error = %v, want nil", values, err)
	}
	if payload.Preferences["foo.bar"] != "dot" {
		t.Errorf("decodeFormValues(%v, payload).Preferences[foo.bar] = %q, want %q", values, payload.Preferences["foo.bar"], "dot")
	}
	if payload.Preferences[`path\name`] != "slash" {
		t.Errorf("decodeFormValues(%v, payload).Preferences[path\\name] = %q, want %q", values, payload.Preferences[`path\name`], "slash")
	}
}

func TestDecodeFormRejectsUnknownFields(t *testing.T) {
	var payload formPayload
	err := decodeFormValues(url.Values{
		"title":   []string{"hello"},
		"unknown": []string{"value"},
	}, &payload)
	if err == nil {
		t.Fatalf("decodeFormValues(values, payload) error = nil, want error")
	}
}

func TestDecodeFormRejectsInvalidScalar(t *testing.T) {
	var payload formPayload
	err := decodeFormValues(url.Values{
		"count": []string{"many"},
	}, &payload)
	if err == nil {
		t.Fatalf("decodeFormValues(values, payload) error = nil, want error")
	}
}

func TestDecodeFormRejectsInvalidTargets(t *testing.T) {
	var nilPointer *struct{}
	var scalar string
	var nonStringKeyMap map[int]string
	var unsupportedMapValue map[string]int

	tests := []struct {
		name   string
		target any
	}{
		{name: "nil target", target: nil},
		{name: "non pointer", target: struct{}{}},
		{name: "nil pointer", target: nilPointer},
		{name: "scalar pointer", target: &scalar},
		{name: "non string map key", target: &nonStringKeyMap},
		{name: "unsupported map value", target: &unsupportedMapValue},
	}

	values := url.Values{"value": []string{"ok"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := decodeFormValues(values, tt.target)
			if err == nil {
				t.Fatalf("decodeFormValues(%v, %s) error = nil, want error", values, tt.name)
			}
		})
	}
}

func TestDecodeFormRejectsInvalidIndexedFields(t *testing.T) {
	tests := []struct {
		name   string
		values url.Values
	}{
		{
			name:   "non numeric index",
			values: url.Values{"tags.one": []string{"go"}},
		},
		{
			name:   "negative index",
			values: url.Values{"tags.-1": []string{"go"}},
		},
		{
			name:   "nested scalar index",
			values: url.Values{"tags.0.name": []string{"go"}},
		},
		{
			name:   "array overflow",
			values: url.Values{"codes.2": []string{"go"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload struct {
				Tags  []string  `form:"tags"`
				Codes [2]string `form:"codes"`
			}
			err := decodeFormValues(tt.values, &payload)
			if err == nil {
				t.Fatalf("decodeFormValues(%v, payload) error = nil, want error", tt.values)
			}
		})
	}
}

func TestDecodeFormCanReturnRawValues(t *testing.T) {
	var payload url.Values
	values := url.Values{
		"tag": []string{"go", "html"},
	}
	err := decodeFormValues(values, &payload)
	if err != nil {
		t.Fatalf("decodeFormValues(%v, url.Values) error = %v, want nil", values, err)
	}
	values["tag"][0] = "changed"
	if len(payload["tag"]) != 2 || payload["tag"][0] != "go" || payload["tag"][1] != "html" {
		t.Errorf("decodeFormValues(%v, url.Values)[tag] = %#v, want %#v", values, payload["tag"], []string{"go", "html"})
	}
}

func TestDecodeFormRejectsMalformedRequestBody(t *testing.T) {
	var payload struct {
		Name string `form:"name"`
	}
	request := httptest.NewRequest("POST", "/", strings.NewReader("name=%zz"))
	err := decodeForm(request, &payload)
	if err == nil {
		t.Fatalf("decodeForm(request with malformed body, payload) error = nil, want error")
	}
}
