package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// ErrUsage reports command-line usage errors.
var ErrUsage = errors.New("usage error")

// ErrHelp requests command help.
var ErrHelp = errors.New("help requested")

// IO contains command input and output streams.
type IO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (commandIO IO) withDefaults() IO {
	if commandIO.Stdin == nil {
		commandIO.Stdin = strings.NewReader("")
	}
	if commandIO.Stdout == nil {
		commandIO.Stdout = io.Discard
	}
	if commandIO.Stderr == nil {
		commandIO.Stderr = io.Discard
	}
	return commandIO
}

// Command is one application command.
type Command struct {
	Name    string
	Summary string
	Usage   string
	Run     func(context.Context, IO, []string) error
}

// Program dispatches application commands.
type Program struct {
	name     string
	commands map[string]Command
	io       IO
}

// Option configures a Program.
type Option func(*Program)

// WithIO configures the program streams.
func WithIO(commandIO IO) Option {
	return func(program *Program) {
		if commandIO.Stdin != nil {
			program.io.Stdin = commandIO.Stdin
		}
		if commandIO.Stdout != nil {
			program.io.Stdout = commandIO.Stdout
		}
		if commandIO.Stderr != nil {
			program.io.Stderr = commandIO.Stderr
		}
	}
}

// New creates a command program.
func New(name string, commands []Command, opts ...Option) *Program {
	program := &Program{
		name:     name,
		commands: make(map[string]Command, len(commands)),
		io: IO{
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		},
	}
	for _, command := range commands {
		if command.Name == "" {
			continue
		}
		program.commands[command.Name] = command
	}
	for _, opt := range opts {
		opt(program)
	}
	return program
}

// Run dispatches args to a command.
func (p *Program) Run(ctx context.Context, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	commandIO := p.io.withDefaults()

	if len(args) == 0 {
		p.writeHelp(commandIO.Stderr)
		return ErrUsage
	}

	commandName := args[0]
	if isHelpArg(commandName) || commandName == "help" {
		p.writeHelp(commandIO.Stdout)
		return nil
	}

	command, ok := p.commands[commandName]
	if !ok {
		fmt.Fprintf(commandIO.Stderr, "%s: unknown command %q\n\n", p.name, commandName)
		p.writeHelp(commandIO.Stderr)
		return fmt.Errorf("%w: unknown command %q", ErrUsage, commandName)
	}

	commandArgs := args[1:]
	if len(commandArgs) > 0 && isHelpArg(commandArgs[0]) {
		writeCommandHelp(commandIO.Stdout, p.name, command)
		return nil
	}

	if command.Run == nil {
		return fmt.Errorf("command %q has no runner", command.Name)
	}
	if err := command.Run(ctx, commandIO, commandArgs); err != nil {
		if errors.Is(err, ErrHelp) {
			writeCommandHelp(commandIO.Stdout, p.name, command)
			return nil
		}
		return fmt.Errorf("run %s: %w", command.Name, err)
	}
	return nil
}

func (p *Program) writeHelp(w io.Writer) {
	fmt.Fprintf(w, "Usage: %s <command> [args]\n\n", p.name)
	fmt.Fprintln(w, "Commands:")
	for _, command := range p.sortedCommands() {
		if command.Summary == "" {
			fmt.Fprintf(w, "  %s\n", command.Name)
			continue
		}
		fmt.Fprintf(w, "  %-12s %s\n", command.Name, command.Summary)
	}
}

func (p *Program) sortedCommands() []Command {
	commands := make([]Command, 0, len(p.commands))
	for _, command := range p.commands {
		commands = append(commands, command)
	}
	slices.SortFunc(commands, func(a Command, b Command) int {
		return strings.Compare(a.Name, b.Name)
	})
	return commands
}

func writeCommandHelp(w io.Writer, programName string, command Command) {
	usage := command.Usage
	if usage == "" {
		usage = command.Name
	}
	fmt.Fprintf(w, "Usage: %s %s\n", programName, usage)
	if command.Summary != "" {
		fmt.Fprintf(w, "\n%s\n", command.Summary)
	}
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "-help" || arg == "--help"
}
