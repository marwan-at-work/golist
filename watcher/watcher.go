package watcher

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"marwan.io/golist/cache"
	"marwan.io/golist/driver"
	"marwan.io/golist/hash"
)

// NewService returns a new watcher
func NewService(dc cache.Service, lggr *logrus.Logger) Service {
	s := &service{}
	s.watchers = map[string]*job{}
	if lggr == nil {
		lggr = logrus.New()
	}
	s.lggr = lggr
	s.dc = dc

	return s
}

// Service can watch your directory
// and update the golist results
// if anything changes in your .go files.
type Service interface {
	Watch(cfg *driver.Config) error
	Close() error
}

type service struct {
	watchers map[string]*job
	mu       sync.Mutex
	lggr     *logrus.Logger
	dc       cache.Service
}

func (s *service) Watch(cfg *driver.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := hash.KeyString(cfg)
	j, ok := s.watchers[key]
	if ok {
		s.lggr.Debugf("%v: already has watcher", cfg.Patterns)
		j.requestExtension()
		// TODO: one watcher for all configs
		return nil
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	j = &job{w: w, dc: s.dc}
	j.timer = time.NewTimer(cacheTime)
	j.lggr = s.lggr
	j.deleter = s.close
	j.key = key
	j.cfg = cfg
	j.addFiles()
	j.timer = time.NewTimer(cacheTime)
	j.extension = make(chan struct{})
	s.watchers[key] = j
	go j.runTimer()
	go j.runWatcher()

	return nil
}

func (s *service) Close() error {
	s.mu.Lock()
	for _, j := range s.watchers {
		err := j.w.Close()
		if err != nil {
			return err
		}
	}
	s.watchers = map[string]*job{}
	s.mu.Unlock()
	return nil
}

func (s *service) close(key string, w *fsnotify.Watcher) {
	s.mu.Lock()
	w.Close()
	delete(s.watchers, key)
	s.mu.Unlock()
}

type job struct {
	w         *fsnotify.Watcher
	dc        cache.Service
	key       string
	cfg       *driver.Config
	timer     *time.Timer
	lggr      *logrus.Logger
	deleter   func(key string, w *fsnotify.Watcher)
	extension chan struct{}
}

const cacheTime = time.Hour

func (j *job) extendDeadline() {
	j.lggr.Debugf("%v: extending deadline", j.cfg.Patterns)
	if !j.timer.Stop() {
		<-j.timer.C
	}
	j.timer.Reset(cacheTime)
}

func (j *job) requestExtension() {
	select {
	case j.extension <- struct{}{}:
	default:
	}
}

func (j *job) runTimer() {
	for {
		select {
		case <-j.timer.C:
			j.lggr.Debugf("%v: expired. Removing watcher", j.cfg.Patterns)
			j.deleter(j.key, j.w)
			return
		case <-j.extension:
			j.extendDeadline()
		}
	}
}

func (j *job) runWatcher() {
	for {
		select {
		case event, ok := <-j.w.Events:
			j.lggr.Debugf("GOT EVENT: %v", event.String())
			if !ok {
				return
			}
			j.requestExtension()
			if event.Op&fsnotify.Write == fsnotify.Write {
				j.lggr.Debugf("%v changed. Updating...", event.Name)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
				err := j.dc.Update(ctx, j.cfg)
				if err != nil {
					j.lggr.Errorf("error updating %v: %v", event.Name, err)
				}
				cancel()
			}
			if event.Op&fsnotify.Create == fsnotify.Create {
				// j.w.Add(event.Name)
				// TODO: figure out new files -- maybe don't need them.
			}
			if event.Op&fsnotify.Rename == fsnotify.Rename {
				// TODO: figure out renaming
			}
		case err, ok := <-j.w.Errors:
			if !ok {
				return
			}
			j.lggr.Errorf("WATCHER ERR: %v", err)
		}
	}
}

func (j *job) addFiles() error {
	files := j.parseFiles()

	for _, file := range files {
		// TODO: stat file or let go/packages handle err?
		j.lggr.Debugf("adding %v", file)
		if err := j.w.Add(file); err != nil {
			return err
		}
	}

	return nil
}

func (j *job) parseFiles() []string {
	files := []string{}
	for _, pattern := range j.cfg.Patterns {
		prefix := "file="
		if strings.HasPrefix(pattern, prefix) {
			file := pattern[len(prefix):]
			files = append(files, file)
		}
	}
	return files
}
