package scaffold

var viewTemplates = map[string]string{
	"internal/views/views.go": `// Package views provides server-rendered HTML helpers.
package views

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"io"

	"github.com/mgomes/ohm"
)

//go:embed templates
var files embed.FS

var templates = htmltemplate.Must(htmltemplate.ParseFS(files,
	"templates/layouts/*.html",
	"templates/pages/*.html",
	"templates/partials/*.html",
	"templates/components/*.html",
))

// LayoutData is the data passed to the application layout.
type LayoutData struct {
	Title   string
	Content htmltemplate.HTML
}

// Render renders a named html/template template with data.
func Render(name string, data any) ohm.HTML {
	return ohm.HTMLTemplate(templates, name, data)
}

// Page wraps trusted body HTML in the application layout.
func Page(title string, body ohm.HTML) ohm.HTML {
	return ohm.HTMLFunc(func(ctx context.Context, w io.Writer) error {
		if body == nil {
			return fmt.Errorf("page body is required")
		}

		var content bytes.Buffer
		if err := body.RenderHTML(ctx, &content); err != nil {
			return fmt.Errorf("render page body: %w", err)
		}

		return Render("layouts/application", LayoutData{
			Title:   title,
			Content: htmltemplate.HTML(content.String()),
		}).RenderHTML(ctx, w)
	})
}
`,
	"internal/views/templates/layouts/application.html": `{{ "{{" }} define "layouts/application" -{{ "}}" }}
<!doctype html>
<html lang="en">
	<head>
		<meta charset="utf-8">
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<title>{{ "{{" }} .Title {{ "}}" }}</title>
	</head>
	<body>
		<main>
			{{ "{{" }} .Content {{ "}}" }}
		</main>
	</body>
</html>
{{ "{{" }}- end {{ "}}" }}
`,
	"internal/views/templates/pages/error.html": `{{ "{{" }} define "pages/error" -{{ "}}" }}
<h1>{{ "{{" }} .Message {{ "}}" }}</h1>
<p>HTTP {{ "{{" }} .Status {{ "}}" }}</p>
{{ "{{" }}- end {{ "}}" }}
`,
	"internal/views/templates/pages/home.html": `{{ "{{" }} define "pages/home" -{{ "}}" }}
{{ "{{" }} template "partials/home" . {{ "}}" }}
{{ "{{" }}- end {{ "}}" }}
`,
	"internal/views/templates/partials/home.html": `{{ "{{" }} define "partials/home" -{{ "}}" }}
<section id="home">
	<h1>Welcome to {{ "{{" }} .Title {{ "}}" }}</h1>
</section>
{{ "{{" }}- end {{ "}}" }}
`,
	"internal/views/templates/components/flash.html": `{{ "{{" }} define "components/flash" -{{ "}}" }}
{{ "{{" }} if .Messages {{ "}}" }}
<section aria-label="Notifications" class="flash-messages">
	{{ "{{" }} range .Messages {{ "}}" }}
	<p class="{{ "{{" }} .CSSClass {{ "}}" }}" role="{{ "{{" }} .Role {{ "}}" }}">{{ "{{" }} .Text {{ "}}" }}</p>
	{{ "{{" }} end {{ "}}" }}
</section>
{{ "{{" }} end {{ "}}" }}
{{ "{{" }}- end {{ "}}" }}
`,
	"internal/views/pages/home.go": `package pages

import (
	"github.com/mgomes/ohm"

	"{{.Module}}/internal/views"
)

type HomeData struct {
	Title string
}

func Home(title string) ohm.HTML {
	data := HomeData{Title: title}
	return views.Page(title, views.Render("pages/home", data))
}
`,
	"internal/views/pages/home_test.go": `package pages

import (
	"context"
	"strings"
	"testing"
)

func TestHomeRendersApplicationLayout(t *testing.T) {
	var body strings.Builder
	if err := Home("{{.Title}}").RenderHTML(context.Background(), &body); err != nil {
		t.Fatalf("Home(title).RenderHTML(ctx, w) error = %v, want nil", err)
	}
	if !strings.Contains(body.String(), "<title>{{.Title}}</title>") {
		t.Errorf("Home(title) body = %q, want page title", body.String())
	}
	if !strings.Contains(body.String(), "<h1>Welcome to {{.Title}}</h1>") {
		t.Errorf("Home(title) body = %q, want heading", body.String())
	}
	if !strings.Contains(body.String(), "<section id=\"home\">") {
		t.Errorf("Home(title) body = %q, want home partial target", body.String())
	}
}
`,
	"internal/views/pages/error.go": `package pages

import (
	"github.com/mgomes/ohm"

	"{{.Module}}/internal/views"
)

type ErrorData struct {
	Status  int
	Message string
}

func Error(status int, message string) ohm.HTML {
	data := ErrorData{
		Status:  status,
		Message: message,
	}
	return views.Page(message, views.Render("pages/error", data))
}
`,
	"internal/views/pages/error_test.go": `package pages

import (
	"context"
	"strings"
	"testing"
)

func TestErrorRendersApplicationLayout(t *testing.T) {
	var body strings.Builder
	if err := Error(404, "Not Found").RenderHTML(context.Background(), &body); err != nil {
		t.Fatalf("Error(status, message).RenderHTML(ctx, w) error = %v, want nil", err)
	}
	if !strings.Contains(body.String(), "<title>Not Found</title>") {
		t.Errorf("Error(status, message) body = %q, want page title", body.String())
	}
	if !strings.Contains(body.String(), "<h1>Not Found</h1>") {
		t.Errorf("Error(status, message) body = %q, want heading", body.String())
	}
	if !strings.Contains(body.String(), "<p>HTTP 404</p>") {
		t.Errorf("Error(status, message) body = %q, want status code", body.String())
	}
}
`,
	"internal/views/partials/README.md": `# Partials

Place route-addressable HTML fragments here.
`,
	"internal/views/partials/home.go": `package partials

import (
	"github.com/mgomes/ohm"

	"{{.Module}}/internal/views"
)

type HomeData struct {
	Title string
}

func Home(title string) ohm.HTML {
	return views.Render("partials/home", HomeData{Title: title})
}
`,
	"internal/views/partials/home_test.go": `package partials

import (
	"context"
	"strings"
	"testing"
)

func TestHomeRendersFragmentTarget(t *testing.T) {
	var body strings.Builder
	if err := Home("{{.Title}}").RenderHTML(context.Background(), &body); err != nil {
		t.Fatalf("Home(title).RenderHTML(ctx, w) error = %v, want nil", err)
	}
	if !strings.Contains(body.String(), "<section id=\"home\">") {
		t.Errorf("Home(title) body = %q, want home target", body.String())
	}
	if !strings.Contains(body.String(), "<h1>Welcome to {{.Title}}</h1>") {
		t.Errorf("Home(title) body = %q, want heading", body.String())
	}
	if strings.Contains(body.String(), "<!doctype html>") {
		t.Errorf("Home(title) body = %q, want fragment without document layout", body.String())
	}
}
`,
	"internal/views/components/README.md": `# Components

Place reusable HTML fragments here.
`,
	"internal/views/components/flash.go": `package components

import (
	"github.com/mgomes/ohm"

	"{{.Module}}/internal/views"
)

type FlashLevel string

const (
	FlashInfo    FlashLevel = "info"
	FlashSuccess FlashLevel = "success"
	FlashWarning FlashLevel = "warning"
	FlashError   FlashLevel = "error"
)

type FlashMessage struct {
	Level FlashLevel
	Text  string
}

type flashData struct {
	Messages []FlashMessage
}

func Flash(messages []FlashMessage) ohm.HTML {
	return views.Render("components/flash", flashData{Messages: messages})
}

func NewFlashMessage(level FlashLevel, text string) FlashMessage {
	if level == "" {
		level = FlashInfo
	}
	return FlashMessage{Level: level, Text: text}
}

func (m FlashMessage) CSSClass() string {
	switch m.Level {
	case FlashSuccess:
		return "flash flash-success"
	case FlashWarning:
		return "flash flash-warning"
	case FlashError:
		return "flash flash-error"
	default:
		return "flash flash-info"
	}
}

func (m FlashMessage) Role() string {
	if m.Level == FlashError {
		return "alert"
	}
	return "status"
}
`,
	"internal/views/components/flash_test.go": `package components

import (
	"context"
	"strings"
	"testing"
)

func TestFlashRendersMessages(t *testing.T) {
	messages := []FlashMessage{
		NewFlashMessage(FlashSuccess, "Saved"),
		NewFlashMessage(FlashError, "<failed>"),
	}

	var body strings.Builder
	if err := Flash(messages).RenderHTML(context.Background(), &body); err != nil {
		t.Fatalf("Flash(messages).RenderHTML(ctx, w) error = %v, want nil", err)
	}
	if !strings.Contains(body.String(), "<section aria-label=\"Notifications\" class=\"flash-messages\">") {
		t.Errorf("Flash(messages) body = %q, want notifications region", body.String())
	}
	if !strings.Contains(body.String(), "<p class=\"flash flash-success\" role=\"status\">Saved</p>") {
		t.Errorf("Flash(messages) body = %q, want success message", body.String())
	}
	if !strings.Contains(body.String(), "<p class=\"flash flash-error\" role=\"alert\">&lt;failed&gt;</p>") {
		t.Errorf("Flash(messages) body = %q, want escaped error alert", body.String())
	}
}

func TestFlashOmitsEmptyMessages(t *testing.T) {
	var body strings.Builder
	if err := Flash(nil).RenderHTML(context.Background(), &body); err != nil {
		t.Fatalf("Flash(nil).RenderHTML(ctx, w) error = %v, want nil", err)
	}
	if body.String() != "" {
		t.Errorf("Flash(nil) body = %q, want empty", body.String())
	}
}

func TestNewFlashMessageDefaultsLevel(t *testing.T) {
	message := NewFlashMessage("", "Saved")

	if message.Level != FlashInfo {
		t.Errorf("NewFlashMessage(%q, %q).Level = %q, want %q", "", "Saved", message.Level, FlashInfo)
	}
	if message.CSSClass() != "flash flash-info" {
		t.Errorf("NewFlashMessage(%q, %q).CSSClass() = %q, want %q", "", "Saved", message.CSSClass(), "flash flash-info")
	}
	if message.Role() != "status" {
		t.Errorf("NewFlashMessage(%q, %q).Role() = %q, want %q", "", "Saved", message.Role(), "status")
	}
}
`,
	"internal/views/forms/forms.go": `package forms

import (
	"slices"
	"strings"
	"unicode"
)

type Values map[string]string

type FieldErrors interface {
	Messages(name string) []string
}

type Errors map[string][]string

type Field struct {
	Name   string
	ID     string
	Label  string
	Value  string
	Errors []string
}

func NewField(name string, label string, values Values, errors FieldErrors) Field {
	if label == "" {
		label = Label(name)
	}
	return Field{
		Name:   name,
		ID:     FieldID(name),
		Label:  label,
		Value:  values.Get(name),
		Errors: fieldErrors(name, errors),
	}
}

func (v Values) Get(name string) string {
	if v == nil {
		return ""
	}
	return v[name]
}

func (e Errors) Get(name string) []string {
	return e.Messages(name)
}

func (e Errors) Messages(name string) []string {
	if e == nil {
		return nil
	}
	return slices.Clone(e[name])
}

func fieldErrors(name string, errors FieldErrors) []string {
	if errors == nil {
		return nil
	}
	return slices.Clone(errors.Messages(name))
}

func FieldID(name string) string {
	id := normalizedFieldID(name)
	if id == "" {
		return "field"
	}
	return id
}

func Label(name string) string {
	id := normalizedFieldID(name)
	if id == "" {
		return ""
	}

	parts := strings.Split(id, "-")
	for i, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func normalizedFieldID(name string) string {
	var builder strings.Builder
	lastSeparator := false
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
			lastSeparator = false
			continue
		}
		if builder.Len() > 0 && !lastSeparator {
			builder.WriteByte('-')
			lastSeparator = true
		}
	}

	return strings.Trim(builder.String(), "-")
}
`,
	"internal/views/forms/forms_test.go": `package forms

import (
	"slices"
	"testing"

	"github.com/mgomes/ohm"
)

func TestNewFieldBuildsViewData(t *testing.T) {
	values := Values{"post[title]": "Hello"}
	errors := Errors{"post[title]": []string{"is required"}}

	field := NewField("post[title]", "", values, errors)

	if field.Name != "post[title]" {
		t.Errorf("NewField(%q, label, values, errors).Name = %q, want %q", "post[title]", field.Name, "post[title]")
	}
	if field.ID != "post-title" {
		t.Errorf("NewField(%q, label, values, errors).ID = %q, want %q", "post[title]", field.ID, "post-title")
	}
	if field.Label != "Post Title" {
		t.Errorf("NewField(%q, label, values, errors).Label = %q, want %q", "post[title]", field.Label, "Post Title")
	}
	if field.Value != "Hello" {
		t.Errorf("NewField(%q, label, values, errors).Value = %q, want %q", "post[title]", field.Value, "Hello")
	}
	if !slices.Equal(field.Errors, []string{"is required"}) {
		t.Errorf("NewField(%q, label, values, errors).Errors = %v, want %v", "post[title]", field.Errors, []string{"is required"})
	}

	errors["post[title]"][0] = "changed"
	if !slices.Equal(field.Errors, []string{"is required"}) {
		t.Errorf("NewField(%q, label, values, errors).Errors after source mutation = %v, want %v", "post[title]", field.Errors, []string{"is required"})
	}
}

func TestNewFieldUsesExplicitLabel(t *testing.T) {
	field := NewField("email", "Email address", nil, nil)

	if field.Label != "Email address" {
		t.Errorf("NewField(%q, %q, nil, nil).Label = %q, want %q", "email", "Email address", field.Label, "Email address")
	}
}

func TestNewFieldAcceptsOhmErrors(t *testing.T) {
	validation := ohm.NewValidation()
	validation.String("email", "").Presence()

	field := NewField("email", "", nil, validation.Errors())

	if !slices.Equal(field.Errors, []string{"is required"}) {
		t.Errorf("NewField(%q, label, values, ohm.Errors).Errors = %v, want %v", "email", field.Errors, []string{"is required"})
	}
}

func TestNewFieldCopiesMessageProviderErrors(t *testing.T) {
	errors := testFieldErrors{
		messages: map[string][]string{
			"email": []string{"is required"},
		},
	}

	field := NewField("email", "", nil, errors)

	errors.messages["email"][0] = "changed"
	if !slices.Equal(field.Errors, []string{"is required"}) {
		t.Errorf("NewField(%q, label, values, errors).Errors after provider mutation = %v, want %v", "email", field.Errors, []string{"is required"})
	}
}

func TestLabel(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "field", want: "Field"},
		{name: "Field", want: "Field"},
		{name: "post[title]", want: "Post Title"},
		{name: "!!!", want: ""},
	}

	for _, tt := range tests {
		got := Label(tt.name)
		if got != tt.want {
			t.Errorf("Label(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFieldID(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "email", want: "email"},
		{name: "post[title]", want: "post-title"},
		{name: " user email ", want: "user-email"},
		{name: "profile.avatar_url", want: "profile-avatar-url"},
		{name: "!!!", want: "field"},
	}

	for _, tt := range tests {
		got := FieldID(tt.name)
		if got != tt.want {
			t.Errorf("FieldID(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

type testFieldErrors struct {
	messages map[string][]string
}

func (e testFieldErrors) Messages(name string) []string {
	return e.messages[name]
}
`,
	"internal/views/assets/assets.go": `package assets

import (
	"net/url"
	"path"
	"strings"
)

const basePath = "/assets/"

func Path(name string) string {
	cleaned := path.Clean("/" + strings.TrimPrefix(name, "/"))
	if cleaned == "/" {
		return basePath
	}

	parts := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return basePath + strings.Join(parts, "/")
}
`,
	"internal/views/assets/assets_test.go": `package assets

import "testing"

func TestPathBuildsAssetURL(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "", want: "/assets/"},
		{name: "app.css", want: "/assets/app.css"},
		{name: "/icons/logo.svg", want: "/assets/icons/logo.svg"},
		{name: "icons/../app.css", want: "/assets/app.css"},
		{name: "avatars/Jane Doe.png", want: "/assets/avatars/Jane%20Doe.png"},
	}

	for _, tt := range tests {
		got := Path(tt.name)
		if got != tt.want {
			t.Errorf("Path(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
`,
	"static/README.md": `# Static Assets

Place application static assets here.
`,
	"static/app.css": `body {
	margin: 2rem;
	font-family: system-ui, sans-serif;
}
`,
}
