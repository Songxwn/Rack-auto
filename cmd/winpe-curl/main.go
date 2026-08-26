package main

import (
	"os"

	"github.com/Songxwn/Rack-auto/internal/winpecurl"
)

func main() {
	os.Exit(winpecurl.Main(os.Args[1:]))
}
