// +build linux

/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package linux

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/linux/runctypes"
	"github.com/containerd/containerd/linux/shim"
	"github.com/containerd/containerd/linux/shim/client"
	"github.com/pkg/errors"
)

// loadBundle loads an existing bundle from disk
func loadBundle(id, path, workdir string) *bundle {
	return &bundle{
		id:      id,
		path:    path,
		workDir: workdir,
	}
}

// newBundle creates a new bundle on disk at the provided path for the given id
func newBundle(id, path, workDir string, spec []byte) (b *bundle, err error) {
	if err := os.MkdirAll(path, 0711); err != nil {
		return nil, err
	}
	path = filepath.Join(path, id)
	defer func() {
		if err != nil {
			os.RemoveAll(path)
		}
	}()
	workDir = filepath.Join(workDir, id)
	if err := os.MkdirAll(workDir, 0711); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(workDir)
		}
	}()

	if err := os.Mkdir(path, 0711); err != nil {
		return nil, err
	}
	if err := os.Mkdir(filepath.Join(path, "rootfs"), 0711); err != nil {
		return nil, err
	}
	err = ioutil.WriteFile(filepath.Join(path, configFilename), spec, 0666)
	return &bundle{
		id:      id,
		path:    path,
		workDir: workDir,
	}, err
}

type bundle struct {
	id      string
	path    string
	workDir string
}

// ShimOpt specifies shim options for initialization and connection
type ShimOpt func(*bundle, string, *runctypes.RuncOptions) (shim.Config, client.Opt)

// ShimRemote is a ShimOpt for connecting and starting a remote shim
func ShimRemote(shimBinary, daemonAddress, cgroup string, debug bool, exitHandler func()) ShimOpt {
	return func(b *bundle, ns string, ropts *runctypes.RuncOptions) (shim.Config, client.Opt) {
		return b.shimConfig(ns, ropts),
			client.WithStart(shimBinary, b.shimAddress(ns), daemonAddress, cgroup, debug, exitHandler)
	}
}

// ShimLocal is a ShimOpt for using an in process shim implementation
func ShimLocal(exchange *exchange.Exchange) ShimOpt {
	return func(b *bundle, ns string, ropts *runctypes.RuncOptions) (shim.Config, client.Opt) {
		return b.shimConfig(ns, ropts), client.WithLocal(exchange)
	}
}

// ShimConnect is a ShimOpt for connecting to an existing remote shim
func ShimConnect(onClose func()) ShimOpt {
	return func(b *bundle, ns string, ropts *runctypes.RuncOptions) (shim.Config, client.Opt) {
		return b.shimConfig(ns, ropts), client.WithConnect(b.shimAddress(ns), onClose)
	}
}

// NewShimClient connects to the shim managing the bundle and tasks creating it if needed
func (b *bundle) NewShimClient(ctx context.Context, namespace string, getClientOpts ShimOpt, runcOpts *runctypes.RuncOptions) (*client.Client, error) {
	cfg, opt := getClientOpts(b, namespace, runcOpts)
	return client.New(ctx, cfg, opt)
}

// Delete deletes the bundle from disk
func (b *bundle) Delete() error {
	err := os.RemoveAll(b.path)
	if err == nil {
		return os.RemoveAll(b.workDir)
	}
	// error removing the bundle path; still attempt removing work dir
	err2 := os.RemoveAll(b.workDir)
	if err2 == nil {
		return err
	}
	return errors.Wrapf(err, "Failed to remove both bundle and workdir locations: %v", err2)
}

func (b *bundle) shimAddress(namespace string) string {
	return filepath.Join(string(filepath.Separator), "containerd-shim", namespace, b.id, "shim.sock")
}

func (b *bundle) shimConfig(namespace string, runcOptions *runctypes.RuncOptions) shim.Config {
	var (
		criuPath      string
		runtimeRoot   string
		systemdCgroup bool
	)
	if runcOptions != nil {
		criuPath = runcOptions.CriuPath
		systemdCgroup = runcOptions.SystemdCgroup
		runtimeRoot = runcOptions.RuntimeRoot
	}
	return shim.Config{
		Path:          b.path,
		WorkDir:       b.workDir,
		Namespace:     namespace,
		Criu:          criuPath,
		RuntimeRoot:   runtimeRoot,
		SystemdCgroup: systemdCgroup,
	}
}
