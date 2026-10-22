package controller

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/netbirdio/netbird/shared/management/client/rest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	clusterSuffix      = ".internal"
	proxyLabel         = "netbird.io/proxy"
	hostnameAnnotation = "external-dns.alpha.kubernetes.io/hostname"
)

var applyOptions = metav1.ApplyOptions{FieldManager: proxyName, Force: true}

type Config struct {
	Annotations   map[string]string
	Labels        map[string]string
	Image         string
	Replicas      int32
	Namespace     string
	ManagementURL string
}

type Controller struct {
	Config
	netbird *rest.Client
	kube    kubernetes.Interface
}

func New(cfg Config, kube kubernetes.Interface, netbirdClient *rest.Client) *Controller {
	return &Controller{Config: cfg, netbird: netbirdClient, kube: kube}
}

func (c *Controller) Reconcile(ctx context.Context) error {
	if err := c.reconcileProxy(ctx); err != nil {
		return err
	}

	services, err := c.netbird.ReverseProxyServices.List(ctx)
	if err != nil {
		return err
	}

	domains, err := c.netbird.ReverseProxyDomains.List(ctx)
	if err != nil {
		return err
	}

	desired := sets.New[string]()
	client := c.kube.CoreV1().Services(c.Namespace)

	for _, svc := range append(c.loadBalancers(services), c.validations(ctx, domains)...) {
		desired.Insert(*svc.Name)
		if _, err := client.Apply(ctx, svc, applyOptions); err != nil {
			return fmt.Errorf("apply service %s: %w", *svc.Name, err)
		}
	}

	return c.prune(ctx, desired)
}

func (c *Controller) cluster(pod string) string {
	return pod + "." + c.Namespace + clusterSuffix
}

func (c *Controller) pod(cluster string) (string, bool) {
	for i := range c.Replicas {
		pod := fmt.Sprintf("%s-%d", proxyName, i)
		if cluster == c.cluster(pod) {
			return pod, true
		}
	}

	return "", false
}

func (c *Controller) prune(ctx context.Context, desired sets.Set[string]) error {
	client := c.kube.CoreV1().Services(c.Namespace)

	existing, err := client.List(ctx, metav1.ListOptions{LabelSelector: proxyLabel})
	if err != nil {
		return err
	}

	for _, svc := range existing.Items {
		if desired.Has(svc.Name) {
			continue
		}

		log.Printf("Reconcile: deleting service %s", svc.Name)
		if err := client.Delete(ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("delete service %s: %w", svc.Name, err)
		}
	}

	return nil
}

func (c *Controller) service(name, pod string, hosts sets.Set[string], spec *corev1ac.ServiceSpecApplyConfiguration) *corev1ac.ServiceApplyConfiguration {
	return corev1ac.Service(name, c.Namespace).
		WithAnnotations(c.Annotations).
		WithLabels(c.Labels).
		WithAnnotations(map[string]string{hostnameAnnotation: strings.Join(sets.List(hosts), ",")}).
		WithLabels(map[string]string{proxyLabel: pod}).
		WithSpec(spec)
}
