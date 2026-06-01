package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mgomes/ohm/ohmcli"
)

func main() {
	if err := ohmcli.New().Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
