package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"

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
	clusterName := ctx.Args().First()
	if clusterName == "" {
		return errors.New("must supply cluster name")
	}
	path := path.Join(os.Getenv("HOME"), ".cache", "lxdk", ctx.Args().First(), "certificates")
	err := os.MkdirAll(path, 777)
	if err != nil {
		return errors.Wrap(err, "error creating certificates dir")
	}

	// k8s CA
	err = createCA(path, "ca", caJSON("Kubernetes"))
	if err != nil {
		return err
	}

	// aggregate CA
	err = createCA(path, "ca-aggregation", caJSON("Kubernetes Front Proxy CA"))
	if err != nil {
		return err
	}

	// etcd CA
	err = createCA(path, "ca-etcd", caJSON("etcd"))
	if err != nil {
		return err
	}

	return nil
}

func createCA(dir, caName string, data []byte) error {
	cfsslCmd := exec.Command("cfssl", "gencert", "-initca", "-")
	cfsslCmd.Dir = dir

	pipe, err := cfsslCmd.StdinPipe()
	if err != nil {
		return errors.Wrap(err, "error creating stdin pipe")
	}

	_, err = pipe.Write(data)
	if err != nil {
		return errors.Wrap(err, "error writing data to stdin")
	}
	err = pipe.Close()
	if err != nil {
		return errors.Wrap(err, "error closing pipe")
	}

	cfsslOut, err := cfsslCmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer cfsslOut.Close()

	cfssljsonCmd := exec.Command("cfssljson", "-bare", caName)
	cfssljsonCmd.Stdin = cfsslOut
	cfssljsonCmd.Dir = dir

	err = cfsslCmd.Start()
	if err != nil {
		return errors.Wrap(err, "cfssl error generating CA")
	}

	out, err := cfssljsonCmd.CombinedOutput()
	if err != nil {
		return err
	}

	if string(out) != "" {
		fmt.Println(string(out))
	}

	return nil
}

func caJSON(CN string) []byte {
	return []byte(fmt.Sprintf(`{
  "CN": "%s",
  "key": {
    "algo": "rsa",
    "size": 2048
  },
  "names": [
    {
      "C": "DE",
      "L": "Berlin",
      "O": "%s",
      "OU": "CA",
      "ST": "Berlin"
    }
  ]
}`, CN, CN))
}
