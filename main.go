package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type WatchedDir struct {
	files       map[string]string
	root        string
	recursive   bool
	isIndexed   bool
	indexLock   sync.Mutex
	watcher     *fsnotify.Watcher
	fileChanged chan fsnotify.Event
}

var _extensions []string

func SetExtentions(s []string) { _extensions = s }

func (w *WatchedDir) Files() map[string]string {
	return w.files
}

func (w *WatchedDir) watchEvents() {
	for evt := range w.watcher.Events {
		if evt.Op == fsnotify.Create || evt.Op == fsnotify.Remove {
			w.indexLock.Lock()
			key := getKey(evt.Name)
			_, exists := w.files[key]
			// Overwrite if it exists
			if evt.Op == fsnotify.Create {
				w.files[key] = evt.Name
			} else if evt.Op == fsnotify.Remove && exists {
				delete(w.files, key)
			}
			w.indexLock.Unlock()
		}
		w.fileChanged <- evt
	}
}

// Waits for indexing to complete if an indexing operation is running
// Otherwise return immediately
func (w *WatchedDir) WaitForIndex() {
	w.indexLock.Lock()
	defer w.indexLock.Unlock()
}

func extLower(s string) string {
	s = filepath.Ext(s)
	if len(s) > 0 {
		return strings.ToLower(s)
	}
	return s
}

func pathEqual(s1, s2 string) bool {
	normalize := func(s string) string {
		s = strings.ToLower(strings.ReplaceAll(s, "\\", "/"))
		return strings.TrimRight(s, "/")
	}

	s1 = normalize(s1)
	s2 = normalize(s2)

	return s1 == s2
}

func (w *WatchedDir) indexFiles() {
	w.indexLock.Lock()
	go func() {
		defer w.indexLock.Unlock()
		items := map[string]string{}
		filepath.WalkDir(w.root, func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				if w.recursive {
					return nil
				} else {
					if pathEqual(w.root, path) {
						return nil
					}
					return filepath.SkipDir
				}
			}
			items[getKey(d.Name())] = path
			return nil
		})
		w.files = items
	}()
}

func getKey(fullname string) string {
	bn := filepath.Base(fullname)
	return bn
}

func Create(root string, recursive, watch bool) *WatchedDir {
	var w = WatchedDir{root: root, recursive: recursive, fileChanged: make(chan fsnotify.Event)}
	watcher, _ := fsnotify.NewWatcher()

	if watch {
		if recursive {
			filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if d.IsDir() {
					err := watcher.Add(path)
					if err != nil {
						log.Printf("Error adding watch: %v", err.Error())
					}
				}
				return nil
			})
		} else {
			watcher.Add(root)
		}
	}

	w.watcher = watcher
	w.indexFiles()
	if watch {
		go w.watchEvents()
	}
	return &w
}

var command = ""

func watchItem(item string) {
	watcher := Create(item, true, true)
	watcher.WaitForIndex()

	for evt := range watcher.fileChanged {
		if evt.Op != fsnotify.Write {
			continue
		}
		log.Printf("evt: %v\n", evt)
		if command != "" {
			c := strings.ReplaceAll(command, "{}", evt.Name)
			cmd := exec.Command("bash", "-c", c)
			log.Printf("Running bash -c '%v'\n", c)
			out, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("Error running command: %v\n", err)
			}
			fmt.Printf(string(out))
		}
	}
}

func main() {
	var help = flag.Bool("help", false, "Show help")
	// var quiet = *flag.Bool("quiet", false, "Disable logging") || *flag.Bool("q", false, "Disable logging")

	log.SetFlags(0)
	flag.StringVar(&command, "command", "", "The command to run when the file changes")
	flag.Parse()
	items := flag.Args()

	if *help || len(items) == 0 || command == "" {
		flag.Usage()
		return
	}

	for _, item := range items {
		go watchItem(item)
	}

	select {}

}
