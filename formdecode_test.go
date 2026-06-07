package ohm

import (
	"net/url"
	"testing"
)

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

func TestDecodeFormCanReturnRawValues(t *testing.T) {
	var payload url.Values
	err := decodeFormValues(url.Values{
		"tag": []string{"go", "html"},
	}, &payload)
	if err != nil {
		t.Fatalf("decodeFormValues(values, url.Values) error = %v, want nil", err)
	}
	if len(payload["tag"]) != 2 || payload["tag"][0] != "go" || payload["tag"][1] != "html" {
		t.Errorf("decodeFormValues(values, url.Values)[tag] = %#v, want %#v", payload["tag"], []string{"go", "html"})
	}
}
