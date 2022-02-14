package main

import (
	"os"
	"path"

	certs "github.com/greymatter-io/lxdk/certificates"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

//kubedee [options] create <cluster name>            create a cluster

var createCmd = &cli.Command{
	Name:   "create",
	Usage:  "create a cluster",
	Action: doCreate,
}

func doCreate(ctx *cli.Context) error {
	cacheDir := ctx.String("cache")

	clusterName := ctx.Args().First()
	if clusterName == "" {
		return errors.New("must supply cluster name")
	}

	path := path.Join(cacheDir, clusterName, "certificates")
	err := os.MkdirAll(path, 0777)
	if err != nil {
		return errors.Wrap(err, "error creating certificates dir")
	}

	// k8s CA
	kubeCAConf := certs.CAConfig{
		Name: "ca",
		Dir:  path,
		CN:   "Kubernetes",
	}
	err = certs.CreateCA(kubeCAConf)
	if err != nil {
		return err
	}

	// aggregate CA
	aggregateCAConf := certs.CAConfig{
		Name: "ca-aggregation",
		Dir:  path,
		CN:   "Kubernetes Front Proxy CA",
	}
	err = certs.CreateCA(aggregateCAConf)
	if err != nil {
		return err
	}

	// etcd CA
	etcdCAConf := certs.CAConfig{
		Name: "ca-etcd",
		Dir:  path,
		CN:   "etcd",
	}
	err = certs.CreateCA(etcdCAConf)
	if err != nil {
		return err
	}

	// CA config
	caConfigPath, err := certs.WriteCAConfig(path)
	if err != nil {
		return err
	}

	// admin cert
	adminCertConfig := certs.CertConfig{
		Name:         "admin",
		CN:           "system:masters",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = certs.CreateCert(adminCertConfig)
	if err != nil {
		return err
	}

	// aggregation client cert
	aggCertConfig := certs.CertConfig{
		Name:         "aggregation-client",
		JSONOverride: certs.CertJSON("kube-apiserver", "kube-apiserver"),
		CA:           aggregateCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = certs.CreateCert(aggCertConfig)
	if err != nil {
		return err
	}

	// kube-controler-manager
	kubeConManConfig := certs.CertConfig{
		Name:         "kube-controller-manager",
		CN:           "kube-controller-manager",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = certs.CreateCert(kubeConManConfig)
	if err != nil {
		return err
	}

	// kube-scheduler
	kubeSchedCertConfig := certs.CertConfig{
		Name:         "system:kube-scheduler",
		FileName:     "kube-scheduler",
		CN:           "system:kube-scheduler",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = certs.CreateCert(kubeSchedCertConfig)
	if err != nil {
		return err
	}

	// TODO: etcd cert
	// TODO: controller cert
	// TODO: worker cert

	return nil
}
