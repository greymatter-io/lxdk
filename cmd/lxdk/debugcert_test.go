package main

import (
	"flag"
	"os"
	"path"
	"testing"

	"github.com/greymatter-io/lxdk/testutils"
	"github.com/urfave/cli/v2"
)

// TestDebugCert tests that the debug-cert command works by ensuring the
// admin.pem cert is properly signed by ca.pem.
func TestDebugCert(t *testing.T) {
	tmpDir, cleanup, err := testutils.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	flags := flag.NewFlagSet("testflags", flag.ErrorHandling(2))
	ctx := cli.NewContext(cli.NewApp(), flags, &cli.Context{})

	os.Setenv("LXDK_CACHE", tmpDir)
	app.RunContext(ctx.Context, []string{"lxdk", "create", "test"})

	err = app.RunContext(ctx.Context, []string{
		"lxdk",
		"debug-cert",
		"--cert-path",
		path.Join(tmpDir, "test", "certificates", "admin.pem"),
		"test",
	})

	if err != nil {
		t.Fatal(err)
	}
}
