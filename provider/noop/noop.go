package noop

import (
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

func (n *noop) Set(svc service.Service) (string, error) {
	n.log.Info("should create / update", "service", svc.String(), "lbIP", ip)
	n.Map.Store(svc.Name, ip)
	return ip, nil
}

func (n *noop) Delete(svc service.Service) (oldIP string, err error) {
	n.log.Info("should delete", "service", svc.String())
	defer n.Map.Delete(svc.Name)
	if ip, ok := n.Map.Load(svc.Name); ok {
		return ip.(string), nil
	}
	return "", nil
}
