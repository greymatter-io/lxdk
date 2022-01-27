package main

import "github.com/urfave/cli/v2"

var kubectlenvCmd = &cli.Command{
	Name:  "kubectl-env",
	Usage: "print kubectl environment variables",
}

//kubedee [options] kubectl-env <cluster name>       print kubectl environment variables
