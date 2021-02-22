package noop

import (
	"sync"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"go.linka.cloud/k8s/lb/pkg/service"
	"go.linka.cloud/k8s/lb/provider"
)

func New() provider.Provider {
	return &noop{log: zap.New()}
}

type noop struct {
	log logr.Logger
	sync.Map
}

func (n *noop) Set(svc service.Service) (ip string, err error) {
	n.log.Info("should create / update", "service", svc.String())
	if len(svc.NodeIPs) > 0 {
		n.Map.Store(svc.NodeIPs[0], nil)
		return svc.NodeIPs[0], nil
	}
	return "", nil
}

func (n *noop) Delete(svc service.Service) (oldIP string, err error) {
	n.log.Info("should delete", "service", svc.String())
	var ip string
	n.Map.Range(func(key, value interface{}) bool {
		if s, ok := key.(string); ok {
			ip = s
			return false
		}
		return true
	})
	return ip, nil
}
