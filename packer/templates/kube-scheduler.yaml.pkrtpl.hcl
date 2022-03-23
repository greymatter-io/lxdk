apiVersion: "kubescheduler.config.k8s.io/v1beta1"
kind: KubeSchedulerConfiguration
clientConnection:
  kubeconfig: "/etc/kubernetes/kube-scheduler.kubeconfig"
leaderElection:
  leaderElect: true
