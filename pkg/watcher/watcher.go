package watcher

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

func (w *WatchedDir) walkAndAddAll(root string, emit bool) {
	if isDir(root) {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				if w.includeHidden == false && isHidden(d.Name()) {
					return nil
				}
				err := w.watcher.Add(path)
				if err != nil {
					log.Printf("Error adding watch: %v", err.Error())
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
			log.Printf("Error adding watch: %v", err.Error())
		}
	}
}

func Create(paths []string) *WatchedDir {
	var w = WatchedDir{fileChanged: make(chan fsnotify.Event)}
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
		if w.includeHidden == false && isHidden(evt.Name) {
			continue
		}
		w.fileChanged <- evt
	}
}

type WatchedDir struct {
	isIndexed     bool
	watcher       *fsnotify.Watcher
	fileChanged   chan fsnotify.Event
	includeHidden bool
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

func WatchItems(items []string, includeHidden bool, onChange func(op fsnotify.Op, path string)) {
	watcher := Create(items)
	watcher.includeHidden = includeHidden
	go watcher.watchEvents()

	watchedMap := make(map[string]map[fsnotify.Op]time.Time)

	for evt := range watcher.fileChanged {
		// Sometimes duplicate events are emitted, e.g.
		// 1: REMOVE ./a
		// 2: REMOVE a
		// Let's just always remove leading './'
		if len(evt.Name) > 2 && evt.Name[:2] == "./" {
			evt.Name = evt.Name[2:]
		}
		var map1 map[fsnotify.Op]time.Time
		var t time.Time
		var ok bool

		if map1, ok = watchedMap[evt.Name]; !ok {
			map1 = make(map[fsnotify.Op]time.Time)
			watchedMap[evt.Name] = map1
		}
		if t, ok = map1[evt.Op]; !ok {
			t = time.Now()
			map1[evt.Op] = t
		}

		elapsed := time.Now().Sub(t)
		// Don't propagate an event if an identical one was propagated less than 10ms ago
		if ok && elapsed < time.Millisecond*1000 {
			continue
		}

		map1[evt.Op] = time.Now()

		if evt.Op.Has(fsnotify.Create) && isDir(evt.Name) {
			go func() {
				// This is for the edge case where e.g. a hierarchy is created like `mkdir -p a/b/c`, and b/c appears before the watcher has started watching `a`
				time.Sleep(time.Millisecond * 100)
				watcher.walkAndAddAll(evt.Name, true)
			}()
		}

		onChange(evt.Op, evt.Name)
	}
}
