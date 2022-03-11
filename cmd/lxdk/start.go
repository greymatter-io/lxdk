package main

import (
	"fmt"
	"path"
	"strings"

	"github.com/greymatter-io/lxdk/certificates"
	certs "github.com/greymatter-io/lxdk/certificates"
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

func doStart(ctx *cli.Context) error {
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

	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	for _, container := range state.Containers {
		err = containers.StartContainer(container, is)
		if err != nil {
			return err
		}

	}

	for _, container := range state.Containers {
		ip, err := containers.GetContainerIP(container, is)
		if err != nil {
			return err
		}

		// etcd cert
		if strings.Contains(container, "etcd") {
			etcdCertConfig := certs.CertConfig{
				Name: "etcd",
				CN:   "etcd",
				CA: certs.CAConfig{
					Name: "ca-etcd",
					Dir:  certDir,
					CN:   "etcd",
				},
				Dir:          certDir,
				CAConfigPath: path.Join(certDir, "ca-config.json"),
				ExtraOpts: map[string]string{
					"hostname": ip + ",127.0.0.1",
				},
			}

			err = certificates.CreateCert(etcdCertConfig)
			if err != nil {
				return err
			}
		}

		// conroller cert
		if strings.Contains(container, "controller") {
			controllerCertConfig := certs.CertConfig{
				Name: "kubernetes",
				CN:   "kubernetes",
				CA: certs.CAConfig{
					Name: "ca",
					Dir:  certDir,
					CN:   "Kubernetes",
				},
				Dir:          certDir,
				CAConfigPath: path.Join(certDir, "ca-config.json"),
				ExtraOpts: map[string]string{
					"hostname": ip + ",127.0.0.1",
				},
			}

			err = certificates.CreateCert(controllerCertConfig)
			if err != nil {
				return err
			}
		}

		// worker cert
		if strings.Contains(container, "worker") {
			workerCertConfig := certs.CertConfig{
				Name:     "node:" + container,
				FileName: container,
				CN:       "system:node:" + container,
				CA: certs.CAConfig{
					Name: "ca",
					Dir:  certDir,
					CN:   "Kubernetes",
				},
				Dir:          certDir,
				CAConfigPath: path.Join(certDir, "ca-config.json"),
				ExtraOpts: map[string]string{
					"hostname": ip + "," + container,
				},
			}

			err = certificates.CreateCert(workerCertConfig)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
