package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/mgomes/ohm"
)

// RoutesCommand returns a command that prints registered routes.
func RoutesCommand(app interface {
	Routes() ([]ohm.Route, error)
}) Command {
	return Command{
		Name:    "routes",
		Summary: "list application routes",
		Usage:   "routes",
		Run: func(_ context.Context, commandIO IO, args []string) error {
			commandIO = commandIO.withDefaults()
			if len(args) > 0 {
				return fmt.Errorf("%w: routes does not accept arguments", ErrUsage)
			}

			routes, err := app.Routes()
			if err != nil {
				return fmt.Errorf("load routes: %w", err)
			}

			writer := tabwriter.NewWriter(commandIO.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(writer, "METHOD\tPATTERN")
			for _, route := range routes {
				fmt.Fprintf(writer, "%s\t%s\n", route.Method, route.Pattern)
			}
			if err := writer.Flush(); err != nil {
				return fmt.Errorf("write routes: %w", err)
			}
			return nil
		},
	}
}
