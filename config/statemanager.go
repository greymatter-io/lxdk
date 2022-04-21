package config

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
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
		gsm := GitStateManager{URL: url, KeyPath: keyPath}
		gsm.Dir = cacheDir
		return gsm, nil
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
	LocalStateManager

	URL     string
	KeyPath string
}

func (mngr GitStateManager) sshAuthMethod() (gitssh.AuthMethod, error) {
	if mngr.KeyPath == "" {
		return nil, fmt.Errorf("must set --git-keypath")
	}

	if os.Getenv("LXDK_SSH_PASSWORD") == "" {
		return nil, fmt.Errorf("LXDK_SSH_PASSWORD is not set")
	}

	sshKey, err := ioutil.ReadFile(mngr.KeyPath)
	if err != nil {
		return nil, err
	}

	publicKey, err := gitssh.NewPublicKeys("git", sshKey, os.Getenv("LXDK_SSH_PASSWORD"))
	if err != nil {
		return nil, err
	}

	publicKey.HostKeyCallbackHelper = gitssh.HostKeyCallbackHelper{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	return publicKey, nil
}

func (mngr GitStateManager) cloneToMemory() (*git.Repository, billy.Filesystem, error) {
	publicKey, err := mngr.sshAuthMethod()
	if err != nil {
		return nil, nil, err
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

func (mngr GitStateManager) Pull(cluster string) (ClusterState, error) {
	_, fs, err := mngr.cloneToMemory()
	if err != nil {
		return ClusterState{}, err
	}

	local_fs := osfs.New(mngr.Dir)
	err = copyDir(fs, local_fs, cluster, cluster)
	if err != nil {
		return ClusterState{}, err
	}

	stateBytes, err := ioutil.ReadFile(path.Join(mngr.Dir, cluster, "state.toml"))
	if err != nil {
		return ClusterState{}, err
	}

	var state ClusterState

	_, err = toml.Decode(string(stateBytes), &state)
	if err != nil {
		return ClusterState{}, err
	}

	return state, nil
}

func (mngr GitStateManager) Push(state ClusterState) error {
	rep, rep_fs, err := mngr.cloneToMemory()
	if err != nil {
		return err
	}

	local_fs := osfs.New(mngr.Dir)
	err = copyDir(local_fs, rep_fs, state.Name, state.Name)
	if err != nil {
		return err
	}

	wt, err := rep.Worktree()
	if err != nil {
		return err
	}

	wt.Add(state.Name)

	commitOpts := git.CommitOptions{
		All: true,
	}
	_, err = wt.Commit("lxdk-"+time.Now().String(), &commitOpts)
	if err != nil {
		return err
	}

	publicKey, err := mngr.sshAuthMethod()
	if err != nil {
		return err
	}

	pushOpts := git.PushOptions{
		Auth: publicKey,
	}

	err = rep.Push(&pushOpts)
	if err != nil {
		return err
	}

	return nil
}

func copyDir(fs_src, fs_dest billy.Filesystem, src string, dest string) error {
	finfos, err := fs_src.ReadDir(src)
	if err != nil {
		return err
	}

	err = os.MkdirAll(dest, 0777)
	if err != nil {
		return err
	}

	for _, f := range finfos {
		if f.IsDir() {
			err = copyDir(fs_src, fs_dest, path.Join(src, f.Name()), path.Join(dest, f.Name()))
			if err != nil {
				return err
			}
		} else {
			file, err := fs_src.Open(path.Join(src, f.Name()))
			if err != nil {
				return err
			}

			var buffer bytes.Buffer
			_, err = io.Copy(&buffer, file)
			if err != nil {
				return err
			}

			fs_dest.Remove(path.Join(dest, f.Name()))
			destfile, err := fs_dest.Create(path.Join(dest, f.Name()))
			if err != nil {
			}

			_, err = destfile.Write(buffer.Bytes())
			if err != nil {
				return err
			}
		}
	}

	return nil
}
