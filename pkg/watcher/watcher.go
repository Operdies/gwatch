package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/exp/slices"
)

var (
	// The handler is run concurrently when new events occur.
	Concurrent Mode = Mode{mode: 0}
	// Events are queued, and the handler is invoked in the order the events occur.
	// A slow handler will cause events to fill indefinitely, unless a queue size is set.
	Queue Mode = Mode{mode: 1}
	// New events are blocked until the current handler has finished.
	Block Mode = Mode{mode: 2}
	// The context for the current handler is cancelled, and a new event is fired.
	// If the handler doesn't properly handle the cancel event, this has the same behavior
	// as a concurrent.
	Kill Mode = Mode{mode: 3}
)

type Mode struct {
	mode int
}

type EmitterOption = int

const (
	Emit   EmitterOption = 1
	NoEmit EmitterOption = 2
)

func (m Mode) String() string {
	if m == Concurrent {
		return "Concurrent"
	}
	if m == Queue {
		return "Queue"
	}
	if m == Block {
		return "Block"
	}
	if m == Kill {
		return "Kill"
	}
	return "Error"
}

func GetMode(m string) (mode Mode, err error) {
	if strings.EqualFold(m, Concurrent.String()) {
		mode = Concurrent
	} else if strings.EqualFold(m, Queue.String()) {
		mode = Queue
	} else if strings.EqualFold(m, Block.String()) {
		mode = Block
	} else if strings.EqualFold(m, Kill.String()) {
		mode = Kill
	} else {
		err = fmt.Errorf("No such mode '%v'", m)
	}
	return mode, err
}

type Handler func(op fsnotify.Op, path string, ctx context.Context)

type Options struct {
	// Only valid if Mode is Queue
	QueueSize int
	// Set the behavior when multiple events occur before the callback has finished
	Mode Mode
	// Whether or not to watch hidden files and directories
	IncludeHidden bool
	// Mask of events to watch
	EventMask fsnotify.Op
}

func (options *Options) String() string {
	if options.Mode == Queue {
		return fmt.Sprintf(`QueueSize: %v
Mode: %v 
IncludeHidden: %v 
Events: %v`,
			options.QueueSize,
			options.Mode.String(),
			options.IncludeHidden,
			options.EventMask.String())
	} else {
		return fmt.Sprintf(`Mode: %v 
IncludeHidden: %v 
Events: %v`,
			options.Mode.String(),
			options.IncludeHidden,
			options.EventMask.String())
	}
}

func isDir(item string) bool {
	stat, err := os.Stat(item)
	if err != nil {
		return false
	}
	return stat.IsDir()
}

func fileExists(file string) bool {
	_, err := os.Stat(file)
	return err == nil
}

func (w *WatchedDir) walkAndAddAll(root string, emit EmitterOption) {
	if isDir(root) {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			link, err := os.Readlink(path)
			if err == nil {
				if slices.Contains(w.links, link) {
					return nil
				}
				w.links = append(w.links, link)
				w.walkAndAddAll(link, emit)
				return nil
			}

			if w.options.IncludeHidden == false {
				if w.isHidden(d.Name()) && path != root {
					if d.IsDir() {
						return fs.SkipDir
					} else {
						return nil
					}
				}
			}

			if d.IsDir() {
				err := w.watcher.Add(path)
				if err != nil {
					fmt.Printf("Error watching '%v': %v\n", path, err.Error())
				}
			}

			if path != root && emit == Emit {
				w.watcher.Events <- fsnotify.Event{Op: fsnotify.Create, Name: path}
			}

			return nil
		})
	} else if fileExists(root) { // verify the file hasn't already been deleted
		err := w.watcher.Add(root)
		if err != nil {
			fmt.Printf("Error watching '%v': %v\n", root, err.Error())
		}
	}

	// Sort the links by length. Otherwise we can encounter edge-cases in w.isHidden
	// where files from nested symbolic links are incorrectly treated as hidden
	slices.SortFunc(w.links, func(a, b string) bool { return len(a) > len(b) })
}

func stripPathChars(s string) string {
	if len(s) > 2 && s[:2] == "./" {
		return s[2:]
	}
	return s
}

func Create(paths []string, options *Options) *WatchedDir {
	var w = WatchedDir{fileChanged: make(chan fsnotify.Event), options: options, paths: paths}
	w.watcher, _ = fsnotify.NewWatcher()
	patterns := make([]string, 0)

	anyPaths := false
	for _, p := range paths {
		if isDir(p) {
			w.walkAndAddAll(p, NoEmit)
			anyPaths = true
		} else {
			stripped := stripPathChars(p)
			// If the path contains any wildcards, interpret it as a pattern
			patterns = append(patterns, stripped)
		}
	}

	// If a pattern starts with a '/', it could be rooted outside of the working directory
	// In order to get such events, we should watch the root directory of the pattern
	// E.g. a pattern such as /hello/world/**/abc.txt should watch /hello/world
	for _, p := range patterns {
		if len(p) > 0 && p[0] == '/' {
			segments := strings.Split(p, "/")
			cnt := len(segments) - 1
			for i, s := range segments {
				if strings.Contains(s, "*") {
					cnt = i
					break
				}
			}
			toWatch := "/" + path.Join(segments[:cnt]...)
			w.walkAndAddAll(toWatch, NoEmit)
		}
	}

	// If a pattern was specified, but no paths were, recursively walk the current directory.
	if anyPaths == false {
		w.walkAndAddAll(".", NoEmit)
	}

	if len(patterns) == 0 {
		patterns = append(patterns, "**")
	}
	w.patterns = patterns
	// Put the shortest patterns first, as these are likely more generic
	slices.SortFunc(w.patterns, func(a, b string) bool { return len(a) < len(b) })
	// Put the longest paths first for future hidden checks
	slices.SortFunc(w.paths, func(a, b string) bool { return len(a) > len(b) })
	return &w
}

func (w *WatchedDir) isHidden(name string) bool {
	// A file is considered hidden if the filename itself,
	// or any of its parent directories start with a '.'
	isHidden := func(name string) bool {
		segments := strings.Split(name, "/")
		for _, n := range segments {
			if len(n) > 1 && n[0] == '.' {
				return true
			}
		}
		return false
	}

	// If a hidden directory was added directly, we should not
	// consider all files inside of it hidden.
	for _, p := range w.paths {
		if strings.HasPrefix(name, p) {
			return isHidden(name[len(p):])
		}
	}

	// If this file was added by following a symbolic link,
	// strip their common prefix in order to not treat it as hidden.
	// This is for edge cases where a symbolic link was followed
	// into a hidden directory, where the file itself should not be hidden
	for _, l := range w.links {
		if strings.HasPrefix(name, l) {
			return isHidden(name[len(l):])
		}
	}

	return isHidden(name)
}

// Primitive glob matcher that understands * to mean 'anything but slash' and ** to mean 'anything'
func match(pattern, input []byte) bool {
	// If both strings are exhausted, we have a match
	if len(pattern) == 0 && len(input) == 0 {
		return true
	}

	// If either string is exhausted, there is no match
	if len(pattern) == 0 || len(input) == 0 {
		return false
	}

	if pattern[0] == '*' {
		if len(pattern) > 1 && pattern[1] == '*' {
			// ** -- match anything
			for i := range input {
				// Check in reverse. This is optimistic.
				// The idea is to consume as many characters as possible since typical patterns are expected to be patterns like '*.go'
				if match(pattern[2:], input[len(input)-i:]) {
					return true
				}
			}

			// Handle the case where * matches nothing
			if match(pattern[2:], input) {
				return true
			}

		} else {
			// * -- match anything but forward slash

			// Handle the case where * matches nothing
			if match(pattern[1:], input) {
				return true
			}

			for i, c := range input {
				if c == '/' {
					return false
				}
				if match(pattern[1:], input[i+1:]) {
					return true
				}
			}
		}
	}

	if pattern[0] == input[0] {
		return match(pattern[1:], input[1:])
	}

	return false
}

func (w *WatchedDir) anyMatch(input string) bool {
	for _, pattern := range w.patterns {
		if match([]byte(pattern), []byte(input)) {
			return true
		}
	}
	return false
}

func (w *WatchedDir) watchEvents() {
	for evt := range w.watcher.Events {
		if w.options.IncludeHidden == false && w.isHidden(evt.Name) {
			continue
		}
		// Sometimes duplicate events are emitted, e.g.
		// 1: REMOVE ./a
		// 2: REMOVE a
		evt.Name = stripPathChars(evt.Name)

		if evt.Op.Has(fsnotify.Create) && isDir(evt.Name) {
			go func() {
				// This is for the edge case where e.g. a hierarchy is created like `mkdir -p a/b/c`, and b/c appears before the watcher has started watching `a`
				time.Sleep(time.Millisecond * 100)
				w.walkAndAddAll(evt.Name, Emit)
			}()
		}
		if w.options.EventMask.Has(evt.Op) {
			// filter any event which doesn't match.
			if w.anyMatch(evt.Name) == false {
				continue
			}

			w.fileChanged <- evt
		}
	}
}

type WatchedDir struct {
	watcher     *fsnotify.Watcher
	fileChanged chan fsnotify.Event
	options     *Options
	// The function which is invoked when an event occurs
	handler  Handler
	patterns []string
	// Maintain a list of links that were followed to prevent an infinite loop
	links []string
	// Keep a list of originally added paths
	paths []string
}

func Sanitize(s string) string {
	charset := []rune{'\\', '|', '&', '$', '!', ';', '{', '}', '(', ')', '?', '+', '<', '>', '\'', '"', '~', '`', '*', '#', '[', ']'}
	for _, c := range charset {
		s = strings.ReplaceAll(s, string(c), `\`+string(c))
	}
	return s
}

func (w *WatchedDir) nextEvent() (evt fsnotify.Event, err error) {
	select {
	case err = <-w.watcher.Errors:
	case evt = <-w.fileChanged:
	}
	return evt, err
}

func concurrentDispatcher(w *WatchedDir) error {
	for {
		evt, err := w.nextEvent()
		if err != nil {
			return err
		}
		go w.handler(evt.Op, evt.Name, context.Background())
	}
}

func killDispatcher(w *WatchedDir) error {
	ctx, cancel := context.WithCancel(context.Background())
	for {
		evt, err := w.nextEvent()
		cancel()
		if err != nil {
			return err
		}
		ctx, cancel = context.WithCancel(context.Background())
		go w.handler(evt.Op, evt.Name, ctx)
	}
}

func queueDispatcher(w *WatchedDir, n int) (err error) {
	queue := make(chan fsnotify.Event, n)
	go func() {
		var evt fsnotify.Event
		for {
			evt, err = w.nextEvent()
			if err != nil {
				close(queue)
				return
			}
			select {
			// If the buffer is not full, add the new event
			case queue <- evt:
				// If the buffer is full, evict the oldest event and add the new one
			default:
				<-queue
				queue <- evt
			}
		}
	}()

	for evt := range queue {
		w.handler(evt.Op, evt.Name, context.Background())
	}
	return
}

func blockingDispatcher(w *WatchedDir) error {
	mut := sync.Mutex{}
	for {
		evt, err := w.nextEvent()
		if err != nil {
			return err
		}
		if mut.TryLock() {
			e := evt
			go func() {
				w.handler(e.Op, e.Name, context.Background())
				mut.Unlock()
			}()
		}
	}
}

func WatchItems(items []string, options *Options, handler Handler) (err error) {
	if options == nil {
		panic("No options set.")
	}

	watcher := Create(items, options)
	watcher.handler = handler
	go watcher.watchEvents()

	switch options.Mode {
	case Concurrent:
		err = concurrentDispatcher(watcher)
	case Kill:
		err = killDispatcher(watcher)
	case Block:
		err = blockingDispatcher(watcher)
	case Queue:
		err = queueDispatcher(watcher, options.QueueSize)
	}
	return
}
