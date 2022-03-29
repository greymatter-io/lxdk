#!/bin/bash

set -eu

curl https://raw.githubusercontent.com/cri-o/cri-o/main/scripts/get | bash

#source /etc/os-release

#sudo apt-get install -y wget

#sudo sh -c "echo 'deb http://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/xUbuntu_${VERSION_ID}/ /' > /etc/apt/sources.list.d/devel:kubic:libcontainers:stable.list"

#wget -nv https://download.opensuse.org/repositories/devel:kubic:libcontainers:stable/xUbuntu_${VERSION_ID}/Release.key -O- | sudo apt-key add -

#apt-get update -qq && apt-get install -y \
  #libbtrfs-dev \
  #containers-common \
  #git \
  #libassuan-dev \
  #libdevmapper-dev \
  #libglib2.0-dev \
  #libc6-dev \
  #libgpgme-dev \
  #libgpg-error-dev \
  #libseccomp-dev \
  #libsystemd-dev \
  #libselinux1-dev \
  #pkg-config \
  #go-md2man \
  #cri-o-runc \
  #libudev-dev \
  #software-properties-common \
  #gcc \
  #make

#tmpdir="$(mktemp -d /tmp/kubedee-crio-XXXXXX)"

#(
    #wget "https://go.dev/dl/go1.18.linux-amd64.tar.gz"
    #rm -rf /usr/local/go && tar -C /usr/local -xzf go1.18.linux-amd64.tar.gz
    #export PATH=$PATH:/usr/local/go/bin
    #cp /usr/local/go/bin/* /usr/local/bin

    #cd $tmpdir

    #git clone https://github.com/cri-o/cri-o
    #cd cri-o

    #make
    #sudo make install

    #cd ..

    #git clone https://github.com/containers/conmon
    #cd conmon
    #make
    #sudo make install

    #cd ..

    #cd cri-o

    #sudo make install.systemd
#)

#rm -rf $tmpdir
