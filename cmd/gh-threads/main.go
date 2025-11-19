package main

import (
	"context"
	"fmt"
	"os"

	"github.com/VRTFinland/gh-threads/internal/app"
)

func main() {
	ctx := context.Background()
	if err := app.New().Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
