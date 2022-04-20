package config

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"

	"github.com/BurntSushi/toml"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh"
)

// Critter's thoughts on this: I wrote this with the intention of supporting
// multiple remote state implementations, and drafted a quick git
// implementation. If we want to be more opinionated and only support one method
// of managing state, then we don't need this. This could also be taken further,
// by eliminating the cache directory and letting each implementation handle its
// own cache, which would require adding separate methods to get certs and
// kubeconfigs, but that seems unnecessary.

// ClusterStateManagers are responsible for managing the local cache and remote
// state of a cluster.
type ClusterStateManager interface {
	// List lists the clusters this ClusterStateManager has access to.
	List() ([]string, error)

	// Cache writes the cluster to the local cache. Cache should not access
	// any remote resources.
	Cache(state ClusterState) error

	// Pull pulls a cluster configuration from wherever this
	// ClusterStateManager stores its persistent state. For remote
	// implementations, this should be the remote location, and not the
	// cache directory. While Pull only returns the cluster state, it will
	// also cache all cluster resources (certificates and kubeconfigs).
	Pull(cluster string) (ClusterState, error)

	// Push pushes a cluster to wherever this ClusterStateManager stores its
	// persistent state.
	Push(state ClusterState) error
}

func ClusterStateManagerFromContext(ctx *cli.Context) (ClusterStateManager, error) {
	switch ctx.String("cluster-state-manager") {
	case "local":
		cacheDir := ctx.String("cache")
		return LocalStateManager{Dir: cacheDir}, nil
	case "git":
		cacheDir := ctx.String("cache")
		url := ctx.String("git-url")
		keyPath := ctx.String("git-keypath")
		return GitStateManager{Dir: cacheDir, URL: url, KeyPath: keyPath}, nil
	}

	return nil, nil
}

// LocalStateManager treats the cache as its persistent state (Push is a noop).
// Most other state managers will have the same Cache method.
type LocalStateManager struct {
	Dir string
}

func (mngr LocalStateManager) List() ([]string, error) {
	dirEntries, err := os.ReadDir(mngr.Dir)
	if err != nil {
		return nil, err
	}

	var clusters []string
	for _, entry := range dirEntries {
		if entry.IsDir() {
			clusters = append(clusters, entry.Name())
		}
	}

	return clusters, nil
}

func (mngr LocalStateManager) Cache(state ClusterState) error {
	cacheDir := path.Join(mngr.Dir, state.Name)
	err := os.MkdirAll(cacheDir, 0777)
	if err != nil {
		return fmt.Errorf("error creating %s: %w", cacheDir, err)
	}

	clusterConfigPath := path.Join(cacheDir, "state.toml")
	w, err := os.Create(clusterConfigPath)
	if err != nil {
		return err
	}

	enc := toml.NewEncoder(w)
	err = enc.Encode(state)
	if err != nil {
		return err
	}

	return nil
}

func (mngr LocalStateManager) Pull(cluster string) (ClusterState, error) {
	var state ClusterState
	clusterConfigPath := path.Join(mngr.Dir, cluster, "state.toml")
	_, err := toml.DecodeFile(clusterConfigPath, &state)
	if err != nil {
		return state, fmt.Errorf("error loading config file %w", err)
	}

	return state, nil
}

func (mngr LocalStateManager) Push(state ClusterState) error {
	return nil
}

// GitStateManager pulls its cluster states from a git repository. It expects
// all clusters to be in their own folder at the root level of the repository.
type GitStateManager struct {
	Dir string
	URL string

	KeyPath string
}

func (mngr GitStateManager) cloneToMemory() (*git.Repository, billy.Filesystem, error) {
	sshKey, err := ioutil.ReadFile(mngr.KeyPath)
	if err != nil {
		return nil, nil, err
	}

	publicKey, err := gitssh.NewPublicKeys("git", sshKey, os.Getenv("LXDK_SSH_PASSWORD"))
	if err != nil {
		return nil, nil, err
	}

	publicKey.HostKeyCallbackHelper = gitssh.HostKeyCallbackHelper{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	fs := memfs.New()
	r, err := git.Clone(memory.NewStorage(), fs, &git.CloneOptions{
		Auth: publicKey,
		URL:  mngr.URL,
	})

	return r, fs, err
}

func (mngr GitStateManager) List() ([]string, error) {
	_, fs, err := mngr.cloneToMemory()
	if err != nil {
		return nil, err
	}

	finfos, err := fs.ReadDir(".")
	if err != nil {
		return nil, err
	}

	for _, finfo := range finfos {
		fmt.Println(finfo.Name())
	}

	return nil, nil
}

func (mngr GitStateManager) Cache(state ClusterState) error {
	return nil
}

func (mngr GitStateManager) Pull(cluster string) (ClusterState, error) {
	_, fs, err := mngr.cloneToMemory()
	if err != nil {
		return ClusterState{}, err
	}

	err = copyDir(fs, cluster, path.Join(mngr.Dir, cluster))
	if err != nil {
		return ClusterState{}, err
	}

	return ClusterState{}, nil
}

func (mngr GitStateManager) Push(state ClusterState) error {
	return nil
}

func copyDir(fs billy.Filesystem, src string, dest string) error {
	finfos, err := fs.ReadDir(src)
	if err != nil {
		return err
	}

	err = os.MkdirAll(dest, 0777)
	if err != nil {
		return err
	}

	for _, f := range finfos {
		fmt.Println(f.Name())
		if f.IsDir() {
			err = copyDir(fs, path.Join(src, f.Name()), path.Join(dest, f.Name()))
			if err != nil {
				return err
			}
		} else {
			file, err := fs.Open(path.Join(src, f.Name()))
			if err != nil {
				return err
			}

			var buffer bytes.Buffer
			_, err = io.Copy(&buffer, file)
			if err != nil {
				return err
			}

			err = ioutil.WriteFile(path.Join(dest, f.Name()), buffer.Bytes(), 777)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
