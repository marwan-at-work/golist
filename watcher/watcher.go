package watcher

import (
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"marwan.io/golist-server/golist"
)

// NewService returns a new watcher
func NewService(gs golist.Service, lggr *logrus.Logger) Service {
	s := &service{}
	s.watchers = map[string]*job{}
	if lggr == nil {
		lggr = logrus.New()
	}
	s.lggr = lggr
	s.gs = gs

	return s
}

// Service can watch your directory
// and update the golist results
// if anything changes in your .go files.
type Service interface {
	Watch(dir string, args []string) error
	Close() error
}

func (s *service) Watch(dir string, args []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := getKey(args)
	j, ok := s.watchers[dir]
	if ok {
		s.lggr.Debugf("%v: already has watcher", dir)
		j.requestExtension()
		if argExists(key, j.args) {
			s.lggr.Debugf("key %v: exists, returning.", key)
			return nil
		}
		// TODO: data race with watcher callbacks
		j.args = append(j.args, key)
		return nil
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	j = &job{w: w, dir: dir}
	j.args = append(j.args, key)
	j.timer = time.NewTimer(cacheTime)
	j.lggr = s.lggr
	j.deleter = s.close
	j.addFiles()
	j.timer = time.NewTimer(cacheTime)
	j.extension = make(chan struct{})
	s.watchers[dir] = j
	go j.runTimer()
	go j.runWatcher(s.gs)

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

type job struct {
	w         *fsnotify.Watcher
	dir       string
	args      []string
	timer     *time.Timer
	lggr      *logrus.Logger
	deleter   func(dir string, w *fsnotify.Watcher)
	extension chan struct{}
}

const cacheTime = time.Hour

func (j *job) extendDeadline() {
	j.lggr.Debugf("%v: extending deadline", j.dir)
	if !j.timer.Stop() {
		<-j.timer.C
	}
	j.timer.Reset(cacheTime)
}

type service struct {
	watchers map[string]*job
	mu       sync.Mutex
	lggr     *logrus.Logger
	gs       golist.Service
}

func (s *service) close(dir string, w *fsnotify.Watcher) {
	s.mu.Lock()
	w.Close()
	delete(s.watchers, dir)
	s.mu.Unlock()
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
			j.lggr.Debugf("%v: expired. Removing watcher", j.dir)
			j.deleter(j.dir, j.w)
			return
		case <-j.extension:
			j.extendDeadline()
		}
	}
}

func (j *job) runWatcher(gs golist.Service) {
	for {
		select {
		case event, ok := <-j.w.Events:
			j.lggr.Debugf("GOT EVENT: %v", event.String())
			if !ok {
				return
			}
			j.requestExtension()
			if event.Op&fsnotify.Write == fsnotify.Write {
				j.lggr.Debugf("%v changed. Updating %v", event.Name, j.dir)
				for _, key := range j.args {
					// TODO log err
					args := getArgs(key)
					gs.Update(j.dir, args)
				}
			}
			if event.Op&fsnotify.Create == fsnotify.Create {
				j.w.Add(event.Name)
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
	files, err := ioutil.ReadDir(j.dir)
	if err != nil {
		return err
	}

	for _, fi := range files {
		fileName := fi.Name()
		if fi.IsDir() || !isGoFile(fileName) {
			continue
		}
		f := filepath.Join(j.dir, fileName)
		j.lggr.Debugf("adding %v", f)
		if err := j.w.Add(f); err != nil {
			return err
		}
	}

	return nil
}

func isGoFile(f string) bool {
	return filepath.Ext(f) == ".go"
}

func getKey(args []string) string {
	return strings.Join(args, "__x__")
}

func getArgs(key string) []string {
	return strings.Split(key, "__x__")
}

func argExists(element string, data []string) bool {
	for _, v := range data {
		if element == v {
			return true
		}
	}
	return false
}
