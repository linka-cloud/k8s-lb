/*
Copyright 2020 The Linka Cloud Team.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.linka.cloud/k8s/lb/pkg/nodemap"
	"go.linka.cloud/k8s/lb/pkg/recorder"
	"go.linka.cloud/k8s/lb/pkg/service"
	"go.linka.cloud/k8s/lb/provider"
)

type Controller interface {
	Reconcile(ctx context.Context, svc corev1.Service) (ctrl.Result, error)
	DeleteService(ctx context.Context, svc corev1.Service) (bool, error)
	NodeMap() nodemap.Map
	Services() service.Map
}

func New(ctx context.Context, client client.Client, rec recorder.Recorder, prov provider.Provider, opts ...Option) (Controller, error) {
	if client == nil {
		return nil, errors.New("client is required")
	}
	if rec == nil {
		return nil, errors.New("recorder is required")
	}
	if prov == nil {
		return nil, errors.New("provider is required")
	}
	o := defaultOptions
	for _, v := range opts {
		v(&o)
	}
	return &controller{
		client:   client,
		rec:      rec,
		prov:     prov,
		services: service.NewMap(),
		options:  &o,
	}, nil
}

type controller struct {
	client   client.Client
	prov     provider.Provider
	rec      recorder.Recorder
	nodes    nodemap.Map
	services service.Map
	mu       sync.RWMutex
	*options
}

func (c *controller) DeleteService(ctx context.Context, svc corev1.Service) (bool, error) {
	k := client.ObjectKeyFromObject(&svc)
	// TODO(adphi): check public and/or private
	s := service.Service{
		Key:         k,
		RequestedIP: svc.Spec.LoadBalancerIP,
		Src:         svc,
	}
	for _, v := range svc.Spec.Ports {
		s.Ports = append(s.Ports, service.Port{
			Name:     v.Name,
			Port:     v.Port,
			Proto:    v.Protocol,
			NodePort: v.NodePort,
		})
	}
	c.Log.WithValues("request", k).Info("deleting service", "service", s.String())
	c.services.Delete(s)
	ip, err := c.prov.Delete(ctx, s)
	if err != nil {
		return false, err
	}
	if !contains(svc.Status.LoadBalancer.Ingress, ip) {
		c.Log.Info("skipping status update as IP already not there")
		return true, nil
	}
	c.Log.Info("removing IP from status")
	// TODO(adphi): we could loose the loadbalancer IP track if we successfully removed the loadbalancer but failed to update the status
	var ings []corev1.LoadBalancerIngress
	for _, v := range svc.Status.LoadBalancer.Ingress {
		if v.IP != ip {
			ings = append(ings, v)
		}
	}
	svc.Status.LoadBalancer.Ingress = ings
	if err := c.client.Status().Update(ctx, &svc); err != nil {
		c.Log.Error(err, "failed to remove loadbalancer ip from status")
		return false, err
	}
	return false, nil
}

func (c *controller) SynNodes(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nodes == nil {
		c.nodes = nodemap.New()
	}
	ns := &corev1.NodeList{}
	if err := c.client.List(ctx, ns); err != nil {
		return err
	}
	for _, v := range ns.Items {
		c.nodes.Store(v.Name, v)
	}
	return nil
}

func (c *controller) Reconcile(ctx context.Context, svc corev1.Service) (ctrl.Result, error) {
	if err := c.SynNodes(ctx); err != nil {
		return ctrl.Result{}, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	k := client.ObjectKeyFromObject(&svc)
	log := c.Log.WithValues("request", k.String())

	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		c.Log.Info("removing non loadbalancer service")
		if ok, err := c.DeleteService(ctx, svc); !ok {
			return ctrl.Result{}, err
		}
		var finalizers []string
		for _, v := range svc.Finalizers {
			if v != "service.kubernetes.io/load-balancer-cleanup" {
				finalizers = append(finalizers, v)
			}
		}
		if len(finalizers) == len(svc.Finalizers) {
			return ctrl.Result{}, nil
		}
		svc.Finalizers = finalizers
		c.Log.Info("removing finalizer")
		if err := c.client.Update(ctx, &svc); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	hasV4 := false
	hasV6 := false
	for _, v := range svc.Spec.IPFamilies {
		if v == corev1.IPv4Protocol {
			hasV4 = true
		} else {
			hasV6 = false
			c.rec.Warn(&svc, "Unsupported", "IPv6 not supported")
		}
	}
	if !hasV4 && hasV6 {
		c.Log.Info("ipv6 unsupported")
		return ctrl.Result{}, nil
	}
	es := corev1.Endpoints{}
	if err := c.client.Get(ctx, k, &es); err != nil {
		log.Error(err, "retrieve endpoint")
		return ctrl.Result{}, err
	}
	s := service.Service{
		Key:         k,
		RequestedIP: svc.Spec.LoadBalancerIP,
		Src:         svc,
	}
	// check public or private IP
	_, isPublic := svc.Annotations[c.PublicIPAnnotation]
	_, isPrivate := svc.Annotations[c.PrivateIPAnnotation]
	switch {
	case isPublic && isPrivate:
		s.Private = c.DefaultsToPrivateIP
	case isPublic:
		s.Private = false
	case isPrivate:
		s.Private = true
	default:
		s.Private = c.DefaultsToPrivateIP
	}
	for _, v := range svc.Spec.Ports {
		s.Ports = append(s.Ports, service.Port{
			Name:     v.Name,
			Port:     v.Port,
			Proto:    v.Protocol,
			NodePort: v.NodePort,
		})

	}
	log = log.WithValues("endpoint", k)

	switch svc.Spec.ExternalTrafficPolicy {
	case corev1.ServiceExternalTrafficPolicyTypeLocal:
		s.AddNodeIPs(c.MakeLocalEndpoints(log, es)...)
	// use default as it is what Kubernetes defaults to
	default:
		s.AddNodeIPs(c.MakeClusterEndpoints(log)...)
	}
	c.services.Store(s)
	ip, old, err := c.prov.Set(ctx, s)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ip == "" {
		c.Log.Error(errors.New("provider returned an empty IP"), "provider failed to provide an IP")
		return ctrl.Result{}, nil
	}
	if (ip == old || old == "") && contains(svc.Status.LoadBalancer.Ingress, ip) {
		c.Log.V(5).Info("skipping as IP already defined in status", "ip", ip)
		return ctrl.Result{}, nil
	}
	var ings []corev1.LoadBalancerIngress
	if c.options.MultipleClusterLB {
		for _, v := range svc.Status.LoadBalancer.Ingress {
			if v.IP == old || v.IP == ip {
				continue
			}
			ings = append(ings, v)
		}
	}
	svc.Status.LoadBalancer.Ingress = append(ings, corev1.LoadBalancerIngress{IP: ip})
	if err := c.client.Status().Update(ctx, &svc); err != nil {
		log.Error(err, "failed to update service status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (c *controller) MakeLocalEndpoints(log logr.Logger, endpoints corev1.Endpoints) []string {
	log = log.WithValues("endpoint", fmt.Sprintf("%s/%s", endpoints.Namespace, endpoints.Name))
	var addrs []string
	for _, e := range endpoints.Subsets {
		for _, v := range e.Addresses {
			if v.NodeName == nil {
				log.Info("nodename is nil")
				continue
			}
			n, ok := c.nodes.Load(*v.NodeName)
			if !ok {
				log.Info("not found", "node", *v.NodeName)
				continue
			}
			if a := nodemap.NodeAddress(n); a != "" {
				addrs = append(addrs, a)
			}
		}
	}
	return addrs
}

func (c *controller) MakeClusterEndpoints(_ logr.Logger) []string {
	return c.nodes.Addresses()
}

func (c *controller) NodeMap() nodemap.Map {
	return c.nodes
}

func (c *controller) Services() service.Map {
	return c.services
}

func contains(ings []corev1.LoadBalancerIngress, ip string) bool {
	for _, v := range ings {
		if v.IP == ip {
			return true
		}
	}
	return false
}
