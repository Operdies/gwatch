package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/operdies/gwatch/pkg/executor"
	"github.com/operdies/gwatch/pkg/watcher"
)

var options watcher.Options

func parseEventMask(mask string) (op fsnotify.Op, err error) {
	parts := strings.Split(mask, "|")
	for _, part := range parts {
		p := strings.TrimSpace(part)
		p = strings.ToLower(p)

		if p == "create" {
			op |= fsnotify.Create
		} else if p == "write" {
			op |= fsnotify.Write
		} else if p == "remove" {
			op |= fsnotify.Remove
		} else if p == "rename" {
			op |= fsnotify.Rename
		} else if p == "chmod" {
			op |= fsnotify.Chmod
		} else {
			err = fmt.Errorf("No such event '%v'", part)
			return
		}
	}
	return op, err
}

func usage() {
	fmt.Printf("Usage: gwatch [OPTIONS]... [PATHS]...\n")
	deferred := make([]string, 0)
	flag.VisitAll(func(f *flag.Flag) {
		if f.Name == "help" {
			return
		}
		typeName, usage := flag.UnquoteUsage(f)
		lines := strings.Split(usage, "\n")
		usage = strings.Join(lines, "\n\t")
		defaultValue := f.DefValue
		if typeName == "string" {
			defaultValue = fmt.Sprintf(`"%v"`, defaultValue)
		}
		if typeName == "" { // bool
			// Show boolean flags last
			deferred = append(deferred, fmt.Sprintf("  -%v\n\t%v\n", f.Name, usage))
		} else {
			fmt.Printf("  -%v %v (default %v)\n\t%v\n", f.Name, typeName, defaultValue, usage)
		}
	})
	for _, def := range deferred {
		fmt.Print(def)
	}
	fmt.Print(`  -help
        Show this help text
`)
	fmt.Print(`  PATHS strings (default ".")
        Any number of paths can be watched. 
        Watching a directory will cause all subdirectories to be watched recursively.
        Creating new subdirectories in a watched directory will automatically add the new directories to the watch list.
        When files are added and removed from watched directories, they are also automatically added and removed from the watch list.

        The globbing wildcards * and ** are supported. Any PATH which is not a directory is interpreted as a pattern.
        If no patterns are supplied, everything is considered a match. Otherwise only files which match at least one pattern are included.
        If calling from a shell, remember to single quote patterns. Otherwise the shell will expand them before starting the program.`)
}

func main() {
	var help = flag.Bool("help", false, "Show this help message")
	var includeHidden = flag.Bool("include-hidden", false, "Recurse into hidden directories")
	var queueSize = flag.Int("queue-size", 1, "The maximum number of queued events. Old events will be evicted.")

	var mode string
	flag.StringVar(&mode, "mode", "Concurrent", `Event backlog behavior.
Concurrent: 
    Run the command concurrently whenever an event is fired, even if previous handlers have not yet finished 
Queue: 
    Queue events and run the handler in sequence 
Block: 
    Ignore events while the handler is running, and resume listening to events when it finishes. 
    This is a special case of 'Queue' with a queue size of 0
Kill: 
    Stop the currently running command when a new event arrives, and run the new one`)
	var eventString string

	flag.StringVar(&eventString, "eventMask", "Create|Write|Remove|Rename|Chmod", "Mask of events to watch.")

	var Command string
	flag.StringVar(&Command, "command", "echo %e %f",
		`The command to run when a file changes. Invoked as "bash -c '<command>'"
Simple string replacement is supported to respond to what happened:
  %e: The mask of events that were triggered (e.g. CHMOD|WRITE is possible)
  %f: Relative path to the file
Shell control characters are quoted when macros are expanded, so a file named a&b will expand to a\&b etc.
No escaping is done on the provided command.`)

	flag.Usage = usage

	flag.Parse()
	items := flag.Args()

	if len(items) == 0 {
		items = append(items, ".")
	}

	if *help || Command == "" {
		flag.Usage()
		return
	}

	m, err := watcher.GetMode(mode)
	if err != nil {
		panic(err)
	}

	eventMask, err := parseEventMask(eventString)
	if err != nil {
		panic(err)
	}

	options = watcher.Options{
		QueueSize:     *queueSize,
		IncludeHidden: *includeHidden,
		Mode:          m,
		EventMask:     eventMask,
	}

	exec := executor.Create(Command)
	err = watcher.WatchItems(items, &options, exec.Execute)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
