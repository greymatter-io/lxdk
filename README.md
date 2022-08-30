# lxdk

[![Build status](https://badge.buildkite.com/2143bceb3a4a9e831febe833965907bc68a9e95941dfd73b29.svg)](https://buildkite.com/greymatter/lxdk)

Launching Kubernetes clusters on LXD

## Building

```
./scripts/bootstrap
./scripts/build
```

## Dependencies

* packer
* cfssl

## Getting Started

Build the LXD images with Packer

```
cd packer
packer init
packer build build.pkr.hcl
```

