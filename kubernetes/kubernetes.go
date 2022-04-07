package kubernetes

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rbac "k8s.io/client-go/applyconfigurations/rbac/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func WaitAPIServerReady(clientset kubernetes.Clientset) error {
	_, err := clientset.RbacV1().ClusterRoles().List(context.Background(), v1.ListOptions{})
	for c := 0; c < 50 && err != nil; c++ {
		log.Default().Println("waiting for API server...", err.Error())
		time.Sleep(3 * time.Second)
		_, err = clientset.RbacV1().ClusterRoles().List(context.Background(), v1.ListOptions{})
	}

	return err
}

func WaitNode(clientset kubernetes.Clientset, name string) error {
	_, err := clientset.CoreV1().Nodes().Get(context.Background(), name, v1.GetOptions{})
	for c := 0; c < 50 && err != nil; c++ {
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		log.Default().Printf("waiting for node: %s: %s", name, err.Error())
		time.Sleep(3 * time.Second)
		_, err = clientset.RbacV1().ClusterRoles().Get(context.Background(), "", v1.GetOptions{})
	}

	return err
}

func ConfigureRBAC(clientset kubernetes.Clientset) error {
	apiVersion := "rbac.authorization.k8s.io/v1"

	apiToKubelet := rbac.ClusterRole("system:kube-apiserver-to-kubelet")
	apiToKubelet.APIVersion = &apiVersion
	apiToKubelet.Annotations = map[string]string{
		"rbac.authorization.kubernetes.io/autoupdate": "true",
	}
	apiToKubelet.Labels = map[string]string{
		"kubernetes.io/bootstrapping": "rbac-defaults",
	}
	apiToKubelet.Rules = []rbac.PolicyRuleApplyConfiguration{
		{
			APIGroups: []string{""},
			Resources: []string{
				"nodes/proxy",
				"nodes/stats",
				"nodes/log",
				"nodes/spec",
				"nodes/metrics",
			},
			Verbs: []string{"*"},
		},
	}

	_, err := clientset.RbacV1().ClusterRoles().Apply(context.Background(), apiToKubelet, v1.ApplyOptions{
		FieldManager: "application/apply-patch",
	})
	if err != nil {
		return fmt.Errorf("could not create cluster role: %w", err)
	}

	kubeAPIServer := rbac.ClusterRoleBinding("system:kube-apiserver")
	roleRefAPIGroup := "rbac.authorization.k8s.io"
	roleRefKind := "ClusterRole"
	roleRefName := "system:kube-apiserver-to-kubelet"
	roleRef := rbac.RoleRefApplyConfiguration{
		APIGroup: &roleRefAPIGroup,
		Kind:     &roleRefKind,
		Name:     &roleRefName,
	}

	namespace := ""
	apiToKubelet.APIVersion = &apiVersion
	kubeAPIServer.Namespace = &namespace
	kubeAPIServer.RoleRef = &roleRef

	subjectsAPIGroup := "rbac.authorization.k8s.io"
	subjectsKind := "User"
	subjectsName := "kubernetes"

	kubeAPIServer.Subjects = []rbac.SubjectApplyConfiguration{
		{
			APIGroup: &subjectsAPIGroup,
			Kind:     &subjectsKind,
			Name:     &subjectsName,
		},
	}

	_, err = clientset.RbacV1().ClusterRoleBindings().Apply(context.Background(), kubeAPIServer, v1.ApplyOptions{
		FieldManager: "application/apply-patch",
	})
	if err != nil {
		return fmt.Errorf("could not create cluster role binding: %w", err)
	}

	return nil
}

func GetClientset(filename string) (*kubernetes.Clientset, error) {
	adminKfg, err := clientcmd.BuildConfigFromFlags("", filename)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(adminKfg)
}
