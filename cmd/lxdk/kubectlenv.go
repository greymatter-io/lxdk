package main

import (
	"fmt"
	"path"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var kubectlenvCmd = &cli.Command{
	Name:   "kubectl-env",
	Usage:  "print kubectl environment variables",
	Action: runKubectlenv,
}

func runKubectlenv(ctx *cli.Context) error {
	cacheDir := ctx.String("cache")

	if ctx.Args().Len() == 0 {
		return errors.New("must supply cluster name")
	}
	clusterName := ctx.Args().First()

	kfgPath := path.Join(cacheDir, clusterName, "kubeconfigs", "client.kubeconfig")
	fmt.Printf("export KUBECONFIG=%s", kfgPath)

	return nil
}
