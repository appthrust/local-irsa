package main

import (
	"context"
	"os"

	"github.com/appthrust/local-irsa/internal/app"
)

func main() {
	runner := app.NewRunner()
	if err := runner.Execute(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}
