# gwatch
GO commandline utility for running a command when an event occurs, with support for simple replacements.
Any number of positional directories can be specified, and they are recursively watched.

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

## Help Text 
```
Usage: gwatch [OPTIONS]... [PATHS]...
  -command string (default "echo %e %f")
	The command to run when the file changes.
	The command is invoked like "bash -c '<command>'"
	Simple string replacement is supported to identify what happened:
	  %e: The mask of events that was triggered (e.g. CHMOD|WRITE is possible)
	  %f: Relative path to the file
	Shell control characters are quoted when macros are expanded, so a file named a&b will expand to a\&b etc.
	No escaping is done on the provided command.
  -eventMask string (default "Create|Write|Remove|Rename|Chmod")
	Mask of events to watch.
  -mode string (default "Concurrent")
	Event backlog behavior.
	Concurrent: 
	    Run the command concurrently whenever an event is fired, even if previous handlers has not yet finished 
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

        If a file is watched, events will only be generated for that file. In other words, if the file is deleted,
        and a new file is created with the same name, events will not be generated for the new file.
        For the same reason, it is not possible to watch a file which does not yet exist.
        This restriction does not apply if instead the directory containing the file is being watched.
```
