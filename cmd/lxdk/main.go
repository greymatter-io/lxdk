package main

import (
	"fmt"
	"log"
	"os"

	"github.com/greymatter-io/lxdk/version"
	"github.com/urfave/cli/v2" // imports as package "cli"
)

var app = &cli.App{
	Name:    "lxdk",
	Usage:   "Fast multi-node Kubernetes on lxd",
	Version: version.Version(),
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:      "config",
			Aliases:   []string{"c"},
			Usage:     "Path to config file",
			TakesFile: true,
			Value:     fmt.Sprintf("%s/.config/lxdk/config.toml", os.Getenv("HOME")),
			EnvVars:   []string{"LXDK_CONFIG"},
		},
		&cli.StringFlag{
			Name:    "cache",
			Usage:   "Path to cache directory",
			Value:   fmt.Sprintf("%s/.cache/lxdk", os.Getenv("HOME")),
			EnvVars: []string{"LXDK_CACHE"},
		},
	},
	Commands: []*cli.Command{
		upCmd,
		startworkerCmd,
		startCmd,
		listCmd,
		kubectlenvCmd,
		etcdenvCmd,
		deleteCmd,
		createCmd,
		debugCertCmd,
	},
	CommandNotFound: func(c *cli.Context, cmd string) {
		fmt.Fprintf(c.App.Writer, `command not found: %s, run "lxdk --help" for help`, cmd)
	},
}

func main() {
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

//kubedee [options] controller-ip <cluster name>     print the IPv4 address of the controller node
//kubedee [options] create-admin-sa <cluster name>   create admin service account in cluster
//kubedee [options] create-user-sa <cluster name>    create user service account in cluster (has 'edit' privileges)
//kubedee [options] smoke-test <cluster name>        smoke test a cluster
