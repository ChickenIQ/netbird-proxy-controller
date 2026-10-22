package controller

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/netbirdio/netbird/shared/management/http/api"
	corev1 "k8s.io/api/core/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
)

func (c *Controller) validations(ctx context.Context, domains []api.ReverseProxyDomain) []*corev1ac.ServiceApplyConfiguration {
	var svcs []*corev1ac.ServiceApplyConfiguration

	for _, d := range domains {
		cluster := ptr.Deref(d.TargetCluster, "")
		pod, ok := c.pod(cluster)
		if d.Type != api.ReverseProxyDomainTypeCustom || !ok {
			continue
		}

		svcs = append(svcs, c.validation(pod, cluster, d))
		if d.Validated {
			continue
		}

		if err := c.netbird.ReverseProxyDomains.Validate(ctx, d.Id); err != nil {
			log.Printf("Reconcile: validate domain %s: %v", d.Domain, err)
		}
	}

	return svcs
}

func (c *Controller) validation(pod, cluster string, d api.ReverseProxyDomain) *corev1ac.ServiceApplyConfiguration {
	spec := corev1ac.ServiceSpec().
		WithType(corev1.ServiceTypeExternalName).
		WithExternalName(cluster)

	name := fmt.Sprintf("%s-%s", pod, strings.ToLower(d.Id))
	return c.service(name, pod, "validation."+d.Domain, spec)
}
