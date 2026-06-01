package migrate

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/mgomes/ohm/cli"
)

// Command returns a migrate command with up, down, reset, and status subcommands.
func Command(runner Runner) cli.Command {
	return cli.Command{
		Name:    "migrate",
		Summary: "run database migrations",
		Usage:   "migrate <up|down|reset|status>",
		Run: func(ctx context.Context, io cli.IO, args []string) error {
			if runner == nil {
				return fmt.Errorf("migration runner is required")
			}
			if len(args) != 1 {
				return fmt.Errorf("%w: migrate requires one subcommand", cli.ErrUsage)
			}

			switch args[0] {
			case "up":
				return runUp(ctx, io, runner)
			case "down":
				return runDown(ctx, io, runner)
			case "reset":
				return runReset(ctx, io, runner)
			case "status":
				return runStatus(ctx, io, runner)
			default:
				return fmt.Errorf("%w: unknown migrate subcommand %q", cli.ErrUsage, args[0])
			}
		},
	}
}

func runUp(ctx context.Context, io cli.IO, runner Runner) error {
	stdout := output(io.Stdout)
	results, err := runner.Up(ctx)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Fprintln(stdout, "No pending migrations.")
		return nil
	}
	for _, result := range results {
		fmt.Fprintf(stdout, "Applied %d %s\n", result.Version, result.Source)
	}
	return nil
}

func runDown(ctx context.Context, io cli.IO, runner Runner) error {
	stdout := output(io.Stdout)
	result, err := runner.Down(ctx)
	if err != nil {
		return err
	}
	if result.Skipped {
		fmt.Fprintln(stdout, "No migrations to roll back.")
		return nil
	}
	fmt.Fprintf(stdout, "Rolled back %d %s\n", result.Version, result.Source)
	return nil
}

func runReset(ctx context.Context, io cli.IO, runner Runner) error {
	stdout := output(io.Stdout)
	results, err := runner.Reset(ctx)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Fprintln(stdout, "No migrations to reset.")
		return nil
	}
	for _, result := range results {
		fmt.Fprintf(stdout, "Rolled back %d %s\n", result.Version, result.Source)
	}
	return nil
}

func runStatus(ctx context.Context, io cli.IO, runner Runner) error {
	stdout := output(io.Stdout)
	statuses, err := runner.Status(ctx)
	if err != nil {
		return err
	}

	writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "VERSION\tSTATE\tSOURCE")
	for _, status := range statuses {
		fmt.Fprintf(writer, "%d\t%s\t%s\n", status.Version, status.State, status.Source)
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("write migration status: %w", err)
	}
	return nil
}

func output(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
