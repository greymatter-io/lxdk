package testutils

import (
	"io/ioutil"
	"os"
)

// tempdir creates a temporary directory in /tmp and returns the dir and a
// function to delete it.
func TempDir() (tmpDir string, cleanup func() error, err error) {
	osTempdir := os.TempDir()

	tmpDir, err = ioutil.TempDir(osTempdir, "*")
	if err != nil {
		return "", nil, err
	}

	cleanup = func() error {
		return os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup, nil
}
