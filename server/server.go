package server

import (
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"marwan.io/golist/cache"
	"marwan.io/golist/driver"
	"marwan.io/golist/watcher"
)

// RunServer runs the golist caching server on a unix socket.
func RunServer(verbose bool) error {
	lggr := logrus.New()
	level := logrus.WarnLevel
	if verbose {
		level = logrus.DebugLevel
	}
	lggr.SetLevel(level)
	dbPath := GetDBPath()
	lggr.Debugf("db path at %v", dbPath)
	dc, err := cache.New(dbPath, lggr)
	if err != nil {
		return err
	}
	go dc.UpdateAll(context.Background())
	w := watcher.NewService(dc, lggr)
	ch := make(chan os.Signal, 2) // len == 2: one for ctrl+C and one for /exit
	http.HandleFunc("/", timer(handler(dc, w, lggr), lggr))
	http.HandleFunc("/exit", exitHandler(ch))

	socket := GetSocketPath()
	l, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	s := &http.Server{Handler: http.DefaultServeMux}
	signal.Notify(ch, os.Interrupt)
	go func() {
		lggr.Debugf("listening on unix socket: %v", socket)
		go s.Serve(l)
	}()

	<-ch
	lggr.Info("SHUTTING DOWN SERVER...")
	err = s.Shutdown(context.Background())
	if err != nil && err != http.ErrServerClosed {
		lggr.Error(err)
	}
	os.RemoveAll(socket)
	lggr.Info("closing watchers")
	return w.Close()
}

func timer(h http.HandlerFunc, lggr *logrus.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		h(w, r)
		lggr.Info(time.Since(t))
	}
}

// Body is the golist body request
//
// TODO: add no-cache flag so that when error occurs, try it.
type Body struct {
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
}

func handler(dc cache.Service, ws watcher.Service, lggr *logrus.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg driver.Config
		err := gob.NewDecoder(r.Body).Decode(&cfg)
		if err != nil {
			lggr.Warnf("incorrect request body: %v", err)
			w.WriteHeader(400)
			return
		}
		lggr.Debugf("received %v - mode: %v, test: %v", cfg.Patterns, cfg.Mode, cfg.Tests)
		// TODO: check if valid files
		bts, err := dc.Get(r.Context(), &cfg)
		if err != nil {
			fmt.Fprint(w, err.Error())
			return
		}
		w.Write(bts)
		ws.Watch(&cfg)
	}
}

func exitHandler(ch chan os.Signal) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		go func() {
			time.Sleep(time.Millisecond * 200)
			ch <- os.Interrupt
		}()
	}
}

// GetSocketPath is the path of a unix socket for
// client/server communication.
func GetSocketPath() string {
	tempdir := os.TempDir()
	if tempdir == "" {
		log.Fatal("no temp dir provided by os")
	}
	return filepath.Join(tempdir, "golistsocket")
}

// GetDBPath returns the path to the cache database.
func GetDBPath() string {
	tempdir := os.TempDir()
	if tempdir == "" {
		log.Fatal("no temp dir provided by os")
	}
	return filepath.Join(tempdir, "golist.db")
}

func validDir(dir string) (string, bool) {
	if dir == "" {
		return "dir must not be empty", false
	}

	_, err := os.Stat(dir)
	if err != nil {
		return err.Error(), false
	}

	return "", true
}
