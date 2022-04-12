package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
)

var listCmd = &cli.Command{
	Name:   "list",
	Usage:  "list clusters",
	Action: doList,
}

func doList(ctx *cli.Context) error {
	cacheDir := ctx.String("cache")

	dirEntries, err := os.ReadDir(cacheDir)
	if err != nil {
		return err
	}

	var clusters []string
	for _, entry := range dirEntries {
		if entry.IsDir() {
			clusters = append(clusters, entry.Name())
		}
	}

	fmt.Println(clusters)

	return nil
}
