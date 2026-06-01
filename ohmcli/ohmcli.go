package ohmcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/mgomes/ohm/cli"
	"github.com/mgomes/ohm/scaffold"
)

// Option configures the Ohm framework CLI.
type Option func(*options)

type options struct {
	io         cli.IO
	ohmVersion string
}

// WithIO configures command input and output streams.
func WithIO(commandIO cli.IO) Option {
	return func(opts *options) {
		opts.io = commandIO
	}
}

// WithOhmVersion configures the Ohm module version written to generated apps.
func WithOhmVersion(version string) Option {
	return func(opts *options) {
		opts.ohmVersion = version
	}
}

// New returns the Ohm framework CLI program.
func New(opts ...Option) *cli.Program {
	cfg := options{}
	for _, opt := range opts {
		opt(&cfg)
	}

	return cli.New("ohm", []cli.Command{
		generateCommand(),
		newCommand(cfg.ohmVersion),
	}, cli.WithIO(cfg.io))
}

func generateCommand() cli.Command {
	return cli.Command{
		Name:    "generate",
		Summary: "generate application code",
		Usage:   "generate <migration> [args]",
		Run: func(_ context.Context, commandIO cli.IO, args []string) error {
			commandIO = withIODefaults(commandIO)
			if len(args) == 0 {
				return fmt.Errorf("%w: generate requires a generator name", cli.ErrUsage)
			}
			if isHelpArg(args[0]) {
				return cli.ErrHelp
			}

			switch args[0] {
			case "migration":
				parsed, err := parseMigrationArgs(args[1:])
				if err != nil {
					return err
				}
				path, err := scaffold.GenerateMigration(scaffold.Migration{
					Name: parsed.name,
					Dir:  parsed.dir,
				})
				if err != nil {
					return err
				}
				fmt.Fprintf(commandIO.Stdout, "Created %s\n", path)
				return nil
			default:
				return fmt.Errorf("%w: unknown generator %q", cli.ErrUsage, args[0])
			}
		},
	}
}

func newCommand(defaultOhmVersion string) cli.Command {
	return cli.Command{
		Name:    "new",
		Summary: "create a new Ohm application",
		Usage:   "new <name> [-db postgres|sqlite] [-module module/path] [-ohm-version version]",
		Run: func(ctx context.Context, commandIO cli.IO, args []string) error {
			commandIO = withIODefaults(commandIO)

			parsed, err := parseNewArgs(args)
			if err != nil {
				return err
			}
			if parsed.ohmVersion == "" {
				parsed.ohmVersion = defaultOhmVersion
			}
			if parsed.ohmVersion == "" {
				version, err := resolveLatestOhmVersion(ctx)
				if err != nil {
					return err
				}
				parsed.ohmVersion = version
			}

			if err := scaffold.GenerateApp(scaffold.App{
				Destination: parsed.destination,
				Module:      parsed.module,
				Database:    parsed.database,
				OhmVersion:  parsed.ohmVersion,
			}); err != nil {
				return err
			}

			fmt.Fprintf(commandIO.Stdout, "Created %s\n", parsed.destination)
			return nil
		},
	}
}

type migrationArgs struct {
	name string
	dir  string
}

func parseMigrationArgs(args []string) (migrationArgs, error) {
	parsed := migrationArgs{}
	var positionals []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isHelpArg(arg):
			return migrationArgs{}, cli.ErrHelp
		case arg == "-dir" || arg == "--dir":
			value, ok := nextArg(args, &i)
			if !ok {
				return migrationArgs{}, fmt.Errorf("%w: %s requires a value", cli.ErrUsage, arg)
			}
			parsed.dir = value
		case strings.HasPrefix(arg, "-dir="):
			parsed.dir = strings.TrimPrefix(arg, "-dir=")
		case strings.HasPrefix(arg, "--dir="):
			parsed.dir = strings.TrimPrefix(arg, "--dir=")
		case strings.HasPrefix(arg, "-"):
			return migrationArgs{}, fmt.Errorf("%w: unknown migration flag %q", cli.ErrUsage, arg)
		default:
			positionals = append(positionals, arg)
		}
	}

	if len(positionals) != 1 {
		return migrationArgs{}, fmt.Errorf("%w: migration requires exactly one name", cli.ErrUsage)
	}
	parsed.name = positionals[0]
	return parsed, nil
}

type newArgs struct {
	destination string
	module      string
	database    scaffold.Database
	ohmVersion  string
}

func parseNewArgs(args []string) (newArgs, error) {
	parsed := newArgs{
		database: scaffold.DatabasePostgres,
	}
	var positionals []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isHelpArg(arg):
			return newArgs{}, cli.ErrHelp
		case arg == "-db" || arg == "--db":
			value, ok := nextArg(args, &i)
			if !ok {
				return newArgs{}, fmt.Errorf("%w: %s requires a value", cli.ErrUsage, arg)
			}
			parsed.database = scaffold.Database(value)
		case strings.HasPrefix(arg, "-db="):
			parsed.database = scaffold.Database(strings.TrimPrefix(arg, "-db="))
		case strings.HasPrefix(arg, "--db="):
			parsed.database = scaffold.Database(strings.TrimPrefix(arg, "--db="))
		case arg == "-module" || arg == "--module":
			value, ok := nextArg(args, &i)
			if !ok {
				return newArgs{}, fmt.Errorf("%w: %s requires a value", cli.ErrUsage, arg)
			}
			parsed.module = value
		case strings.HasPrefix(arg, "-module="):
			parsed.module = strings.TrimPrefix(arg, "-module=")
		case strings.HasPrefix(arg, "--module="):
			parsed.module = strings.TrimPrefix(arg, "--module=")
		case arg == "-ohm-version" || arg == "--ohm-version":
			value, ok := nextArg(args, &i)
			if !ok {
				return newArgs{}, fmt.Errorf("%w: %s requires a value", cli.ErrUsage, arg)
			}
			parsed.ohmVersion = value
		case strings.HasPrefix(arg, "-ohm-version="):
			parsed.ohmVersion = strings.TrimPrefix(arg, "-ohm-version=")
		case strings.HasPrefix(arg, "--ohm-version="):
			parsed.ohmVersion = strings.TrimPrefix(arg, "--ohm-version=")
		case strings.HasPrefix(arg, "-"):
			return newArgs{}, fmt.Errorf("%w: unknown new flag %q", cli.ErrUsage, arg)
		default:
			positionals = append(positionals, arg)
		}
	}

	if len(positionals) != 1 {
		return newArgs{}, fmt.Errorf("%w: new requires exactly one app name", cli.ErrUsage)
	}
	parsed.destination = positionals[0]
	return parsed, nil
}

func resolveLatestOhmVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-json", "github.com/mgomes/ohm@latest")
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return "", fmt.Errorf("resolve latest ohm module version: %w", err)
		}
		return "", fmt.Errorf("resolve latest ohm module version: %w: %s", err, message)
	}

	var module struct {
		Version string
	}
	if err := json.Unmarshal(output, &module); err != nil {
		return "", fmt.Errorf("decode latest ohm module version: %w", err)
	}
	if module.Version == "" {
		return "", fmt.Errorf("resolve latest ohm module version: version is empty")
	}
	return module.Version, nil
}

func nextArg(args []string, i *int) (string, bool) {
	if *i+1 >= len(args) || strings.HasPrefix(args[*i+1], "-") {
		return "", false
	}
	*i = *i + 1
	return args[*i], true
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "-help" || arg == "--help"
}

func withIODefaults(commandIO cli.IO) cli.IO {
	if commandIO.Stdin == nil {
		commandIO.Stdin = bytes.NewReader(nil)
	}
	if commandIO.Stdout == nil {
		commandIO.Stdout = io.Discard
	}
	if commandIO.Stderr == nil {
		commandIO.Stderr = io.Discard
	}
	return commandIO
}
