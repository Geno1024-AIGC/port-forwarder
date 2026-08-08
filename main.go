package main

import (
	"flag"
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "port-forwarder %s\n\n", version)
		fmt.Fprintf(flag.CommandLine.Output(), "A TCP port-forwarding daemon with an embedded web admin UI.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	fmt.Fprintf(os.Stderr, "port-forwarder %s: not wired up yet\n", version)
}