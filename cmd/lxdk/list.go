package main

import "github.com/urfave/cli/v2"

var listCmd = &cli.Command{
	Name:  "list",
	Usage: "list clusters",
}

//kubedee [options] list                             list all clusters
