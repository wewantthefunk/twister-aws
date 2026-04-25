// Command mappath prints the absolute path from server.json's mapPath field.
// Used by the Makefile for `make run` (Docker volume -v <mapPath>:/app).
// Relative mapPath is resolved against the process working directory.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/christian/twister/internal/config"
)

func main() {
	cfgPath := flag.String("config", "server.json", "path to server.json")
	flag.Parse()
	c, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mappath: %v\n", err)
		os.Exit(1)
	}
	if strings.TrimSpace(c.MapPath) == "" {
		fmt.Fprintf(os.Stderr, "mappath: mapPath is empty in %q (set mapPath in server.json to the host directory to mount at /app)\n", *cfgPath)
		os.Exit(1)
	}
	abs, err := filepath.Abs(c.MapPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mappath: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(abs)
}
