# lxdk

Launching Kubernetes clusters on LXD

## Overview

lxdk is a command line tool for managing Kubernetes clusters on LXD.

## Requirements

* [packer 1.7+](https://releases.hashicorp.com/packer/)
* Privileged access to an LXD remote
* The [CFSSL tool suite](https://github.com/cloudflare/cfssl)
* Go 1.17+

## Getting Started

Switch to the LXD remote where you'd like to launch a cluster.

```
lxc remote switch $NAME
```

_Note: point at the correct remote before doing a Packer build_

Clone this repo and build the base LXD images.

```
git clone git@github.com:greymatter-io/lxdk.git
cd lxdk/packer
packer init
packer build build.pkr.hcl
```

This will take several minutes, especially the first time.

The built images can be viewed with

```
lxc image list lxdk-
```

Next, build the `lxdk` command line tool itself.

```
./scripts/build
```

The binary will be in **bin/**. Move or symlink it to your PATH.

## Usage

Create a cluster backed by the default storage pool.

```
lxdk up --storage-pool default $CLUSTER
```

## Building

```
./scripts/build
```

## Acknowledgements

lxdk is heavily inspired by the code and configurations of Michael Schu's original
[kubedee](https://github.com/schu/kubedee), and zer0def's [fork](https://github.com/zer0def/kubedee).

