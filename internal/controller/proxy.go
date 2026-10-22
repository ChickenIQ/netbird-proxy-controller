package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"

	"github.com/netbirdio/netbird/shared/management/http/api"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
)

const (
	tokenSecret     = "netbird-proxy-token"
	tokenAnnotation = "netbird.io/token"
	proxyName       = "netbird-proxy"
	certsDir        = "/certs"
	tokenKey        = "token"
)

func (c *Controller) reconcileProxy(ctx context.Context) error {
	secret, err := c.ensureToken(ctx)
	if err != nil {
		return err
	}

	if _, err := c.kube.AppsV1().StatefulSets(c.Namespace).Apply(ctx, c.statefulSet(secret), applyOptions); err != nil {
		return fmt.Errorf("apply statefulset %s: %w", proxyName, err)
	}

	return nil
}

func (c *Controller) ensureToken(ctx context.Context) (*corev1.Secret, error) {
	secrets := c.kube.CoreV1().Secrets(c.Namespace)

	existing, err := secrets.Get(ctx, tokenSecret, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		return existing, err
	}

	tokens, err := c.netbird.ReverseProxyTokens.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, t := range tokens {
		if t.Name != c.Domain || t.Revoked {
			continue
		}

		log.Printf("Reconcile: revoking proxy token %s", t.Id)
		if err := c.netbird.ReverseProxyTokens.Delete(ctx, t.Id); err != nil {
			return nil, fmt.Errorf("revoke proxy token %s: %w", t.Id, err)
		}
	}

	token, err := c.netbird.ReverseProxyTokens.Create(ctx, api.ProxyTokenRequest{Name: c.Domain, ExpiresIn: new(0)})
	if err != nil {
		return nil, fmt.Errorf("create proxy token: %w", err)
	}

	secret := &corev1.Secret{
		StringData: map[string]string{tokenKey: token.PlainToken},
		ObjectMeta: metav1.ObjectMeta{Name: tokenSecret},
	}

	log.Printf("Reconcile: creating secret %s", tokenSecret)
	return secrets.Create(ctx, secret, metav1.CreateOptions{})
}

func (c *Controller) statefulSet(secret *corev1.Secret) *appsv1ac.StatefulSetApplyConfiguration {
	labels := map[string]string{"app.kubernetes.io/name": proxyName}

	podIndex := corev1ac.EnvVarSource().
		WithFieldRef(corev1ac.ObjectFieldSelector().WithFieldPath("metadata.labels['apps.kubernetes.io/pod-index']"))

	token := corev1ac.EnvVarSource().
		WithSecretKeyRef(corev1ac.SecretKeySelector().WithName(tokenSecret).WithKey(tokenKey))

	container := corev1ac.Container().
		WithVolumeMounts(corev1ac.VolumeMount().WithName("certs").WithMountPath(certsDir)).
		WithSecurityContext(corev1ac.SecurityContext().
			WithCapabilities(corev1ac.Capabilities().WithDrop("ALL")).
			WithAllowPrivilegeEscalation(false)).
		WithImage(c.Image).
		WithName("proxy").
		WithEnv(
			env("POD_INDEX").WithValueFrom(podIndex),
			env("NB_PROXY_TOKEN").WithValueFrom(token),
			env("NB_PROXY_WG_PORT").WithValue("65535"),
			env("NB_PROXY_CERT_LOCK_METHOD").WithValue("flock"),
			env("NB_PROXY_ACME_CERTIFICATES").WithValue("true"),
			env("NB_PROXY_CERTIFICATE_DIRECTORY").WithValue(certsDir),
			env("NB_PROXY_HEALTH_ADDRESS").WithValue("localhost:65534"),
			env("NB_PROXY_MANAGEMENT_ADDRESS").WithValue(c.ManagementURL),
			env("NB_PROXY_ADDRESS").WithValue(fmt.Sprintf(":%d", httpsPort)),
			env("NB_PROXY_DOMAIN").WithValue(c.clusterDomain("$(POD_INDEX)")),
		)

	security := corev1ac.PodSecurityContext().
		WithSeccompProfile(corev1ac.SeccompProfile().WithType(corev1.SeccompProfileTypeRuntimeDefault)).
		WithRunAsNonRoot(true).
		WithRunAsUser(1000).
		WithFSGroup(1000)

	certs := corev1ac.PersistentVolumeClaim("certs", "").
		WithSpec(corev1ac.PersistentVolumeClaimSpec().
			WithAccessModes(corev1.ReadWriteOnce).
			WithResources(corev1ac.VolumeResourceRequirements().
				WithRequests(corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("128Mi")})))

	return appsv1ac.StatefulSet(proxyName, c.Namespace).
		WithLabels(labels).
		WithSpec(appsv1ac.StatefulSetSpec().
			WithSelector(metav1ac.LabelSelector().WithMatchLabels(labels)).
			WithPodManagementPolicy(appsv1.ParallelPodManagement).
			WithVolumeClaimTemplates(certs).
			WithServiceName(proxyName).
			WithReplicas(c.Replicas).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithSpec(corev1ac.PodSpec().WithSecurityContext(security).WithContainers(container)).
				WithAnnotations(map[string]string{tokenAnnotation: fmt.Sprintf("%x", sha256.Sum256(secret.Data[tokenKey]))}).
				WithLabels(labels)))
}

func env(name string) *corev1ac.EnvVarApplyConfiguration {
	return corev1ac.EnvVar().WithName(name)
}
