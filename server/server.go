package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"marwan.io/golist/lister"
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
	gs, err := lister.New(dbPath, lggr)
	if err != nil {
		return err
	}
	defer gs.Close()
	go gs.UpdateAll()
	w := watcher.NewService(gs, lggr)
	ch := make(chan os.Signal, 2) // len == 2: one for ctrl+C and one for /exit
	http.HandleFunc("/", timer(handler(gs, w, lggr), lggr))
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

func handler(gs lister.Service, ws watcher.Service, lggr *logrus.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b Body
		err := json.NewDecoder(r.Body).Decode(&b)
		if err != nil {
			lggr.Warnf("incorrect request body: %v", err)
			w.WriteHeader(400)
			return
		}
		if s, ok := validDir(b.Dir); !ok {
			fmt.Println(b.Dir, "is an invalid directory")
			http.Error(w, s, 400)
			return
		}
		go ws.Watch(b.Dir, b.Args)
		bts, err := gs.Get(b.Dir, b.Args)
		if err != nil {
			// TODO: handle err
		}
		w.Write(bts)
	}
}

func exitHandler(ch chan os.Signal) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO: remove unix socket
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
