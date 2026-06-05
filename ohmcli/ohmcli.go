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

var version = "dev"

// Option configures the Ohm framework CLI.
type Option func(*options)

type options struct {
	io         cli.IO
	version    string
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

// WithVersion configures the version printed by the framework CLI.
func WithVersion(version string) Option {
	return func(opts *options) {
		opts.version = version
	}
}

// New returns the Ohm framework CLI program.
func New(opts ...Option) *cli.Program {
	cfg := options{
		version: version,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return cli.New("ohm", []cli.Command{
		generateCommand(),
		newCommand(cfg.ohmVersion),
		versionCommand(cfg.version),
	}, cli.WithIO(cfg.io))
}

func versionCommand(version string) cli.Command {
	return cli.Command{
		Name:    "version",
		Summary: "print the Ohm CLI version",
		Usage:   "version",
		Run: func(_ context.Context, commandIO cli.IO, args []string) error {
			commandIO = withIODefaults(commandIO)
			if len(args) > 0 {
				if isHelpArg(args[0]) {
					return cli.ErrHelp
				}
				return fmt.Errorf("%w: version does not accept arguments", cli.ErrUsage)
			}
			fmt.Fprintln(commandIO.Stdout, version)
			return nil
		},
	}
}

func generateCommand() cli.Command {
	return cli.Command{
		Name:    "generate",
		Summary: "generate application code",
		Usage:   "generate <handler|migration|resource|test-from-replay> [args]",
		Run: func(_ context.Context, commandIO cli.IO, args []string) error {
			commandIO = withIODefaults(commandIO)
			if len(args) == 0 {
				return fmt.Errorf("%w: generate requires a generator name", cli.ErrUsage)
			}
			if isHelpArg(args[0]) {
				return cli.ErrHelp
			}

			switch args[0] {
			case "handler":
				parsed, err := parseHandlerArgs(args[1:])
				if err != nil {
					return err
				}
				result, err := scaffold.GenerateHandler(scaffold.Handler{
					Name: parsed.name,
					Dir:  parsed.dir,
				})
				if err != nil {
					return err
				}
				for _, path := range result.CreatedFiles {
					fmt.Fprintf(commandIO.Stdout, "Created %s\n", path)
				}
				if result.RegisterUpdated {
					fmt.Fprintf(commandIO.Stdout, "Updated %s\n", result.RegisterFile)
				}
				return nil
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
			case "resource":
				parsed, err := parseResourceArgs(args[1:])
				if err != nil {
					return err
				}
				result, err := scaffold.GenerateResource(scaffold.Resource{
					Name:   parsed.name,
					Fields: parsed.fields,
					Dir:    parsed.dir,
				})
				if err != nil {
					return err
				}
				for _, path := range result.CreatedFiles {
					fmt.Fprintf(commandIO.Stdout, "Created %s\n", path)
				}
				if result.RegisterUpdated {
					fmt.Fprintf(commandIO.Stdout, "Updated %s\n", result.RegisterFile)
				}
				return nil
			case "test-from-replay":
				parsed, err := parseReplayTestArgs(args[1:])
				if err != nil {
					return err
				}
				result, err := scaffold.GenerateReplayTest(scaffold.ReplayTest{
					SnapshotPath: parsed.snapshotPath,
					Dir:          parsed.dir,
				})
				if err != nil {
					return err
				}
				fmt.Fprintf(commandIO.Stdout, "Created %s\n", result.CreatedFile)
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

type namedDirArgs struct {
	name string
	dir  string
}

type handlerArgs namedDirArgs

func parseHandlerArgs(args []string) (handlerArgs, error) {
	parsed, err := parseNamedDirArgs("handler", args)
	return handlerArgs(parsed), err
}

type migrationArgs namedDirArgs

func parseMigrationArgs(args []string) (migrationArgs, error) {
	parsed, err := parseNamedDirArgs("migration", args)
	return migrationArgs(parsed), err
}

type resourceArgs struct {
	name   string
	fields []scaffold.ResourceField
	dir    string
}

func parseResourceArgs(args []string) (resourceArgs, error) {
	parsed := resourceArgs{}
	var positionals []string

	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		switch {
		case isHelpArg(arg):
			return resourceArgs{}, cli.ErrHelp
		case arg == "-dir" || arg == "--dir":
			value, ok := nextArg(&args)
			if !ok {
				return resourceArgs{}, fmt.Errorf("%w: %s requires a value", cli.ErrUsage, arg)
			}
			parsed.dir = value
		case strings.HasPrefix(arg, "-dir="):
			parsed.dir = strings.TrimPrefix(arg, "-dir=")
		case strings.HasPrefix(arg, "--dir="):
			parsed.dir = strings.TrimPrefix(arg, "--dir=")
		case strings.HasPrefix(arg, "-"):
			return resourceArgs{}, fmt.Errorf("%w: unknown resource flag %q", cli.ErrUsage, arg)
		default:
			positionals = append(positionals, arg)
		}
	}

	if len(positionals) < 2 {
		return resourceArgs{}, fmt.Errorf("%w: resource requires a name and at least one field", cli.ErrUsage)
	}
	parsed.name = positionals[0]
	for _, fieldArg := range positionals[1:] {
		field, err := parseResourceFieldArg(fieldArg)
		if err != nil {
			return resourceArgs{}, err
		}
		parsed.fields = append(parsed.fields, field)
	}
	return parsed, nil
}

func parseResourceFieldArg(arg string) (scaffold.ResourceField, error) {
	name, fieldType, ok := strings.Cut(arg, ":")
	if !ok || name == "" || fieldType == "" {
		return scaffold.ResourceField{}, fmt.Errorf("%w: resource field %q must use name:type", cli.ErrUsage, arg)
	}
	return scaffold.ResourceField{Name: name, Type: fieldType}, nil
}

type replayTestArgs struct {
	snapshotPath string
	dir          string
}

func parseReplayTestArgs(args []string) (replayTestArgs, error) {
	parsed := replayTestArgs{}
	var positionals []string

	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		switch {
		case isHelpArg(arg):
			return replayTestArgs{}, cli.ErrHelp
		case arg == "-dir" || arg == "--dir":
			value, ok := nextArg(&args)
			if !ok {
				return replayTestArgs{}, fmt.Errorf("%w: %s requires a value", cli.ErrUsage, arg)
			}
			parsed.dir = value
		case strings.HasPrefix(arg, "-dir="):
			parsed.dir = strings.TrimPrefix(arg, "-dir=")
		case strings.HasPrefix(arg, "--dir="):
			parsed.dir = strings.TrimPrefix(arg, "--dir=")
		case strings.HasPrefix(arg, "-"):
			return replayTestArgs{}, fmt.Errorf("%w: unknown test-from-replay flag %q", cli.ErrUsage, arg)
		default:
			positionals = append(positionals, arg)
		}
	}

	if len(positionals) != 1 {
		return replayTestArgs{}, fmt.Errorf("%w: test-from-replay requires exactly one snapshot path", cli.ErrUsage)
	}
	parsed.snapshotPath = positionals[0]
	return parsed, nil
}

func parseNamedDirArgs(generator string, args []string) (namedDirArgs, error) {
	parsed := namedDirArgs{}
	var positionals []string

	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		switch {
		case isHelpArg(arg):
			return namedDirArgs{}, cli.ErrHelp
		case arg == "-dir" || arg == "--dir":
			value, ok := nextArg(&args)
			if !ok {
				return namedDirArgs{}, fmt.Errorf("%w: %s requires a value", cli.ErrUsage, arg)
			}
			parsed.dir = value
		case strings.HasPrefix(arg, "-dir="):
			parsed.dir = strings.TrimPrefix(arg, "-dir=")
		case strings.HasPrefix(arg, "--dir="):
			parsed.dir = strings.TrimPrefix(arg, "--dir=")
		case strings.HasPrefix(arg, "-"):
			return namedDirArgs{}, fmt.Errorf("%w: unknown %s flag %q", cli.ErrUsage, generator, arg)
		default:
			positionals = append(positionals, arg)
		}
	}

	if len(positionals) != 1 {
		return namedDirArgs{}, fmt.Errorf("%w: %s requires exactly one name", cli.ErrUsage, generator)
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

	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		switch {
		case isHelpArg(arg):
			return newArgs{}, cli.ErrHelp
		case arg == "-db" || arg == "--db":
			value, ok := nextArg(&args)
			if !ok {
				return newArgs{}, fmt.Errorf("%w: %s requires a value", cli.ErrUsage, arg)
			}
			parsed.database = scaffold.Database(value)
		case strings.HasPrefix(arg, "-db="):
			parsed.database = scaffold.Database(strings.TrimPrefix(arg, "-db="))
		case strings.HasPrefix(arg, "--db="):
			parsed.database = scaffold.Database(strings.TrimPrefix(arg, "--db="))
		case arg == "-module" || arg == "--module":
			value, ok := nextArg(&args)
			if !ok {
				return newArgs{}, fmt.Errorf("%w: %s requires a value", cli.ErrUsage, arg)
			}
			parsed.module = value
		case strings.HasPrefix(arg, "-module="):
			parsed.module = strings.TrimPrefix(arg, "-module=")
		case strings.HasPrefix(arg, "--module="):
			parsed.module = strings.TrimPrefix(arg, "--module=")
		case arg == "-ohm-version" || arg == "--ohm-version":
			value, ok := nextArg(&args)
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

func nextArg(args *[]string) (string, bool) {
	if len(*args) == 0 || strings.HasPrefix((*args)[0], "-") {
		return "", false
	}
	value := (*args)[0]
	*args = (*args)[1:]
	return value, true
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
