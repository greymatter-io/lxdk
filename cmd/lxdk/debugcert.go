package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"path"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

// debugCertCmd is the debug-cert command, which checks that the provided cert
// at --cert-path is signed by a cert in the cache directory.
var debugCertCmd = &cli.Command{
	Name:  "debug-cert",
	Usage: "check if a cert is correctly signed by an lxdk-managed CA",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "cert-path",
			Usage: "path to cert to verify",
		},
	},
	Action: doDebugCert,
}

func doDebugCert(ctx *cli.Context) error {
	cacheDir := ctx.String("cache")

	clusterName := ctx.Args().First()
	if clusterName == "" {
		return errors.New("must supply cluster name")
	}

	checkCertPath := ctx.String("cert-path")
	if checkCertPath == "" {
		return errors.New("must set --cert-path")
	}

	cachedCertsPath := path.Join(cacheDir, clusterName, "certificates")

	caPath := path.Join(cachedCertsPath, "ca.pem")
	caAggregationPath := path.Join(cachedCertsPath, "ca-aggregation.pem")
	caEtcdPath := path.Join(cachedCertsPath, "ca-etcd.pem")
	paths := []string{caPath, caAggregationPath, caEtcdPath}

	for _, path := range paths {
		signed, err := certSigned(checkCertPath, caPath)
		if err != nil {
			return err
		}
		if signed {
			fmt.Printf("cert %s signed by CA at %s", checkCertPath, path)
			return nil
		}

	}

	return errors.New("no cert signature found for cert " + checkCertPath)
}

func certSigned(certPath, caPath string) (bool, error) {
	certBytes, err := ioutil.ReadFile(certPath)
	if err != nil {
		return false, err
	}

	caBytes, err := ioutil.ReadFile(caPath)
	if err != nil {
		return false, err
	}

	roots := x509.NewCertPool()
	if ok := roots.AppendCertsFromPEM(caBytes); !ok {
		return false, err
	}

	block, _ := pem.Decode(certBytes)
	if err != nil {
		return false, err
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, err
	}

	opts := x509.VerifyOptions{
		Roots: roots,
	}

	if _, err := cert.Verify(opts); err != nil {
		return false, nil
	}

	return true, nil
}
