package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mgomes/ohm/cli"
)

// Command returns a command that replays a request snapshot through handler.
func Command(handler http.Handler) cli.Command {
	return cli.Command{
		Name:    "replay",
		Summary: "replay a request snapshot",
		Usage:   "replay <snapshot.json>",
		Run: func(ctx context.Context, commandIO cli.IO, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("%w: replay requires one snapshot path", cli.ErrUsage)
			}

			snapshot, err := readSnapshot(args[0])
			if err != nil {
				return err
			}

			response, err := run(ctx, handler, snapshot)
			if err != nil {
				return fmt.Errorf("run replay: %w", err)
			}

			stdout := output(commandIO.Stdout)
			statusCode := response.Result().StatusCode
			status := http.StatusText(statusCode)
			if status == "" {
				status = "Unknown Status"
			}
			if _, err := fmt.Fprintf(stdout, "Status: %d %s\n", statusCode, status); err != nil {
				return fmt.Errorf("write replay status: %w", err)
			}
			if response.Body.Len() == 0 {
				return nil
			}
			if _, err := fmt.Fprintln(stdout); err != nil {
				return fmt.Errorf("write replay separator: %w", err)
			}
			if _, err := response.Body.WriteTo(stdout); err != nil {
				return fmt.Errorf("write replay body: %w", err)
			}
			return nil
		},
	}
}

func readSnapshot(path string) (snapshot Snapshot, err error) {
	file, err := os.Open(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open replay snapshot %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close replay snapshot %q: %w", path, closeErr)
		}
	}()

	if err := json.NewDecoder(file).Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode replay snapshot %q: %w", path, err)
	}
	return snapshot, nil
}

func output(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
