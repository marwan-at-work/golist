package cmddriver

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"marwan.io/golist/driver"
	"marwan.io/golist/server"
)

type config struct {
	server   bool
	verbose  bool
	exit     bool
	patterns []string
}

type driverRequest struct {
	Mode       driver.LoadMode   `json:"mode"`
	Env        []string          `json:"env"`
	BuildFlags []string          `json:"build_flags"`
	Tests      bool              `json:"tests"`
	Overlay    map[string][]byte `json:"overlay"`
}

func getFlags() *config {
	fs := flag.NewFlagSet("GolistFlags", flag.ExitOnError)
	sflag := fs.Bool("s", false, "run the golist server")
	verbose := fs.Bool("v", false, "verbose golist server")
	exit := fs.Bool("exit", false, "exit the server")

	err := fs.Parse(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	return &config{
		server:   *sflag,
		verbose:  *verbose,
		exit:     *exit,
		patterns: fs.Args(),
	}
}

func getCfg(c *config) *driver.Config {
	var cfg driver.Config
	cfg.Patterns = c.patterns

	var dr driverRequest
	must(json.NewDecoder(os.Stdin).Decode(&dr))
	cfg.Dir = getDir()
	cfg.Mode = dr.Mode
	cfg.Env = dr.Env
	cfg.BuildFlags = dr.BuildFlags
	cfg.Tests = dr.Tests
	// TODO: overlay once go list takes overlay.

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
	var b io.Reader
	var err error
	if !c.exit {
		b, err = getBody(c)
	}
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
	return exec.Command("golist", "-s").Start()
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
