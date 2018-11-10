package main // import "marwan.io/golist"

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"marwan.io/golist/server"
)

var sflag = flag.Bool("s", false, "run the golist server")
var verbose = flag.Bool("v", false, "verbose golist server")
var exit = flag.Bool("exit", false, "exit the server")

func main() {
	flag.Parse()
	if *sflag {
		must(server.RunServer(*verbose))
		return
	}

	c := getClient()
	b, err := getBody()
	must(err)
	url := "http://unix/"
	if *exit {
		url += "exit"
	}

	req, _ := http.NewRequest(http.MethodPost, url, b)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := c.Do(req)
	if err != nil {
		must(tryServer())
		time.Sleep(time.Second)
		resp, err = c.Do(req)
	}
	must(err)
	defer resp.Body.Close()
	io.Copy(os.Stdout, resp.Body)
}

func tryServer() error {
	return exec.Command("golist", "-s").Start()
}

func getClient() *http.Client {
	socket := server.GetSocketPath()
	return &http.Client{
		Transport: &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socket)
		}},
	}
}

func getBody() (io.Reader, error) {
	args := flag.Args()
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	b := &server.Body{
		Dir:  dir,
		Args: args,
	}
	var bts bytes.Buffer
	err = json.NewEncoder(&bts).Encode(b)
	return &bts, err
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
