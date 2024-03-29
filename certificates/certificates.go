package certificates

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"path"
	"strings"

	"github.com/pkg/errors"
)

type CAConfig struct {
	// Name is the base filename (no extension) of the CA to be created
	Name string

	// CN of the CA
	CN string

	// Dir is the directory to create the CA in
	Dir string
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

// CAJSON returns a cfssl JSON configuration with CN as both the CN and
// Organization
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
	FileName string

	CN string
	CA CAConfig

	// Directory to create the certificate in
	Dir string

	// Path to ca-config.json for cfssl
	CAConfigPath string

	// ExtraOpts are extra options to pass to cfssl gencert
	ExtraOpts map[string]string

	JSONOverride []byte
}

func CreateCert(conf CertConfig) error {
	if conf.FileName == "" {
		conf.FileName = conf.Name
	}
	caPath := path.Join(conf.CA.Dir, conf.CA.Name+".pem")
	caKeyPath := path.Join(conf.CA.Dir, conf.CA.Name+"-key.pem")
	profile := "kubernetes"

	args := make([]string, 6+len(conf.ExtraOpts))
	args = []string{
		"gencert",
		"-ca=" + caPath,
		"-ca-key=" + caKeyPath,
		"-config=" + conf.CAConfigPath,
		"-profile=" + profile,
	}
	if len(conf.ExtraOpts) > 0 {
		for k, v := range conf.ExtraOpts {
			args = append(args, fmt.Sprintf("-%s=%s", k, v))
		}
	}
	args = append(args, "-")

	cfsslCmd := exec.Command(
		"cfssl",
		args...,
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
		data = CertJSON(conf.CN, conf.Name)
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

// CertJSON returns a cfssl certificate json configuration
func CertJSON(CN, organization string) []byte {
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
}`, CN, organization))
}
