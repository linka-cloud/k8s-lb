package provider

import (
	"context"

	"go.linka.cloud/k8s/lb/pkg/service"
)

type Provider interface {
	Set(ctx context.Context, svc service.Service) (newIP string, oldIP string, err error)
	Delete(ctx context.Context, svc service.Service) (oldIP string, err error)
}
