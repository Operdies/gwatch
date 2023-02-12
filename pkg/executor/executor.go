package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/operdies/gwatch/pkg/watcher"
)

type Executor struct {
	command string
}

func Create(command string) *Executor {
	if command == "" {
		panic("No command given")
	}
	return &Executor{command: command}
}

func (executor *Executor) Execute(op fsnotify.Op, path string, context context.Context) {
	Command := executor.command

	opname := watcher.Sanitize(op.String())
	filename := watcher.Sanitize(path)
	c := strings.ReplaceAll(Command, "%f", filename)
	c = strings.ReplaceAll(c, "%e", opname)

	cmd := exec.CommandContext(context, "bash", "-c", c)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil && context.Err() == nil {
		switch err.(type) {
		// Ignore ExitErrors. If the action errored, then the stderr output should tell us what's wrong.
		case *exec.ExitError:
			return
		default:
			fmt.Fprintf(os.Stderr, "Error running command: %v\n", err)
		}
	}
}
