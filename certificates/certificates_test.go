package certificates

import (
	"os"
	"path"
	"testing"

	"github.com/greymatter-io/lxdk/testutils"
	"github.com/pkg/errors"
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
	err = CreateCert(certConf)
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
