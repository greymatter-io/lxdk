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
	kubeCAConf := CAConfig{
		Name: "ca",
		Dir:  path,
		CN:   "Kubernetes",
	}
	err = CreateCA(kubeCAConf)
	if err != nil {
		return err
	}

	// aggregate CA
	aggregateCAConf := CAConfig{
		Name: "ca-aggregation",
		Dir:  path,
		CN:   "Kubernetes Front Proxy CA",
	}
	err = CreateCA(aggregateCAConf)
	if err != nil {
		return err
	}

	// etcd CA
	etcdCAConf := CAConfig{
		Name: "ca-etcd",
		Dir:  path,
		CN:   "etcd",
	}
	err = CreateCA(etcdCAConf)
	if err != nil {
		return err
	}

	// CA config
	caConfigPath, err := WriteCAConfig(path)
	if err != nil {
		return err
	}

	// admin cert
	adminCertConfig := CertConfig{
		Name:         "admin",
		CN:           "system:masters",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = createCert(adminCertConfig)
	if err != nil {
		return err
	}

	// aggregation client cert
	aggCertConfig := CertConfig{
		Name:         "aggregation-client",
		JSONOverride: certJSON("kube-apiserver", "kube-apiserver"),
		CA:           aggregateCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = createCert(aggCertConfig)
	if err != nil {
		return err
	}

	// kube-controler-manager
	kubeConManConfig := CertConfig{
		Name:         "kube-controller-manager",
		CN:           "kube-controller-manager",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = createCert(kubeConManConfig)
	if err != nil {
		return err
	}

	// kube-scheduler
	kubeSchedCertConfig := CertConfig{
		Name:         "system:kube-scheduler",
		FileName:     "kube-scheduler",
		CN:           "system:kube-scheduler",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = createCert(kubeSchedCertConfig)
	if err != nil {
		return err
	}

	// TODO: etcd cert
	// TODO: controller cert
	// TODO: worker cert

	return nil
}

type CAConfig struct {
	Name string
	CN   string
	Dir  string
}

func CreateCA(conf CAConfig) error {
	cfsslCmd := exec.Command("cfssl", "gencert", "-initca", "-")
	cfsslCmd.Dir = conf.Dir

	pipe, err := cfsslCmd.StdinPipe()
	if err != nil {
		return errors.Wrap(err, "error creating stdin pipe")
	}

	data := CAJSON(conf.CN)

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
	cfssljsonCmd := exec.Command("cfssljson", "-bare", conf.Name)
	cfssljsonCmd.Stdin = cfsslOut
	cfssljsonCmd.Dir = conf.Dir

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

func CAJSON(CN string) []byte {
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

func WriteCAConfig(dir string) (fullPath string, err error) {
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

type CertConfig struct {
	Name string

	// FileName overrides Name for saving the file (optional, use if Name is
	// not a valid file name)
	FileName     string
	CN           string
	CA           CAConfig
	Dir          string
	CAConfigPath string

	JSONOverride []byte
}

func createCert(conf CertConfig) error {
	if conf.FileName == "" {
		conf.FileName = conf.Name
	}
	caPath := path.Join(conf.CA.Dir, conf.CA.Name+".pem")
	caKeyPath := path.Join(conf.CA.Dir, conf.CA.Name+"-key.pem")
	profile := "kubernetes"

	cfsslCmd := exec.Command(
		"cfssl",
		"gencert",
		"-ca="+caPath,
		"-ca-key="+caKeyPath,
		"-config="+conf.CAConfigPath,
		"-profile="+profile,
		"-",
	)
	cfsslCmd.Dir = conf.Dir

	pipe, err := cfsslCmd.StdinPipe()
	if err != nil {
		return errors.Wrap(err, "error creating stdin pipe")
	}

	var data []byte
	if len(conf.JSONOverride) != 0 {
		data = conf.JSONOverride
	} else {
		data = certJSON(conf.CN, conf.Name)
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

	cfssljsonCmd := exec.Command("cfssljson", "-bare", conf.FileName)
	cfssljsonCmd.Dir = conf.Dir

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
