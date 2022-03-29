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
      "apt-get install -y libgpgme11 kitty-terminfo htop jq socat curl iptables cloud-init",
      "rm -rf /var/cache/apt",
      "mkdir -p /opt/cni/bin",
      "curl -fsSL https://github.com/containernetworking/plugins/releases/download/v1.1.1/cni-plugins-linux-amd64-v1.1.1.tgz | tar -xzC /opt/cni/bin",
      "curl -fsSLo /opt/cni/bin/flannel https://github.com/flannel-io/cni-plugin/releases/download/v1.0.1/flannel-amd64",
      "chmod +x /opt/cni/bin/flannel",
      ]
  }

  provisioner "shell" {
    only             = ["lxd.kubedee-worker", "lxd.kubedee-controller"]
    environment_vars = ["k8s_version=${var.k8s_version}", "crio_url=${local.crio_url[var.crio_version]}"]
    scripts = [
      "./scripts/download-k8s.sh",
      "./scripts/download-crio.sh",
      "./scripts/download-registry.sh",
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

  provisioner "shell" {
    only             = ["lxd.kubedee-controller", "lxd.kubedee-worker"]
    inline           = [
        "sudo systemctl daemon-reload",
        "sudo systemctl enable crio",
        "sudo systemctl start crio",
    ]
  }

  /*# crio.service*/
  /*provisioner "file" {*/
    /*only        = ["lxd.kubedee-controller", "lxd.kubedee-worker"]*/
    /*source      = "./templates/crio/crio.service.pkrtpl.hcl"*/
    /*destination = "/etc/systemd/system/crio.service"*/
  /*}*/

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

  # oci-registry.service
  provisioner "file" {
    only        = ["lxd.kubedee-worker", "lxd.kubedee-controller"]
    source      = "./templates/oci-registry.service.pkrtpl.hcl"
    destination = "/etc/systemd/system/oci-registry.service"
  }

  provisioner "shell" {
    only        = ["lxd.kubedee-worker", "lxd.kubedee-controller"]
    inline      = [
        "mkdir -p /etc/docker/registry",
        "mkdir -p /etc/crio",
        "mkdir -p /etc/kubernetes/config",
    ]
  }

  # registry config.yml
  provisioner "file" {
    only             = ["lxd.kubedee-worker", "lxd.kubedee-controller"]
    source      = "./templates/registry_config.yml.pkrtpl.hcl"
    destination = "/etc/docker/registry/config.yml"
  }

  # scheduler config
  provisioner "file" {
    only             = ["lxd.kubedee-worker", "lxd.kubedee-controller"]
    source      = "./templates/kube-scheduler.yaml.pkrtpl.hcl"
    destination = "/etc/kubernetes/config/kube-scheduler.yaml"
  }

  /*provisioner "file" {*/
    /*only             = ["lxd.kubedee-worker", "lxd.kubedee-controller"]*/
    /*sources     = [*/
        /*"./templates/crio/crictl.yaml.pkrtpl.hcl",*/
        /*"./templates/crio/crio-umount.conf.pkrtpl.hcl",*/
        /*"./templates/crio/crio.service.pkrtpl.hcl",*/
        /*"./templates/crio/policy.json.pkrtpl.hcl",*/
    /*]*/
    /*destination = "/etc/crio/"*/
  /*}*/
}
