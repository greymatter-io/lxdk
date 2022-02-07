package main

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/greymatter-io/lxdk/testutils"
	"github.com/urfave/cli/v2"
)

func TestCreateCACreatesObjects(t *testing.T) {
	tmpDir, cleanup, err := testutils.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	conf := CAConfig{
		Name: "test-ca",
		CN:   "Tests",
		Dir:  tmpDir,
	}
	err = CreateCA(conf)
	if err != nil {
		t.Fatal(err)
	}

	objects := []string{conf.Name + ".csr", conf.Name + "-key.pem", conf.Name + ".pem"}
	for _, filename := range objects {
		t.Log("checking file: " + filename)

		fullPath := path.Join(tmpDir, filename)
		_, err = os.Stat(fullPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				t.Fatalf("file %s was not created", filename)
			}
			t.Fatal(err)
		}
	}
}

func TestCreateCertCreatesObjects(t *testing.T) {
	tmpDir, cleanup, err := testutils.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	caConf := CAConfig{
		Name: "test-ca",
		CN:   "Tests",
		Dir:  tmpDir,
	}
	err = CreateCA(caConf)
	if err != nil {
		t.Fatal("error creating CA before creating cert:", err)
	}

	_, err = WriteCAConfig(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	certConf := CertConfig{
		Name: "test-cert",
		CN:   "test-cert",
		CA:   caConf,
		Dir:  tmpDir,
	}
	err = createCert(certConf)
	if err != nil {
		t.Fatal(err)
	}

	objects := []string{certConf.Name + ".csr", certConf.Name + "-key.pem", certConf.Name + ".pem"}
	for _, filename := range objects {
		t.Log("checking file: " + filename)

		fullPath := path.Join(tmpDir, filename)
		_, err = os.Stat(fullPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				t.Fatalf("file %s was not created", filename)
			}
			t.Fatal(err)
		}
	}
}

func TestCertChains(t *testing.T) {
	tmpDir, cleanup, err := testutils.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	flags := flag.NewFlagSet("testflags", flag.ErrorHandling(2))
	ctx := cli.NewContext(cli.NewApp(), flags, &cli.Context{})

	os.Setenv("LXDK_CACHE", tmpDir)

	app.RunContext(ctx.Context, []string{"lxdk", "create", "test", "--cache", tmpDir})

	caPath := path.Join(tmpDir, "test", "certificates", "ca.pem")
	caAggregationPath := path.Join(tmpDir, "test", "certificates", "ca-aggregation.pem")

	adminPath := path.Join(tmpDir, "test", "certificates", "admin.pem")
	aggregationClientPath := path.Join(tmpDir, "test", "certificates", "aggregation-client.pem")
	kubeControllerManagerPath := path.Join(tmpDir, "test", "certificates", "kube-controller-manager.pem")
	kubeSchedulerPath := path.Join(tmpDir, "test", "certificates", "kube-scheduler.pem")

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
