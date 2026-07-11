package main

import (
	"flag"

	"cipherproxy/internal/gui"
	"cipherproxy/internal/headless"
)

func main() {
	headlessFlag := flag.Bool("headless", false, "run without GUI, using saved config, for scripted testing")
	flag.Parse()

	if *headlessFlag {
		headless.Run()
		return
	}
	gui.Run()
}
