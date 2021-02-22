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
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.linka.cloud/k8s/lb/pkg/nodemap"
	"go.linka.cloud/k8s/lb/pkg/recorder"
	"go.linka.cloud/k8s/lb/pkg/service"
)

const (
	PrivateIPAnnotation = "lb.k8s.linka.cloud/private"
	PublicIPAnnotation  = "lb.k8s.linka.cloud/public"
)

type Controller interface {
	Reconcile(svc corev1.Service) (ctrl.Result, error)
	ReSync() (ctrl.Result, error)
	DeleteService(svc corev1.Service) error
	NodeMap() nodemap.Map
	Services() service.Map
}

func New(ctx context.Context, log logr.Logger, client client.Client, rec recorder.Recorder) Controller {
	return &controller{
		ctx:      ctx,
		log:      log,
		client:   client,
		rec:      rec,
		services: service.NewMap(),
	}
}

type controller struct {
	ctx      context.Context
	log      logr.Logger
	client   client.Client
	rec      recorder.Recorder
	nodes    nodemap.Map
	services service.Map
	mu       sync.RWMutex
}

func (c *controller) DeleteService(svc corev1.Service) error {
	k, _ := client.ObjectKeyFromObject(&svc)
	// TODO(adphi): check public and/or private
	for _, v := range svc.Spec.Ports {
		s := service.Service{
			Key:         k,
			Name:        k.Name,
			Port:        v.Port,
			Proto:       v.Protocol,
			RequestedIP: svc.Spec.LoadBalancerIP,
		}
		c.log.WithValues("request", k).Info("deleting service", "service", s.String())
		c.services.Delete(s)
	}
	return nil
}

func (c *controller) ReSync() (ctrl.Result, error) {
	svcs := &corev1.ServiceList{}
	if err := c.client.List(c.ctx, svcs); err != nil {
		return ctrl.Result{}, err
	}
	requeue := false
	for _, v := range svcs.Items {
		if v.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}
		r, err := c.Reconcile(v)
		if err != nil {
			return ctrl.Result{}, err
		}
		if r.Requeue {
			requeue = true
		}
	}
	return ctrl.Result{Requeue: requeue}, nil
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
		c.log.Error(err, "object key")
		return ctrl.Result{}, err
	}
	log := c.log.WithValues("request", k.String())
	// TODO(adphi): Is it a bug ? Only load balancer services are scheduled to reconcile
	//  what if the service type changed ?
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		c.log.Info("removing non loadbalancer service")
		if err := c.DeleteService(svc); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if svc.Spec.IPFamily != nil && *svc.Spec.IPFamily == corev1.IPv6Protocol {
		c.log.Info("ipv6 unsupported")
		return ctrl.Result{}, nil
	}
	es := corev1.Endpoints{}
	if err := c.client.Get(c.ctx, k, &es); err != nil {
		log.Error(err, "retrieve endpoint")
		return ctrl.Result{}, err
	}
	for _, p := range svc.Spec.Ports {
		// TODO(adphi): check public and/or private
		s := service.Service{
			Key:         k,
			Name:        p.Name,
			Proto:       p.Protocol,
			NodePort:    p.NodePort,
			Port:        p.Port,
			RequestedIP: svc.Spec.LoadBalancerIP,
		}

		log := log.WithValues("endpoint", k)

		switch svc.Spec.ExternalTrafficPolicy {
		case corev1.ServiceExternalTrafficPolicyTypeLocal:
			s.AddNodeIPs(c.MakeLocalEndpoints(log, es)...)
		// use default as it is what Kubernetes defaults to
		default:
			s.AddNodeIPs(c.MakeClusterEndpoints(log)...)
		}
		if o, ok := c.services.Load(s); ok && o.Equals(s) {
			log.Info("skipping as service did not changed", "service", s.String())
			continue
		}
		log.Info("should update", "service", s.String())
		c.services.Store(s)

		// TODO(adphi): replace with real implementation
		var ings []corev1.LoadBalancerIngress
		for _, v := range s.NodeIPs {
			ings = append(ings, corev1.LoadBalancerIngress{IP: v})
		}
		svc.Status.LoadBalancer.Ingress = ings
		if err := c.client.Status().Update(context.Background(), &svc); err != nil {
			log.Error(err, "failed to update service status")
			return ctrl.Result{}, err
		}
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
