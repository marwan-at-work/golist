package main

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
	"marwan.io/golist-server/golist"
	"marwan.io/golist-server/watcher"
)

func runServer(gs golist.Service, lggr *logrus.Logger, w watcher.Service) error {
	ch := make(chan os.Signal, 2) // len == 2: one for ctrl+C and one for /exit

	http.HandleFunc("/", timer(handler(gs, w, lggr), lggr))
	http.HandleFunc("/exit", exitHandler(ch))

	p := getSockDir()
	p = ":8912"
	l, err := net.Listen("tcp", p)
	// TODO: use unix socket
	// l, err := net.Listen("unix", p)
	if err != nil {
		return err
	}

	s := &http.Server{Handler: http.DefaultServeMux}
	signal.Notify(ch, os.Interrupt)
	go func() {
		lggr.Debugf("listening on unix socket: %v", p)
		go s.Serve(l)
	}()

	<-ch
	lggr.Info("SHUTTING DOWN SERVER...")
	err = s.Shutdown(context.Background())
	if err != nil && err != http.ErrServerClosed {
		lggr.Error(err)
	}
	os.RemoveAll(p)
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

// TODO: add no-cache flag so that when error occurs, try it.
type body struct {
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
}

func handler(gs golist.Service, ws watcher.Service, lggr *logrus.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b body
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

func getSockDir() string {
	tempdir := os.TempDir()
	if tempdir == "" {
		log.Fatal("no temp dir provided by os")
	}
	return filepath.Join(tempdir, "golistserver")
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
