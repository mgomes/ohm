package ohm

import (
	"context"
	"errors"
	htmltemplate "html/template"
	"strings"
	"testing"
)

func TestHTMLTemplateRendersNamedTemplate(t *testing.T) {
	tmpl := htmltemplate.Must(htmltemplate.New("views").Parse(`
{{ define "hello" }}<p>Hello, {{ .Name }}</p>{{ end }}
`))

	var body strings.Builder
	err := HTMLTemplate(tmpl, "hello", struct {
		Name string
	}{Name: "<Ada>"}).RenderHTML(context.Background(), &body)
	if err != nil {
		t.Fatalf("HTMLTemplate(t, name, data).RenderHTML(ctx, w) error = %v, want nil", err)
	}
	if got := body.String(); got != "<p>Hello, &lt;Ada&gt;</p>" {
		t.Errorf("HTMLTemplate(t, name, data).RenderHTML(ctx, w) body = %q, want escaped HTML", got)
	}
}

func TestHTMLTemplateReturnsContextError(t *testing.T) {
	tmpl := htmltemplate.Must(htmltemplate.New("views").Parse(`{{ define "hello" }}Hello{{ end }}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := HTMLTemplate(tmpl, "hello", nil).RenderHTML(ctx, &strings.Builder{})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("HTMLTemplate(t, name, data).RenderHTML(canceled ctx, w) error = %v, want %v", err, context.Canceled)
	}
}

func TestHTMLTemplateRejectsInvalidTemplate(t *testing.T) {
	tests := []struct {
		name string
		html HTML
		want string
	}{
		{
			name: "nil-template",
			html: HTMLTemplate(nil, "hello", nil),
			want: "html template is required",
		},
		{
			name: "empty-name",
			html: HTMLTemplate(htmltemplate.New("views"), "", nil),
			want: "html template name is required",
		},
	}

	for _, tt := range tests {
		err := tt.html.RenderHTML(context.Background(), &strings.Builder{})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("HTMLTemplate invalid case %s error = %v, want containing %q", tt.name, err, tt.want)
		}
	}
}
