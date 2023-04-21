package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/livekit/psrpc/protoc-gen-psrpc/internal/gen"
	"github.com/livekit/psrpc/version"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *versionFlag {
		fmt.Println(version.Version)
		os.Exit(0)
	}

	g := newGenerator()
	gen.Main(g)
}
