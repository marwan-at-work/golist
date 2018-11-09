package main

import (
	"flag"

	"github.com/sirupsen/logrus"
	"marwan.io/golist-server/golist"
	"marwan.io/golist-server/watcher"
)

var sflag = flag.Bool("s", false, "run the golist server")
var verbose = flag.Bool("v", false, "verbose golist server")

func main() {
	flag.Parse()
	lggr := logrus.New()
	level := logrus.WarnLevel
	if *verbose {
		level = logrus.DebugLevel
	}
	lggr.SetLevel(level)

	if *sflag {
		doServer(lggr)
	}
}

func doServer(lggr *logrus.Logger) {
	// TODO: dbpath
	gs, err := golist.New("./list.db", lggr)
	must(err)
	go gs.UpdateAll()
	w := watcher.NewService(gs, lggr)
	must(runServer(gs, lggr, w))

}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
