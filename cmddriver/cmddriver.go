package cmddriver

import (
	"bytes"
	"context"
	"encoding/gob"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"marwan.io/golist/driver"
	"marwan.io/golist/server"
)

type buildFlags []string

func (bfs *buildFlags) String() string {
	return strings.Join(*bfs, ", ")
}

func (bfs *buildFlags) Set(s string) error {
	*bfs = append(*bfs, s)
	return nil
}

type config struct {
	server  bool
	verbose bool
	exit    bool

	useTest    bool
	useExport  bool
	useDeps    bool
	buildFlags []string
	patterns   []string
}

func getFlags() *config {
	fs := flag.NewFlagSet("GolistFlags", flag.ExitOnError)
	sflag := fs.Bool("s", false, "run the golist server")
	verbose := fs.Bool("v", false, "verbose golist server")
	exit := fs.Bool("exit", false, "exit the server")

	useTest := fs.Bool("test", false, "go/packages test arg")
	useExport := fs.Bool("export", false, "go/packages export arg")
	useDeps := fs.Bool("deps", false, "go/packages deps arg")
	bfs := buildFlags{}
	fs.Var(&bfs, "buildflag", "go/packages buildflag arg")
	if len(os.Args) < 2 {
		log.Fatal("not enough args")
	}
	if os.Args[1] != "list" {
		log.Fatal("first argument must be list")
	}
	err := fs.Parse(os.Args[2:])
	if err != nil {
		log.Fatal(err)
	}

	return &config{
		server:  *sflag,
		verbose: *verbose,
		exit:    *exit,

		useTest:    *useTest,
		useExport:  *useExport,
		useDeps:    *useDeps,
		buildFlags: []string(bfs),
		patterns:   fs.Args(),
	}
}

func getCfg(c *config) *driver.Config {
	var cfg driver.Config
	cfg.Patterns = c.patterns
	cfg.WantSizes = true // go/packages doesn't send this
	cfg.WantDeps = c.useDeps
	cfg.UsesExportData = c.useExport
	cfg.Dir = getDir()
	cfg.Env = os.Environ()
	cfg.BuildFlags = c.buildFlags
	cfg.Tests = c.useTest
	return &cfg
}

func getDir() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return dir
}

func getBody(c *config) (io.Reader, error) {
	var bts bytes.Buffer
	err := gob.NewEncoder(&bts).Encode(getCfg(c))
	return &bts, err
}

// Main starts the daemon or client
func Main() {
	c := getFlags()
	if c.server {
		must(server.RunServer(c.verbose))
		return
	}

	client := getClient()
	b, err := getBody(c)
	must(err)
	url := "http://unix/"
	if c.exit {
		url += "exit"
	}

	req, _ := http.NewRequest(http.MethodPost, url, b)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		if c.exit {
			// don't close the server if it's already closed
			// TODO: check for no conn err
			return
		}
		tryServer()
		time.Sleep(time.Second * 2)
		resp, err = client.Do(req)
	}
	must(err)
	defer resp.Body.Close()
	io.Copy(os.Stdout, resp.Body)
}

func tryServer() error {
	return exec.Command("golist", "list", "-s").Start()
}

func getClient() *http.Client {
	socket := server.GetSocketPath()
	return &http.Client{
		Transport: &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", socket, time.Second*30)
		}},
		Timeout: time.Second * 30,
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
