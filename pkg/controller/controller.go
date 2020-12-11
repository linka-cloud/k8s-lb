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
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.linka.cloud/lb/pkg/nodemap"
	"go.linka.cloud/lb/pkg/service"
)

type Controller interface {
	Reconcile(svc corev1.Service) (ctrl.Result, error)
	ReSync() (ctrl.Result, error)
	DeleteService(svc corev1.Service) error
	NodeMap() nodemap.Map
	Services() service.Map
}

func New(ctx context.Context, log logr.Logger, client client.Client) Controller {
	return &controller{
		ctx:      ctx,
		log:      log,
		client:   client,
		services: service.NewMap(),
	}
}

type controller struct {
	ctx      context.Context
	log      logr.Logger
	client   client.Client
	nodes    nodemap.Map
	services service.Map
	mu       sync.RWMutex
}

func (c *controller) DeleteService(svc corev1.Service) error {
	k, _ := client.ObjectKeyFromObject(&svc)
	for _, v := range svc.Spec.Ports {
		s := service.Service{
			Key:   k,
			Port:  v.Port,
			Proto: v.Protocol,
		}
		c.log.WithValues("request", k).Info("deleting service", "service", s.String())
		c.services.Delete(s)
	}
	return nil
}

func (c *controller) ReSync() (ctrl.Result, error) {

	svcs := &corev1.ServiceList{}
	if err := c.client.List(c.ctx, svcs, client.InNamespace("test")); err != nil {
		return ctrl.Result{}, err
	}

	// for _, v := range svcs.Items {
	// 	if _, err := c.Reconcile(v); err != nil {
	// 		return ctrl.Result{}, err
	// 	}
	// }
	return ctrl.Result{}, nil
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
	c.mu.RLock()
	nodes := c.nodes
	c.mu.RUnlock()
	if nodes == nil {
		if err := c.SynNodes(); err != nil {
			return ctrl.Result{}, err
		}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, err := client.ObjectKeyFromObject(&svc)
	if err != nil {
		c.log.Error(err, "object key")
		return ctrl.Result{}, err
	}
	log := c.log.WithValues("request", k.String())
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		c.log.Info("skipping non loadbalancer service")
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
		s := service.Service{
			Key:      k,
			Proto:    p.Protocol,
			NodePort: p.NodePort,
			Port:     p.Port,
		}
		log := log.WithValues("endpoint", k)
		for _, e := range es.Subsets {
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
				var address string
				for _, v := range n.Status.Addresses {
					// TODO(adphi): check subnet ??
					switch v.Type {
					case corev1.NodeInternalIP:
						if address == "" {
							address = v.Address
						}
					case corev1.NodeExternalIP:
						address = v.Address
					}
				}
				if address != "" {
					s.AddNodeIP(address)
				}
			}
		}
		if o, ok := c.services.Load(s); ok && o.Equals(s) {
			log.Info("skipping as service did not changed", "service", s.String())
			continue
		}
		log.Info("should update", "service", s.String())
		c.services.Store(s)
	}
	return ctrl.Result{}, nil
}

func (c *controller) NodeMap() nodemap.Map {
	return c.nodes
}

func (c *controller) Services() service.Map {
	return c.services
}
