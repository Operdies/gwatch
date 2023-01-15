# gwatch
GO commandline utility for running a command when an event occurs, with support for simple replacements.
Any number of positional directories can be specified, and they are recursively watched.
There is a switch to include hidden directories, `-include-hidden`.

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
