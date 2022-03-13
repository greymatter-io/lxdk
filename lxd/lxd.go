package lxd

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/lxc/config"
	"github.com/pkg/errors"
)

const snapSocketPath = "/var/snap/lxd/common/lxd/unix.socket"

func InstanceServerConnect() (lxd.InstanceServer, string, error) {
	var is lxd.InstanceServer
	confDir := path.Join(os.Getenv("HOME"), ".config", "lxc")

	isSnap, err := IsSnap()
	if err != nil {
		return nil, "", err
	}
	if isSnap {
		confDir = path.Join(os.Getenv("HOME"), "snap", "lxd", "common", "config")
	}

	lxdConf := os.Getenv("LXD_CONF")
	if !(lxdConf == "") {
		confDir = lxdConf
	}

	confFile := path.Join(confDir, "config.yml")

	conf, err := config.LoadConfig(confFile)
	if err != nil {
		return is, "", err
	}

	if isSnap && conf.DefaultRemote == "local" {
		is, err = lxd.ConnectLXDUnix(snapSocketPath, nil)
		if err != nil {
			return lxd.InstanceServer(is), "", errors.Errorf("could not connect to socket at %s", snapSocketPath)
		}
		return is, "", err
	}

	log.Default().Printf("using remote: %s", conf.DefaultRemote)
	is, err = conf.GetInstanceServer(conf.DefaultRemote)
	if err != nil {
		return nil, "", fmt.Errorf("error getting instanse server from config: %w", err)
	}

	uri, err := url.Parse(conf.Remotes[conf.DefaultRemote].Addr)
	if err != nil {
		return nil, "", err
	}

	return is, uri.Hostname(), nil
}

// IsSnap returns true if lxd was installed using snap, and false otherwise.
// This is necessary because the snap install puts its config file in a
// different location on disk.
func IsSnap() (bool, error) {
	lxcPath, err := exec.LookPath("lxc")
	if err != nil {
		return false, fmt.Errorf("could not find lxc in PATH: %w", err)
	}

	if strings.Contains(lxcPath, "snap") {
		return true, nil
	}

	return false, nil
}
