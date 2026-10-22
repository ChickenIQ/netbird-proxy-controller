package controller

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/netbirdio/netbird/shared/management/http/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
)

const (
	httpsPort    = 443
	podNameLabel = "statefulset.kubernetes.io/pod-name"
)

type proxy struct {
	ports       map[string]*corev1ac.ServicePortApplyConfiguration
	validations sets.Set[string]
	hosts       sets.Set[string]
	cluster     string
	pod         string
}

func newProxy(pod, cluster string) *proxy {
	p := &proxy{
		ports:       map[string]*corev1ac.ServicePortApplyConfiguration{},
		hosts:       sets.New(cluster, "*."+cluster),
		validations: sets.New[string](),
		cluster:     cluster,
		pod:         pod,
	}

	p.add(httpsPort, corev1.ProtocolTCP)
	return p
}

func (p *proxy) add(number int32, protocol corev1.Protocol) {
	name := fmt.Sprintf("%s-%d", strings.ToLower(string(protocol)), number)
	p.ports[name] = corev1ac.ServicePort().
		WithTargetPort(intstr.FromInt32(number)).
		WithProtocol(protocol).
		WithPort(number).
		WithName(name)
}

func (p *proxy) listen(s api.Service) {
	if label, ok := strings.CutSuffix(s.Domain, "."+p.cluster); !ok || strings.Contains(label, ".") {
		p.hosts.Insert(s.Domain)
	}

	number := int32(ptr.Deref(s.ListenPort, 0))
	if number == 0 {
		return
	}

	switch ptr.Deref(s.Mode, api.ServiceModeHttp) {
	case api.ServiceModeTcp, api.ServiceModeTls:
		p.add(number, corev1.ProtocolTCP)
	case api.ServiceModeUdp:
		p.add(number, corev1.ProtocolUDP)
	}
}

func (c *Controller) loadBalancer(p *proxy) *corev1ac.ServiceApplyConfiguration {
	spec := corev1ac.ServiceSpec().
		WithSelector(map[string]string{podNameLabel: p.pod}).
		WithType(corev1.ServiceTypeLoadBalancer)

	for _, name := range slices.Sorted(maps.Keys(p.ports)) {
		spec.WithPorts(p.ports[name])
	}

	return c.service(p.pod, p.pod, p.hosts, spec)
}
