package noop

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"go.linka.cloud/k8s/lb/pkg/service"
	"go.linka.cloud/k8s/lb/provider"
)

const (
	ip = "192.168.42.42"
)

func New() provider.Provider {
	return &noop{log: zap.New()}
}

type noop struct {
	log logr.Logger
	sync.Map
}

func (n *noop) Set(_ context.Context, svc service.Service) (string, string, error) {
	n.log.Info("should create / update", "service", svc.String(), "lbIP", ip)
	var old string
	if v, ok := n.Map.Load(svc.Key.String()); ok {
		old = v.(string)
	}
	n.Map.Store(svc.Key.String(), ip)
	return ip, old, nil
}

func (n *noop) Delete(_ context.Context, svc service.Service) (oldIP string, err error) {
	n.log.Info("should delete", "service", svc.String())
	defer n.Map.Delete(svc.Key.String())
	if ip, ok := n.Map.Load(svc.Key.String()); ok {
		return ip.(string), nil
	}
	return "", nil
}
