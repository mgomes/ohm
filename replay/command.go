package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgomes/ohm/cli"
)

// Command returns a command that replays a request snapshot through handler.
func Command(handler http.Handler) cli.Command {
	return cli.Command{
		Name:    "replay",
		Summary: "replay a request snapshot",
		Usage:   "replay [--write-expected] [--write-expected-body] <snapshot.json>",
		Run: func(ctx context.Context, commandIO cli.IO, args []string) error {
			parsed, err := parseArgs(args)
			if err != nil {
				return err
			}

			snapshot, err := readSnapshot(parsed.snapshotPath)
			if err != nil {
				return err
			}

			if err := writeBoundaryWarnings(output(commandIO.Stderr), snapshot); err != nil {
				return err
			}

			response, err := run(ctx, handler, snapshot)
			if err != nil {
				return fmt.Errorf("run replay: %w", err)
			}
			if parsed.writeExpected {
				expectedOpts := []ExpectedResponseOption(nil)
				if parsed.writeExpectedBody {
					expectedOpts = append(expectedOpts, WithExpectedResponseBodyLimit(DefaultExpectedResponseBodyLimit))
				}
				expected, err := ExpectedResponseFrom(response, expectedOpts...)
				if err != nil {
					return err
				}
				snapshot.ExpectedResponse = &expected
				if err := writeSnapshotFile(parsed.snapshotPath, snapshot); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(output(commandIO.Stderr), "Updated %s\n", parsed.snapshotPath); err != nil {
					return fmt.Errorf("write replay update: %w", err)
				}
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

func writeBoundaryWarnings(w io.Writer, snapshot Snapshot) error {
	boundaries, err := snapshotBoundaries(snapshot)
	if err != nil {
		return err
	}
	for _, boundary := range boundaries.uncontrolled {
		if _, err := fmt.Fprintf(w, "Warning: replay snapshot records uncontrolled %s boundary; results may not be deterministic.\n", boundary); err != nil {
			return fmt.Errorf("write replay determinism warning: %w", err)
		}
	}
	return nil
}

type args struct {
	snapshotPath      string
	writeExpected     bool
	writeExpectedBody bool
}

func parseArgs(raw []string) (args, error) {
	parsed := args{}
	var positionals []string

	for _, arg := range raw {
		switch {
		case arg == "--write-expected" || arg == "-write-expected":
			parsed.writeExpected = true
		case arg == "--write-expected-body" || arg == "-write-expected-body":
			parsed.writeExpected = true
			parsed.writeExpectedBody = true
		case strings.HasPrefix(arg, "-"):
			return args{}, fmt.Errorf("%w: unknown replay flag %q", cli.ErrUsage, arg)
		default:
			positionals = append(positionals, arg)
		}
	}

	if len(positionals) != 1 {
		return args{}, fmt.Errorf("%w: replay requires one snapshot path", cli.ErrUsage)
	}
	parsed.snapshotPath = positionals[0]
	return parsed, nil
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

	snapshot, err = DecodeSnapshot(file)
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode replay snapshot %q: %w", path, err)
	}
	return snapshot, nil
}

func writeSnapshotFile(path string, snapshot Snapshot) (err error) {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode replay snapshot %q: %w", path, err)
	}
	data = append(data, '\n')

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect replay snapshot %q: %w", path, err)
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".")
	if err != nil {
		return fmt.Errorf("create temporary replay snapshot for %q: %w", path, err)
	}
	tempPath := temp.Name()
	removeTemp := true
	closed := false
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	defer func() {
		if closed {
			return
		}
		if closeErr := temp.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close temporary replay snapshot for %q: %w", path, closeErr)
		}
	}()

	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write temporary replay snapshot for %q: %w", path, err)
	}
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod temporary replay snapshot for %q: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary replay snapshot for %q: %w", path, err)
	}
	closed = true
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace replay snapshot %q: %w", path, err)
	}
	removeTemp = false
	return nil
}

func output(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
