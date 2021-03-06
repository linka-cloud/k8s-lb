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
	DefaultsToPrivateIP bool
	MultipleClusterLB   bool
	Log                 logr.Logger
}

type Option func(o *options)

func WithLogger(log logr.Logger) Option {
	return func(o *options) {
		o.Log = log
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
			o.PublicIPAnnotation = a
		}
	}
}

func WithDefaultsToPrivateIP(b bool) Option {
	return func(o *options) {
		o.DefaultsToPrivateIP = b
	}
}

func WithMultipleClusterLB(b bool) Option {
	return func(o *options) {
		o.MultipleClusterLB = b
	}
}

var defaultOptions = options{
	PrivateIPAnnotation: PrivateIPAnnotation,
	PublicIPAnnotation:  PublicIPAnnotation,
	Log:                 zap.New(),
}
