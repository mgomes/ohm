package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/mgomes/ohm"
)

func TestRoutesCommandPrintsRoutes(t *testing.T) {
	app := ohm.New()
	app.Post("/posts", func(req *ohm.Request) error {
		return nil
	})
	app.Get("/posts/{id}", func(req *ohm.Request) error {
		return nil
	})

	var stdout bytes.Buffer
	command := RoutesCommand(app)
	err := command.Run(context.Background(), IO{Stdout: &stdout}, nil)
	if err != nil {
		t.Fatalf("RoutesCommand(app).Run(ctx, io, nil) error = %v, want nil", err)
	}

	want := "METHOD  PATTERN\nPOST    /posts\nGET     /posts/{id}\n"
	if stdout.String() != want {
		t.Errorf("RoutesCommand(app).Run(ctx, io, nil) stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRoutesCommandRejectsArguments(t *testing.T) {
	app := ohm.New()
	command := RoutesCommand(app)

	err := command.Run(context.Background(), IO{Stdout: &bytes.Buffer{}}, []string{"extra"})
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("RoutesCommand(app).Run(ctx, io, %v) error = %v, want ErrUsage", []string{"extra"}, err)
	}
}
