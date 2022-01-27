package main

import "github.com/urfave/cli/v2"

var createCmd = &cli.Command{
	Name:  "create",
	Usage: "create a cluster",
}

//kubedee [options] create <cluster name>            create a cluster
