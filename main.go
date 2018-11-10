package main

import (
	"flag"
	"fmt"

	"marwan.io/golist/server"
)

var sflag = flag.Bool("s", false, "run the golist server")
var verbose = flag.Bool("v", false, "verbose golist server")

func main() {
	flag.Parse()
	if *sflag {
		must(server.RunServer(*verbose))
	}
	fmt.Println(flag.Args())
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
