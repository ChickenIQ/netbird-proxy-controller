package controller

import (
	"fmt"
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

type port struct {
	pod    string
	number int32
}

type listener struct {
	protocols sets.Set[corev1.Protocol]
	hosts     sets.Set[string]
}

func (c *Controller) loadBalancers(services []api.Service) []*corev1ac.ServiceApplyConfiguration {
	listeners := map[port]*listener{}

	for _, s := range services {
		pod, ok := c.pod(ptr.Deref(s.ProxyCluster, ""))
		if !s.Enabled || !ok {
			continue
		}

		p, protocol := port{pod, int32(ptr.Deref(s.ListenPort, 0))}, corev1.ProtocolTCP
		switch ptr.Deref(s.Mode, api.ServiceModeHttp) {
		case api.ServiceModeHttp:
			p.number = httpsPort
		case api.ServiceModeUdp:
			protocol = corev1.ProtocolUDP
		}

		if p.number == 0 {
			continue
		}

		l := listeners[p]
		if l == nil {
			l = &listener{protocols: sets.New[corev1.Protocol](), hosts: sets.New[string]()}
			listeners[p] = l
		}

		l.protocols.Insert(protocol)
		l.hosts.Insert(s.Domain)
	}

	var svcs []*corev1ac.ServiceApplyConfiguration
	for p, l := range listeners {
		svcs = append(svcs, c.loadBalancer(p, l))
	}

	return svcs
}

func (c *Controller) loadBalancer(p port, l *listener) *corev1ac.ServiceApplyConfiguration {
	spec := corev1ac.ServiceSpec().
		WithSelector(map[string]string{podNameLabel: p.pod}).
		WithType(corev1.ServiceTypeLoadBalancer)

	for _, protocol := range sets.List(l.protocols) {
		spec.WithPorts(corev1ac.ServicePort().
			WithName(strings.ToLower(string(protocol))).
			WithTargetPort(intstr.FromInt32(p.number)).
			WithProtocol(protocol).
			WithPort(p.number))
	}

	name := fmt.Sprintf("%s-%d", p.pod, p.number)
	return c.service(name, p.pod, strings.Join(sets.List(l.hosts), ","), spec)
}
