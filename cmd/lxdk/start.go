package main

import (
	"fmt"
	"strings"

	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/containers"
	"github.com/greymatter-io/lxdk/lxd"
	"github.com/urfave/cli/v2"
)

var startCmd = &cli.Command{
	Name:   "start",
	Usage:  "start a cluster",
	Action: doStart,
}

// TODO: controller cert
// TODO: worker cert

func doStart(ctx *cli.Context) error {
	state, err := config.ClusterStateFromContext(ctx)
	if err != nil {
		return err
	}

	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	for _, container := range state.Containers {
		err = containers.StartContainer(container, is)
		if err != nil {
			return err
		}

		// TODO: etcd cert
		if strings.Contains(container, "etcd") {
		}
	}

	for _, container := range state.Containers {
		var ip string
		if ip, err = containers.GetContainerIP(container, is); err != nil {
			fmt.Println(err)
		}
		fmt.Println(ip)
	}

	return nil
}

//kubedee [options] start <cluster name>             start a cluster
