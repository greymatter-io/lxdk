package main

import "github.com/urfave/cli/v2"

var deleteCmd = &cli.Command{
	Name:  "delete",
	Usage: "delete a cluster",
}

//kubedee [options] delete <cluster name>            delete a cluster
