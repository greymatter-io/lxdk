package main

import (
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"path"
	"testing"

	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/lxd"
	"github.com/greymatter-io/lxdk/testutils"
)

// TestCertChains tests that the generated certs are signed by the proper
// generated CA.
func TestCertChains(t *testing.T) {
	tmpDir, cleanup, err := testutils.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	err = createCerts(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	caPath := path.Join(tmpDir, "certificates", "ca.pem")
	caAggregationPath := path.Join(tmpDir, "certificates", "ca-aggregation.pem")

	adminPath := path.Join(tmpDir, "certificates", "admin.pem")
	aggregationClientPath := path.Join(tmpDir, "certificates", "aggregation-client.pem")
	kubeControllerManagerPath := path.Join(tmpDir, "certificates", "kube-controller-manager.pem")
	kubeSchedulerPath := path.Join(tmpDir, "certificates", "kube-scheduler.pem")

	// admin should be signed by "ca"
	testCertSigned(adminPath, caPath, t)

	// aggregation-client should be signed by "ca-aggregation"
	testCertSigned(aggregationClientPath, caAggregationPath, t)

	// kube-controller-manager should be signed by "ca"
	testCertSigned(kubeControllerManagerPath, caPath, t)

	// kube-scheduler should be signed by "ca"
	testCertSigned(kubeSchedulerPath, caPath, t)
}

func testCertSigned(certPath, caPath string, t *testing.T) {
	certBytes, err := ioutil.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}

	caBytes, err := ioutil.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()
	if ok := roots.AppendCertsFromPEM(caBytes); !ok {
		t.Fatal("failed to parse root certificate at", caPath)
	}

	block, _ := pem.Decode(certBytes)
	if err != nil {
		t.Fatal("could not parse certificate:", err)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal("could not parse certificate:", err)
	}

	opts := x509.VerifyOptions{
		Roots: roots,
	}

	if _, err := cert.Verify(opts); err != nil {
		t.Fatal(err)
	}
}

func TestCreateNetwork(t *testing.T) {
	testFast(t)

	is, err := lxd.InstanceServerConnect()
	if err != nil {
		t.Fatal(err)
	}

	state := config.ClusterState{
		Name: "test",
	}

	networkID, err := createNetwork(state)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = is.GetNetwork(networkID)
	if err != nil {
		t.Fatal(err)
	}

	err = is.DeleteNetwork(networkID)
	if err != nil {
		t.Fatal(err)
	}
}
