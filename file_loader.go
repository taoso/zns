package zns

import (
	"log"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
)

type watcher struct {
	file string
	cb   func(file string)
}

type FileLoader struct {
	lastMod time.Time

	t *time.Ticker

	watchers []watcher

	fsWatcher *fsnotify.Watcher
}

func NewFileLoader() *FileLoader {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Panicln("FileLoader NewFileLoader error:", err)
	}
	return &FileLoader{
		fsWatcher: watcher,
	}
}

func (l *FileLoader) load() {
	for _, w := range l.watchers {
		if err := l.loadOne(w); err != nil {
			log.Panicln("FileLoader load error:", err)
		}
	}
}

func (l *FileLoader) loadOne(w watcher) error {
	info, err := os.Stat(w.file)
	if err != nil {
		return err
	}
	lastMod := info.ModTime()
	if !lastMod.After(l.lastMod) {
		return nil
	}
	go w.cb(w.file)
	l.lastMod = lastMod
	return nil
}

func (l *FileLoader) Watch(file string, cb func(file string)) {
	l.watchers = append(l.watchers, watcher{
		file: file,
		cb:   cb,
	})

	l.fsWatcher.Add(file)
}

func (l *FileLoader) Stop() {
	l.t.Stop()
}

func (l *FileLoader) Start() {
	l.load()
	go l.Loop()
}

func (l *FileLoader) Loop() {
	l.t = time.NewTicker(10 * time.Minute)
	defer l.t.Stop()

	for {
		select {
		case <-l.t.C:
			l.load()
		case event, ok := <-l.fsWatcher.Events:
			if !ok {
				return
			}

			eventPath := event.Name

			for _, w := range l.watchers {
				if eventPath == w.file {
					l.loadOne(w)
				}
			}
		case err, ok := <-l.fsWatcher.Errors:
			if !ok {
				return
			}
			log.Println("FileLoader fsWatcher error:", err)
		}
	}
}
