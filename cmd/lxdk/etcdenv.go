package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"path"

	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/containers"
	"github.com/greymatter-io/lxdk/lxd"
	"github.com/urfave/cli/v2"
)

var etcdenvCmd = &cli.Command{
	Name:   "etcd-env",
	Usage:  "print etcdctl environment variables",
	Action: doEtcdenv,
}

func doEtcdenv(ctx *cli.Context) error {
	cacheDir := ctx.String("cache")
	if ctx.Args().Len() == 0 {
		return fmt.Errorf("must supply cluster name")
	}
	clusterName := ctx.Args().First()
	certDir := path.Join(cacheDir, clusterName, "certificates")

	state, err := config.ClusterStateFromContext(ctx)
	if err != nil {
		return err
	}

	log.Default().SetOutput(ioutil.Discard)

	is, hostname, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	etcdIP, err := containers.WaitContainerIP(state.EtcdContainerName, []string{hostname}, is)
	if err != nil {
		return err
	}

	cert := func(name string) string { return path.Join(certDir, name+".pem") }
	fmt.Println("ETCDCTL_CACERT=" + cert("ca"))
	fmt.Println("ETCDCTL_CERT=" + cert("etcd"))
	fmt.Println("ETCDCTL_KEY=" + cert("etcd-key"))
	fmt.Println("ETCD_INSECURE_TRANSPORT=false")
	fmt.Println("ETCD_ENDPOINTS=" + etcdIP.String())
	fmt.Println("ETCD_API=3")

	return nil
}
