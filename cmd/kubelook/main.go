package main

import (
	"os"

	"github.com/bdnix/kubelook/internal/app"
)

func main() { os.Exit(app.Main(os.Args[1:], os.Stdout, os.Stderr, os.Getenv)) }
