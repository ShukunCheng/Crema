package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("crema", Version)
		os.Exit(0)
	}
	fmt.Println("crema", Version, "— TUI arrives once the agent layer lands")
}
