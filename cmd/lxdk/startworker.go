package main

import "github.com/urfave/cli/v2"

var startworkerCmd = &cli.Command{
	Name:  "start-worker",
	Usage: "start a new worker node in a cluster",
}

//kubedee [options] start-worker <cluster name>      start a new worker node in a cluster
