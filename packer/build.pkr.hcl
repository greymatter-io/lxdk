packer {
  required_plugins {
    lxd = {
      version = ">=1.0.0"
      source  = "github.com/hashicorp/lxd"
    }
  }
}

variable "k8s_version" {
  type        = string
  description = "kubernetes version"
  default     = "v1.21.1"
}

variable "crio_version" {
  type = string
  default = "v1.21.4"
  description = "The cri-o release to download; Must have a corresponding crio_url map entry"
}

variable "etcd_version" {
  type = string
  default = "v3.4.14"
  description = "etcd version to download"
}

locals {
  # cri-o releases: https://github.com/cri-o/cri-o/releases
  crio_url = {
    "v1.21.4" = "https://storage.googleapis.com/k8s-conform-cri-o/artifacts/cri-o.amd64.61748dc51bdf1af367b8a68938dbbc81c593b95d.tar.gz"
  }

  # timestamp for our build containers
  ts = formatdate("YYYYMMDDhhmmss", timestamp())
}


source "lxd" "ubuntu_2004" {
  image = "images:ubuntu/20.04"
}

build {

  source "lxd.ubuntu_2004" {
    name           = "kubedee-etcd"
    output_image   = "kubedee-etcd"
    container_name = "kubedee-etcd-${local.ts}"
  }

  source "lxd.ubuntu_2004" {
    name           = "kubedee-controller"
    output_image   = "kubedee-controller"
    container_name = "kubedee-controller-${local.ts}"
  }

  source "lxd.ubuntu_2004" {
    name           = "kubedee-worker"
    output_image   = "kubedee-worker"
    container_name = "kubedee-worker-${local.ts}"
  }

  provisioner "shell" {
    inline = [
      "apt-get update -y",
      "apt-get install -y libgpgme11 kitty-terminfo htop jq socat curl",
      "rm -rf /var/cache/apt",
    ]
  }

  provisioner "shell" {
    only             = ["lxd.kubedee-worker", "lxd.kubedee-controller"]
    environment_vars = ["k8s_version=${var.k8s_version}", "crio_url=${local.crio_url[var.crio_version]}"]
    scripts = [
      "./scripts/download-k8s.sh",
      "./scripts/download-crio.sh",
    ]
  }

  provisioner "shell" {
    only             = ["lxd.kubedee-etcd"]
    environment_vars = ["etcd_version=${var.etcd_version}"]
    scripts = [
      "./scripts/download-etcd.sh",
    ]
  }

  # etcd.service - initcluster container name, client urls
  provisioner "file" {
    only        = ["lxd.kubedee-etcd"]
    source      = "./templates/etcd.service.pkrtpl.hcl"
    destination = "/etc/systemd/system/etcd.service"
  }

  # kube-proxy.service - cluster-cidr=10.200.0.0/16
  provisioner "file" {
    only        = ["lxd.kubedee-controller", "lxd.kubedee-worker"]
    source      = "./templates/kube-proxy.service.pkrtpl.hcl"
    destination = "/etc/systemd/system/kube-proxy.service"
  }

  # kubelet.service
  provisioner "file" {
    only        = ["lxd.kubedee-controller", "lxd.kubedee-worker"]
    source      = "./templates/kubelet.service.pkrtpl.hcl"
    destination = "/etc/systemd/system/kubelet.service"
  }

  # crio.service
  provisioner "file" {
    only        = ["lxd.kubedee-controller", "lxd.kubedee-worker"]
    source      = "./templates/crio.service.pkrtpl.hcl"
    destination = "/etc/systemd/system/crio.service"
  }

  #kube-scheduler.service 
  provisioner "file" {
    only        = ["lxd.kubedee-controller"]
    source      = "./templates/kube-scheduler.service.pkrtpl.hcl"
    destination = "/etc/systemd/system/kube-scheduler.service"
  }

  # kube-apiserver.service - admission-plugins, etcdip:2379, 
  #    service-cluster-ip-range, node-port-range,
  provisioner "file" {
    only        = ["lxd.kubedee-controller"]
    source      = "./templates/kube-apiserver.service.pkrtpl.hcl"
    destination = "/etc/systemd/system/kube-apiserver.service"
  }

  # kube-controller-manager.service - cluster-cidr, service-cluster-ip-range
  provisioner "file" {
    only        = ["lxd.kubedee-controller"]
    source      = "./templates/kube-controller-manager.service.pkrtpl.hcl"
    destination = "/etc/systemd/system/kube-controller-manager.service"
  }

}

