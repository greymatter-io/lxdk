package main

import (
	"crypto/x509"
	"encoding/pem"
	"flag"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/containers"
	"github.com/greymatter-io/lxdk/lxd"
	"github.com/greymatter-io/lxdk/testutils"
	"github.com/urfave/cli/v2"
)

func testFast(t *testing.T) {
	if strings.ToLower(os.Getenv("TEST_FAST")) == "true" {
		t.Skip("TEST_FAST is true, skipping...")
	}
}

// TODO: env test_fast=true to skip slow tests
func startCluster(t *testing.T) (func(), string) {
	tmpDir, cleanup, err := testutils.TempDir()
	if err != nil {
		t.Fatal(err)
	}

	flags := flag.NewFlagSet("testflags", flag.ErrorHandling(2))
	ctx := cli.NewContext(cli.NewApp(), flags, &cli.Context{})

	os.Setenv("LXDK_CACHE", tmpDir)
	err = app.RunContext(ctx.Context, []string{"lxdk", "create", "test"})
	if err != nil {
		t.Fatal(err)
	}

	err = app.RunContext(ctx.Context, []string{"lxdk", "start", "test"})
	if err != nil {
		t.Fatal(err)
	}

	return func() {
		err = app.RunContext(ctx.Context, []string{"lxdk", "delete", "test"})
		if err != nil {
			t.Fatal(err)
		}
		cleanup()
	}, tmpDir
}

func TestClusterStarted(t *testing.T) {
	testFast(t)

	cleanup, tmpDir := startCluster(t)
	defer cleanup()

	stateBytes, err := ioutil.ReadFile(path.Join(tmpDir, "test", "state.toml"))
	if err != nil {
		t.Fatal(err)
	}

	var state config.ClusterState
	err = toml.Unmarshal(stateBytes, &state)
	if err != nil {
		t.Fatal(err)
	}

	is, _, err := lxd.InstanceServerConnect()
	if err != nil {
		t.Fatal(err)
	}

	for _, container := range state.Containers {
		instance, _, err := is.GetInstance(container)
		if err != nil {
			t.Fatal(err)
		}

		if instance.Status != "Running" {
			t.Fatalf("container %s is not running", container)
		}
	}
}

func TestCertificatesCreated(t *testing.T) {
	testFast(t)

	cleanup, tmpDir := startCluster(t)
	defer cleanup()

	stateBytes, err := ioutil.ReadFile(path.Join(tmpDir, "test", "state.toml"))
	if err != nil {
		t.Fatal(err)
	}

	var state config.ClusterState
	err = toml.Unmarshal(stateBytes, &state)
	if err != nil {
		t.Fatal(err)
	}

	is, _, err := lxd.InstanceServerConnect()
	if err != nil {
		t.Fatal(err)
	}

	var etcdIP net.IP
	var controllerIP net.IP
	var workerIP net.IP
	var workerName string
	for _, container := range state.Containers {
		if strings.Contains(container, "etcd") {
			etcdIP, err = containers.WaitContainerIP(container, is)
		}
		if strings.Contains(container, "controller") {
			controllerIP, err = containers.WaitContainerIP(container, is)
		}
		if strings.Contains(container, "worker") {
			workerIP, err = containers.WaitContainerIP(container, is)
			workerName = container
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	etcdPath := path.Join(tmpDir, "test", "certificates", "etcd.pem")
	controllerPath := path.Join(tmpDir, "test", "certificates", "kubernetes.pem")
	workerPath := path.Join(tmpDir, "test", "certificates", workerName+".pem")

	// etcd cert
	testHostnames(etcdPath, []string{etcdIP.String(), "127.0.0.1"}, t)

	// controller cert
	testHostnames(controllerPath, []string{controllerIP.String(), "127.0.0.1"}, t)

	// worker cert
	testHostnames(workerPath, []string{workerIP.String(), workerName}, t)
}

func TestStartedCertChains(t *testing.T) {
	testFast(t)

	cleanup, tmpDir := startCluster(t)
	defer cleanup()

	stateBytes, err := ioutil.ReadFile(path.Join(tmpDir, "test", "state.toml"))
	if err != nil {
		t.Fatal(err)
	}

	var state config.ClusterState
	err = toml.Unmarshal(stateBytes, &state)
	if err != nil {
		t.Fatal(err)
	}

	etcdCAPath := path.Join(tmpDir, "test", "certificates", "etcd.pem")
	caPath := path.Join(tmpDir, "test", "certificates", "ca.pem")

	controllerPath := path.Join(tmpDir, "test", "certificates", "kubernetes.pem")
	etcdPath := path.Join(tmpDir, "test", "certificates", "etcd.pem")

	var workerPath string
	for _, container := range state.Containers {
		if strings.Contains(container, "worker") {
			workerPath = path.Join(tmpDir, "test", "certificates", container+".pem")
		}
	}

	testCertSigned(etcdPath, etcdCAPath, t)
	testCertSigned(controllerPath, caPath, t)
	testCertSigned(workerPath, caPath, t)
}

func testHostnames(certPath string, hostnames []string, t *testing.T) {
	certBytes, err := ioutil.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}

	block, _ := pem.Decode(certBytes)
	if err != nil {
		t.Fatal("could not parse certificate:", err)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal("could not parse certificate:", err)
	}

	for _, h := range hostnames {
		err = cert.VerifyHostname(h)
		if err != nil {
			t.Fatal(err)
		}
	}
}
