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

type listener struct {
	ports map[string]*corev1ac.ServicePortApplyConfiguration
	hosts sets.Set[string]
}

func (c *Controller) loadBalancers(services []api.Service) []*corev1ac.ServiceApplyConfiguration {
	listeners := map[string]*listener{}

	for _, s := range services {
		pod, ok := c.pod(ptr.Deref(s.ProxyCluster, ""))
		if !s.Enabled || !ok {
			continue
		}

		number, protocol := int32(ptr.Deref(s.ListenPort, 0)), corev1.ProtocolTCP
		switch ptr.Deref(s.Mode, api.ServiceModeHttp) {
		case api.ServiceModeHttp:
			number = httpsPort
		case api.ServiceModeUdp:
			protocol = corev1.ProtocolUDP
		}

		if number == 0 {
			continue
		}

		l := listeners[pod]
		if l == nil {
			l = &listener{ports: map[string]*corev1ac.ServicePortApplyConfiguration{}, hosts: sets.New[string]()}
			listeners[pod] = l
		}

		name := fmt.Sprintf("%s-%d", strings.ToLower(string(protocol)), number)
		l.ports[name] = corev1ac.ServicePort().
			WithTargetPort(intstr.FromInt32(number)).
			WithProtocol(protocol).
			WithPort(number).
			WithName(name)
		l.hosts.Insert(s.Domain)
	}

	var svcs []*corev1ac.ServiceApplyConfiguration
	for pod, l := range listeners {
		svcs = append(svcs, c.loadBalancer(pod, l))
	}

	return svcs
}

func (c *Controller) loadBalancer(pod string, l *listener) *corev1ac.ServiceApplyConfiguration {
	spec := corev1ac.ServiceSpec().
		WithSelector(map[string]string{podNameLabel: pod}).
		WithType(corev1.ServiceTypeLoadBalancer)

	for _, name := range slices.Sorted(maps.Keys(l.ports)) {
		spec.WithPorts(l.ports[name])
	}

	return c.service(pod, pod, l.hosts, spec)
}
