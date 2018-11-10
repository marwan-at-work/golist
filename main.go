package main

import (
	"flag"

	"marwan.io/golist-server/server"
)

var sflag = flag.Bool("s", false, "run the golist server")
var verbose = flag.Bool("v", false, "verbose golist server")

func main() {
	flag.Parse()
	if *sflag {
		must(server.RunServer(*verbose))
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
