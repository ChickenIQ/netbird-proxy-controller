package controller

import (
	"context"
	"log"

	"github.com/netbirdio/netbird/shared/management/http/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
)

func (c *Controller) validations(ctx context.Context, domains []api.ReverseProxyDomain) []*corev1ac.ServiceApplyConfiguration {
	hosts := map[string]sets.Set[string]{}

	for _, d := range domains {
		pod, ok := c.pod(ptr.Deref(d.TargetCluster, ""))
		if d.Type != api.ReverseProxyDomainTypeCustom || !ok {
			continue
		}

		if hosts[pod] == nil {
			hosts[pod] = sets.New[string]()
		}

		hosts[pod].Insert("validation." + d.Domain)
		if d.Validated {
			continue
		}

		if err := c.netbird.ReverseProxyDomains.Validate(ctx, d.Id); err != nil {
			log.Printf("Reconcile: validate domain %s: %v", d.Domain, err)
		}
	}

	var svcs []*corev1ac.ServiceApplyConfiguration
	for pod, h := range hosts {
		svcs = append(svcs, c.validation(pod, h))
	}

	return svcs
}

func (c *Controller) validation(pod string, hosts sets.Set[string]) *corev1ac.ServiceApplyConfiguration {
	spec := corev1ac.ServiceSpec().
		WithType(corev1.ServiceTypeExternalName).
		WithExternalName(c.cluster(pod))

	return c.service(pod+"-validation", pod, hosts, spec)
}
