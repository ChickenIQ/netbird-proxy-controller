package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/chickeniq/netbird-proxy-controller/internal/controller"
	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	netbirdToken := mustEnv("NETBIRD_TOKEN")
	netbirdURL := mustEnv("NETBIRD_URL")
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		log.Fatalf("read namespace: %v", err)
	}

	replicas, err := strconv.ParseInt(mustEnv("PROXY_REPLICAS"), 10, 32)
	if err != nil {
		log.Fatalf("parse PROXY_REPLICAS: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("load in-cluster config: %v", err)
	}

	kube, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("create Kubernetes client: %v", err)
	}

	cfg := controller.Config{
		Annotations:   mustMap("PROXY_SERVICE_ANNOTATIONS"),
		Labels:        mustMap("PROXY_SERVICE_LABELS"),
		Image:         mustEnv("PROXY_IMAGE"),
		Namespace:     string(namespace),
		Domain:        mustEnv("PROXY_DOMAIN"),
		Replicas:      int32(replicas),
		ManagementURL: netbirdURL,
	}

	ctrl := controller.New(cfg, kube, netbird.New(netbirdURL, netbirdToken))
	log.Printf("starting netbird proxy controller in namespace %s", cfg.Namespace)
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		if err := ctrl.Reconcile(ctx); err != nil {
			log.Printf("reconcile: %v", err)
		}
	}, 10*time.Second)
}

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("missing required environment variable %s", name)
	}

	return value
}

func mustMap(name string) map[string]string {
	var m map[string]string
	if err := json.Unmarshal([]byte(mustEnv(name)), &m); err != nil {
		log.Fatalf("parse %s: %v", name, err)
	}

	return m
}
