package main

import (
	"errors"
	"os"
	"path"
	"testing"

	"github.com/greymatter-io/lxdk/testutils"
)

func TestCreateCA(t *testing.T) {
	caName := "test-ca"
	caDN := "Tests"

	tmpDir, cleanup, err := testutils.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	err = createCA(tmpDir, caName, caJSON(caDN))
	if err != nil {
		t.Fatal(err)
	}

	objects := []string{caName + ".csr", caName + "-key.pem", caName + ".pem"}
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
