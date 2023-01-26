package main

import (
	"flag"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/operdies/gwatch/pkg/watcher"
)

var Command string

func OnChange(op fsnotify.Op, path string) {
	if Command == "" {
		return
	}

	opname := watcher.Sanitize(op.String())
	filename := watcher.Sanitize(path)
	c := strings.ReplaceAll(Command, "%f", filename)
	c = strings.ReplaceAll(c, "%e", opname)

	cmd := exec.Command("bash", "-c", c)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error running command %v: %v\n", c, err)
	}
	fmt.Print(string(out))
}

func main() {
	// TODO: Add options
	// Grace period -- how often should we propagate events
	// Block/Allowlist of eventtypes
	// 1. "I only care about WRITE"
	// 2. "I don't care about CHMOD"
	var help = flag.Bool("help", false, "Show help")
	var includeHidden = flag.Bool("include-hidden", false, "Recurse into hidden directories")

	log.SetFlags(0)
	flag.StringVar(&Command, "command", "", "The command to run when the file changes")
	flag.Parse()
	items := flag.Args()

	if *help || len(items) == 0 || Command == "" {
		flag.Usage()
		return
	}

	watcher.WatchItems(items, *includeHidden, OnChange)
}
