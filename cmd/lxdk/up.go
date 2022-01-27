package main

import "github.com/urfave/cli/v2"

var upCmd = &cli.Command{
	Name:  "up",
	Usage: "create + start in one command",
}

//kubedee [options] up <cluster name>                create + start in one command
