package main

import "github.com/urfave/cli/v2"

var etcdenvCmd = &cli.Command{
	Name:  "etcd-env",
	Usage: "print etcdctl environment variables",
}

//kubedee [options] etcd-env <cluster name>          print etcdctl environment variables
