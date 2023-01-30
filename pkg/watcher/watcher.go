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
	// The function which is invoked when an event occurs
	handler Handler
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
