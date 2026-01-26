package zns

import (
	"log"
	"os"
	"time"
)

type FileLoader struct {
	Interval time.Duration

	lastMod time.Time

	t *time.Ticker

	watchers []struct {
		file string
		cb   func(file string)
	}
}

func (l *FileLoader) load() {
	for _, w := range l.watchers {
		info, err := os.Stat(w.file)
		if err != nil {
			log.Panicln("FileLoader load error:", err)
			continue
		}
		lastMod := info.ModTime()
		if !lastMod.After(l.lastMod) {
			return
		}
		go w.cb(w.file)
		l.lastMod = lastMod
	}
}

func (l *FileLoader) Watch(file string, cb func(file string)) {
	l.watchers = append(l.watchers, struct {
		file string
		cb   func(file string)
	}{
		file: file,
		cb:   cb,
	})
}

func (l *FileLoader) Stop() {
	l.t.Stop()
}

func (l *FileLoader) Start() {
	l.load()
	go l.Loop()
}

func (l *FileLoader) Loop() {
	if l.Interval <= 0 {
		l.Interval = 1 * time.Minute
	}

	l.t = time.NewTicker(l.Interval)
	for range l.t.C {
		l.load()
	}
}
