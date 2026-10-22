package controller

import (
	"context"
	"fmt"
	"log"

	"github.com/netbirdio/netbird/shared/management/http/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
)

const (
	proxyName   = "netbird-proxy"
	tokenSecret = "netbird-proxy-token"
	tokenKey    = "token"
	certsDir    = "/certs"
)

func (c *Controller) reconcileProxy(ctx context.Context) error {
	if err := c.ensureToken(ctx); err != nil {
		return err
	}

	if _, err := c.kube.AppsV1().StatefulSets(c.Namespace).Apply(ctx, c.statefulSet(), applyOptions); err != nil {
		return fmt.Errorf("apply statefulset %s: %w", proxyName, err)
	}

	return nil
}

func (c *Controller) ensureToken(ctx context.Context) error {
	secrets := c.kube.CoreV1().Secrets(c.Namespace)

	_, err := secrets.Get(ctx, tokenSecret, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		return err
	}

	tokens, err := c.netbird.ReverseProxyTokens.List(ctx)
	if err != nil {
		return err
	}

	for _, t := range tokens {
		if t.Name != c.Namespace || t.Revoked {
			continue
		}

		log.Printf("Reconcile: revoking proxy token %s", t.Id)
		if err := c.netbird.ReverseProxyTokens.Delete(ctx, t.Id); err != nil {
			return fmt.Errorf("revoke proxy token %s: %w", t.Id, err)
		}
	}

	token, err := c.netbird.ReverseProxyTokens.Create(ctx, api.ProxyTokenRequest{Name: c.Namespace, ExpiresIn: new(0)})
	if err != nil {
		return fmt.Errorf("create proxy token: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: tokenSecret},
		StringData: map[string]string{tokenKey: token.PlainToken},
	}

	log.Printf("Reconcile: creating secret %s", tokenSecret)
	_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
	return err
}

func (c *Controller) statefulSet() *appsv1ac.StatefulSetApplyConfiguration {
	labels := map[string]string{"app.kubernetes.io/name": proxyName}

	podName := corev1ac.EnvVarSource().
		WithFieldRef(corev1ac.ObjectFieldSelector().WithFieldPath("metadata.name"))

	token := corev1ac.EnvVarSource().
		WithSecretKeyRef(corev1ac.SecretKeySelector().WithName(tokenSecret).WithKey(tokenKey))

	container := corev1ac.Container().
		WithVolumeMounts(corev1ac.VolumeMount().WithName("certs").WithMountPath(certsDir)).
		WithImage(c.Image).
		WithName("proxy").
		WithEnv(
			env("POD_NAME").WithValueFrom(podName),
			env("NB_PROXY_TOKEN").WithValueFrom(token),
			env("NB_PROXY_ACME_CERTIFICATES").WithValue("true"),
			env("NB_PROXY_CERTIFICATE_DIRECTORY").WithValue(certsDir),
			env("NB_PROXY_DOMAIN").WithValue(c.cluster("$(POD_NAME)")),
			env("NB_PROXY_MANAGEMENT_ADDRESS").WithValue(c.ManagementURL),
			env("NB_PROXY_ADDRESS").WithValue(fmt.Sprintf(":%d", httpsPort)),
		)

	certs := corev1ac.PersistentVolumeClaim("certs", "").
		WithSpec(corev1ac.PersistentVolumeClaimSpec().
			WithAccessModes(corev1.ReadWriteOnce).
			WithResources(corev1ac.VolumeResourceRequirements().
				WithRequests(corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("128Mi")})))

	return appsv1ac.StatefulSet(proxyName, c.Namespace).
		WithLabels(labels).
		WithSpec(appsv1ac.StatefulSetSpec().
			WithSelector(metav1ac.LabelSelector().WithMatchLabels(labels)).
			WithVolumeClaimTemplates(certs).
			WithServiceName(proxyName).
			WithReplicas(c.Replicas).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithSpec(corev1ac.PodSpec().WithContainers(container)).
				WithLabels(labels)))
}

func env(name string) *corev1ac.EnvVarApplyConfiguration {
	return corev1ac.EnvVar().WithName(name)
}
