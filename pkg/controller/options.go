package controller

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	PrivateIPAnnotation = "lb.k8s.linka.cloud/private"
	PublicIPAnnotation  = "lb.k8s.linka.cloud/public"
)

type options struct {
	PrivateIPAnnotation string
	PublicIPAnnotation  string
	Log                 logr.Logger
}

type Option func(o *options)

func WithLogger(log logr.Logger) Option {
	return func(o *options) {
		if log != nil {
			o.Log = log
		}
	}
}

func WithPrivateIPAnnotation(a string) Option {
	return func(o *options) {
		if a != "" {
			o.PrivateIPAnnotation = a
		}
	}
}

func WithPublicIPAnnotation(a string) Option {
	return func(o *options) {
		if a != "" {
			o.PrivateIPAnnotation = a
		}
	}
}

var defaultOptions = options{
	PrivateIPAnnotation: PrivateIPAnnotation,
	PublicIPAnnotation:  PublicIPAnnotation,
	Log:                 zap.New(),
}
