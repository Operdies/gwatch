# gwatch
CLI utility for running a command when an event occurs.
Any number of files and directories can be specified, and they are recursively watched.

This utility is made possible by the excellent [fsnotify](https://github.com/fsnotify/fsnotify) 

## Example Usage:
```bash
$ gwatch -command 'echo %e %f' . &
$ echo hello > a
CREATE a
$ echo hello >> a
WRITE a
$ rm a
REMOVE a
$ touch b
CREATE b
```

## Install 
```bash 
$ go install github.com/operdies/gwatch@latest
```

## Usage
```
Usage: gwatch [OPTIONS]... [PATHS]...
  -command string (default "echo %e %f")
	The command to run when a file changes. Invoked as "bash -c '<command>'"
	Simple string replacement is supported to respond to what happened:
	  %e: The mask of events that were triggered (e.g. CHMOD|WRITE is possible)
	  %f: Relative path to the file
	Shell control characters are quoted when macros are expanded, so a file named a&b will expand to a\&b etc.
	No escaping is done on the provided command.
  -eventMask string (default "Create|Write|Remove|Rename|Chmod")
	Mask of events to watch.
  -mode string (default "Concurrent")
	Event backlog behavior.
	Concurrent: 
	    Run the command concurrently whenever an event is fired, even if previous handlers have not yet finished 
	Queue: 
	    Queue events and run the handler in sequence 
	Block: 
	    Ignore events while the handler is running, and resume listening to events when it finishes. 
	    This is a special case of 'Queue' with a queue size of 0
	Kill: 
	    Stop the currently running command when a new event arrives, and run the new one
  -queue-size int (default 1)
	The maximum number of queued events. Old events will be evicted.
  -include-hidden
	Recurse into hidden directories
  -help
        Show this help text
  PATHS strings (default ".")
        Any number of paths can be watched. 
        Watching a directory will cause all subdirectories to be watched recursively.
        Creating new subdirectories in a watched directory will automatically add the new directories to the watch list.
        When files are added and removed from watched directories, they are also automatically added and removed from the watch list.

        The globbing wildcards * and ** are supported. Any PATH which is not a directory is interpreted as a pattern.
        If no patterns are supplied, everything is considered a match. Otherwise only files which match at least one pattern are included.
        If calling from a shell, remember to single quote patterns. Otherwise the shell will expand them before starting the program.
```

# Examples 

Check out the [examples](./examples/).

