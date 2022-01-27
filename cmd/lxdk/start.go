package main

import "github.com/urfave/cli/v2"

var startCmd = &cli.Command{
	Name:  "start",
	Usage: "start a cluster",
}

//kubedee [options] start <cluster name>             start a cluster
