package ohm

import (
	"context"
	"fmt"
	htmltemplate "html/template"
	"io"
)

// HTML renders a server-rendered HTML response body.
type HTML interface {
	RenderHTML(context.Context, io.Writer) error
}

// HTMLFunc adapts a function to HTML.
type HTMLFunc func(context.Context, io.Writer) error

// RenderHTML renders HTML through f.
func (f HTMLFunc) RenderHTML(ctx context.Context, w io.Writer) error {
	if f == nil {
		return fmt.Errorf("html function is required")
	}
	return f(ctx, w)
}

// HTMLTemplate renders a named html/template template with data.
func HTMLTemplate(t *htmltemplate.Template, name string, data any) HTML {
	return htmlTemplate{
		template: t,
		name:     name,
		data:     data,
	}
}

type htmlTemplate struct {
	template *htmltemplate.Template
	name     string
	data     any
}

func (t htmlTemplate) RenderHTML(ctx context.Context, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.template == nil {
		return fmt.Errorf("html template is required")
	}
	if t.name == "" {
		return fmt.Errorf("html template name is required")
	}
	return t.template.ExecuteTemplate(w, t.name, t.data)
}
