package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"

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

	path := path.Join(cacheDir, ctx.Args().First(), "certificates")
	err := os.MkdirAll(path, 0777)
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

	// CA config
	_, err = writeCAConfig(path)
	if err != nil {
		return err
	}

	// admin cert
	err = createCert(path, "ca", "admin", certJSON("admin", "system:masters"))
	if err != nil {
		return err
	}

	// aggregation client cert
	err = createCert(path, "ca-aggregation", "aggregation-client", certJSON("kube-apiserver", "kube-apiserver"))
	if err != nil {
		return err
	}

	// TODO: etcd cert
	// TODO: controller cert
	// TODO: worker cert

	// kube-controler-manager
	err = createCert(path, "ca", "kube-controller-manager", certJSON("kube-controller-manager", "kube-controller-manager"))
	if err != nil {
		return err
	}

	// kube-scheduler
	err = createCert(path, "ca", "kube-scheduler", certJSON("system:kube-scheduler", "system:kube-scheduler"))
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

func writeCAConfig(dir string) (fullPath string, err error) {
	fullPath = path.Join(dir, "ca-config.json")
	err = ioutil.WriteFile(fullPath, []byte(`{
  "signing": {
    "default": {
      "expiry": "8760h"
    },
    "profiles": {
      "kubernetes": {
        "usages": ["signing", "key encipherment", "server auth", "client auth"],
        "expiry": "8760h"
      }
    }
  }
}`), 0777)
	if err != nil {
		return "", errors.Wrap(err, "error writing to "+fullPath)
	}

	return fullPath, nil
}

func createCert(dir, caName, name string, data []byte) error {
	caPath := caName + ".pem"
	caKeyPath := caName + "-key.pem"
	caConfigPath := "ca-config.json"
	profile := "kubernetes"

	cfsslCmd := exec.Command(
		"cfssl",
		"gencert",
		"-ca="+caPath,
		"-ca-key="+caKeyPath,
		"-config="+caConfigPath,
		"-profile="+profile,
		"-",
	)
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

	out, err := cfsslCmd.CombinedOutput()
	if err != nil {
		if string(out) != "" {
			fmt.Println(string(out))
		}
		return errors.Wrap(err, "cfssl error generating cert")
	}

	cfssljsonCmd := exec.Command("cfssljson", "-bare", name)
	cfssljsonCmd.Dir = dir

	jsonPipe, err := cfssljsonCmd.StdinPipe()
	if err != nil {
		return errors.Wrap(err, "error creating stdin pipe")
	}

	out = []byte(string(out)[strings.Index(string(out), "{"):])

	_, err = jsonPipe.Write(out)
	if err != nil {
		return errors.Wrap(err, "error writing data to stdin")
	}
	err = jsonPipe.Close()

	out, err = cfssljsonCmd.CombinedOutput()
	if string(out) != "" {
		fmt.Println(string(out))
	}
	if err != nil {
		return err
	}

	return nil
}

func certJSON(CN, name string) []byte {
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
      "OU": "kubedee",
      "ST": "Berlin"
    }
  ]
}`, CN, name))
}
