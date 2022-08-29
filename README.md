# lxdk

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

