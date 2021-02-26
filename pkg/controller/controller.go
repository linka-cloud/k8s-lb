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
	Reconcile(svc corev1.Service) (ctrl.Result, error)
	DeleteService(svc corev1.Service) error
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
		ctx:      ctx,
		client:   client,
		rec:      rec,
		prov:     prov,
		services: service.NewMap(),
		options:  &o,
	}, nil
}

type controller struct {
	ctx      context.Context
	client   client.Client
	prov     provider.Provider
	rec      recorder.Recorder
	nodes    nodemap.Map
	services service.Map
	mu       sync.RWMutex
	*options
}

func (c *controller) DeleteService(svc corev1.Service) error {
	k, _ := client.ObjectKeyFromObject(&svc)
	// TODO(adphi): check public and/or private
	s := service.Service{
		Key:         k,
		RequestedIP: svc.Spec.LoadBalancerIP,
	}
	for _, v := range svc.Spec.Ports {
		s.Ports = append(s.Ports, service.Port{
			Name:     v.Name,
			Port:     v.Port,
			Proto:    v.Protocol,
			NodePort: v.NodePort,
		})
	}
	c.Log.WithValues("request", k).V(5).Info("deleting service", "service", s.String())
	c.services.Delete(s)
	ip, err := c.prov.Delete(s)
	if err != nil {
		return err
	}
	if !contains(svc.Status.LoadBalancer.Ingress, ip) {
		c.Log.V(5).Info("skipping status update as IP already not there")
		return nil
	}
	// TODO(adphi): we could loose the loadbalancer IP track if we successfully removed the loadbalancer but failed to update the status
	var ings []corev1.LoadBalancerIngress
	for _, v := range svc.Status.LoadBalancer.Ingress {
		if v.IP != ip {
			ings = append(ings, v)
		}
	}
	svc.Status.LoadBalancer.Ingress = ings
	if err := c.client.Status().Update(c.ctx, &svc); err != nil {
		c.Log.Error(err, "failed to remove loadbalancer ip from status")
		return err
	}
	return nil
}

func (c *controller) SynNodes() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nodes == nil {
		c.nodes = nodemap.New()
	}
	ns := &corev1.NodeList{}
	if err := c.client.List(c.ctx, ns); err != nil {
		return err
	}
	for _, v := range ns.Items {
		c.nodes.Store(v.Name, v)
	}
	return nil
}

func (c *controller) Reconcile(svc corev1.Service) (ctrl.Result, error) {
	if err := c.SynNodes(); err != nil {
		return ctrl.Result{}, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, err := client.ObjectKeyFromObject(&svc)
	if err != nil {
		c.Log.Error(err, "object key")
		return ctrl.Result{}, err
	}
	log := c.Log.WithValues("request", k.String())

	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		c.Log.Info("removing non loadbalancer service")
		if err := c.DeleteService(svc); err != nil {
			return ctrl.Result{}, err
		}
		// TODO(adphi): update status
		return ctrl.Result{}, nil
	}
	if svc.Spec.IPFamily != nil && *svc.Spec.IPFamily == corev1.IPv6Protocol {
		c.Log.Info("ipv6 unsupported")
		return ctrl.Result{}, nil
	}
	es := corev1.Endpoints{}
	if err := c.client.Get(c.ctx, k, &es); err != nil {
		log.Error(err, "retrieve endpoint")
		return ctrl.Result{}, err
	}
	s := service.Service{
		Key:         k,
		RequestedIP: svc.Spec.LoadBalancerIP,
	}
	for _, v := range svc.Spec.Ports {
		// TODO(adphi): check public and/or private
		// TODO(adphi): check if we should delete a private / public loadbalancer
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
	if o, ok := c.services.Load(s); ok && o.Equals(s) {
		log.V(5).Info("skipping as service did not changed", "service", s.String())
		return ctrl.Result{}, nil
	}
	c.services.Store(s)
	ip, err := c.prov.Set(s)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ip == "" {
		c.Log.Error(errors.New("provider returned an empty IP"), "provider failed to provide an IP")
		return ctrl.Result{}, nil
	}
	if contains(svc.Status.LoadBalancer.Ingress, ip) {
		c.Log.V(5).Info("skipping as IP already defined in status", "ip", ip)
		return ctrl.Result{}, nil
	}
	svc.Status.LoadBalancer.Ingress = append(svc.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{IP: ip})
	if err := c.client.Status().Update(c.ctx, &svc); err != nil {
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
