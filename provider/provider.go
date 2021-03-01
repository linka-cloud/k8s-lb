package provider

import (
	"go.linka.cloud/k8s/lb/pkg/service"
)

type Provider interface {
	Set(svc service.Service) (newIP string, oldIP string, err error)
	Delete(svc service.Service) (oldIP string, err error)
}
