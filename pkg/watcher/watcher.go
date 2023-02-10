package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
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

func (w *WatchedDir) walkAndAddAll(root string, emit bool) {
	if isDir(root) {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {

			if w.options.IncludeHidden == false {
				if isHidden(d.Name()) {
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
					fmt.Printf("Error waching '%v': %v\n", path, err.Error())
				}
			}

			if path != root && emit {
				w.watcher.Events <- fsnotify.Event{Op: fsnotify.Create, Name: path}
			}

			return nil
		})
	} else if fileExists(root) { // verify the file hasn't already been deleted
		err := w.watcher.Add(root)
		if err != nil {
			fmt.Printf("Error waching '%v': %v\n", root, err.Error())
		}
	}
}

func stripPathChars(s string) string {
	if len(s) > 2 && s[:2] == "./" {
		return s[2:]
	}
	return s
}

func Create(paths []string, options *Options) *WatchedDir {
	var w = WatchedDir{fileChanged: make(chan fsnotify.Event), options: options}
	w.watcher, _ = fsnotify.NewWatcher()
	patterns := make([]string, 0)

	anyPaths := false
	for _, p := range paths {
		if isDir(p) {
			w.walkAndAddAll(p, false)
			anyPaths = true
		} else {
			// If the path contains any wildcards, interpret it as a pattern
			patterns = append(patterns, stripPathChars(p))
		}
	}

	// If a pattern was specified, but no paths were, recursively walk the current directory.
	if anyPaths == false {
		w.walkAndAddAll(".", false)
	}

	w.patterns = patterns
	return &w
}

func isHidden(name string) bool {
	segments := strings.Split(name, "/")
	for _, n := range segments {
		if len(n) > 1 && n[0] == '.' {
			return true
		}
	}
	return false
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
		if w.options.IncludeHidden == false && isHidden(evt.Name) {
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
				w.walkAndAddAll(evt.Name, true)
			}()
		}
		if w.options.EventMask.Has(evt.Op) {
			// if any patterns are defined, filter any event which doesn't match.
			if len(w.patterns) > 0 {
				if w.anyMatch(evt.Name) == false {
					continue
				}
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
