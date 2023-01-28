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

func (m Mode) ToString() string {
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
	if m == Concurrent.ToString() {
		mode = Concurrent
	} else if m == Queue.ToString() {
		mode = Queue
	} else if m == Block.ToString() {
		mode = Block
	} else if m == Kill.ToString() {
		mode = Kill
	} else {
		err = fmt.Errorf("No such mode '%v'", m)
	}
	return mode, err
}

type Options struct {
	// Only valid if Mode is Queue
	QueueSize int
	// Set the behavior when multiple events occur before the callback has finished
	Mode Mode
	// Whether or not to watch hidden files and directories
	IncludeHidden bool
	// The function which is invoked when an event occurs
	Handler func(op fsnotify.Op, path string, ctx context.Context)
	// Mask of events to watch
	EventMask fsnotify.Op
}

func (w *WatchedDir) walkAndAddAll(root string, emit bool) {
	if isDir(root) {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				if w.options.IncludeHidden == false && isHidden(d.Name()) {
					return nil
				}
				err := w.watcher.Add(path)
				if err != nil {
					fmt.Printf("Error adding watch: %v", err.Error())
				}
			}

			if path != root && emit {
				w.watcher.Events <- fsnotify.Event{Op: fsnotify.Create, Name: path}
			}

			return nil
		})
	} else {
		err := w.watcher.Add(root)
		if err != nil {
			fmt.Printf("Error adding watch: %v", err.Error())
		}
	}
}

func Create(paths []string, options *Options) *WatchedDir {
	var w = WatchedDir{fileChanged: make(chan fsnotify.Event), options: options}
	w.watcher, _ = fsnotify.NewWatcher()
	for _, p := range paths {
		w.walkAndAddAll(p, false)
	}
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

func (w *WatchedDir) watchEvents() {
	for evt := range w.watcher.Events {
		if w.options.IncludeHidden == false && isHidden(evt.Name) {
			continue
		}
		// Sometimes duplicate events are emitted, e.g.
		// 1: REMOVE ./a
		// 2: REMOVE a
		// Let's just always remove leading './'
		if len(evt.Name) > 2 && evt.Name[:2] == "./" {
			evt.Name = evt.Name[2:]
		}
		if evt.Op.Has(fsnotify.Create) && isDir(evt.Name) {
			go func() {
				// This is for the edge case where e.g. a hierarchy is created like `mkdir -p a/b/c`, and b/c appears before the watcher has started watching `a`
				time.Sleep(time.Millisecond * 100)
				w.walkAndAddAll(evt.Name, true)
			}()
		}
		if w.options.EventMask.Has(evt.Op) {
			w.fileChanged <- evt
		}
	}
}

type WatchedDir struct {
	watcher     *fsnotify.Watcher
	fileChanged chan fsnotify.Event
	options     *Options
}

func Sanitize(s string) string {
	charset := []rune{'\\', '|', '&', '$', '!', ';', '{', '}', '(', ')', '?', '+', '<', '>', '\'', '"', '~', '`', '*', '#', '[', ']'}
	for _, c := range charset {
		s = strings.ReplaceAll(s, string(c), `\`+string(c))
	}
	return s
}

func isDir(item string) bool {
	stat, err := os.Stat(item)
	if err != nil {
		return false
	}
	return stat.IsDir()
}

func concurrentDispatcher(w *WatchedDir) {
	for evt := range w.fileChanged {
		go w.options.Handler(evt.Op, evt.Name, context.Background())
	}
}

func killDispatcher(w *WatchedDir) {
	ctx, cancel := context.WithCancel(context.Background())
	for evt := range w.fileChanged {
		cancel()
		ctx, cancel = context.WithCancel(context.Background())
		go w.options.Handler(evt.Op, evt.Name, ctx)
	}
	cancel()
}

func queueDispatcher(w *WatchedDir, n int) {
	ch := make(chan fsnotify.Event, n)
	go func() {
		for evt := range w.fileChanged {
			select {
			// If the buffer is not full, add the new event
			case ch <- evt:
				// If the buffer is full, evict the oldest event and add the new one
			default:
				<-ch
				ch <- evt
			}
		}
	}()

	for evt := range ch {
		w.options.Handler(evt.Op, evt.Name, context.Background())
	}
}

func blockingDispatcher(w *WatchedDir) {
	mut := sync.Mutex{}
	for evt := range w.fileChanged {
		if mut.TryLock() {
			e := evt
			go func() {
				w.options.Handler(e.Op, e.Name, context.Background())
				mut.Unlock()
			}()
		}
	}
}

func WatchItems(items []string, options *Options) {
	if options == nil {
		panic("No options set.")
	}
	if options.Handler == nil {
		panic("No handler set.")
	}

	watcher := Create(items, options)
	go watcher.watchEvents()

	switch options.Mode {
	case Concurrent:
		concurrentDispatcher(watcher)
	case Kill:
		killDispatcher(watcher)
	case Block:
		blockingDispatcher(watcher)
	case Queue:
		queueDispatcher(watcher, options.QueueSize)
	}
}
