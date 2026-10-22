package controller

import (
	"context"
	"log"

	"github.com/netbirdio/netbird/shared/management/http/api"
	corev1 "k8s.io/api/core/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

func (c *Controller) validate(ctx context.Context, p *proxy, d api.ReverseProxyDomain) {
	p.validations.Insert("validation." + d.Domain)
	if d.Validated {
		return
	}

	if err := c.netbird.ReverseProxyDomains.Validate(ctx, d.Id); err != nil {
		log.Printf("Reconcile: validate domain %s: %v", d.Domain, err)
	}
}

func (c *Controller) validation(cluster string, p *proxy) *corev1ac.ServiceApplyConfiguration {
	spec := corev1ac.ServiceSpec().
		WithType(corev1.ServiceTypeExternalName).
		WithExternalName(cluster)

	return c.service(p.pod+"-validation", p.pod, p.validations, spec)
}
