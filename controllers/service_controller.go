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

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"go.linka.cloud/k8s/lb/pkg/controller"
)

const (
	ServiceFinalizer = "service.kubernetes.io/load-balancer-cleanup"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Ctrl   controller.Controller
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *ServiceReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("service", req.NamespacedName)

	// your logic here
	s := corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, &s); err != nil {
		if err := client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch service")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if !s.DeletionTimestamp.IsZero() {
		log.Info("removing")
		if err := r.Ctrl.DeleteService(s); err != nil {
			return ctrl.Result{}, err
		}
		if ok := removeFinalizer(&s); !ok {
			return ctrl.Result{}, nil
		}
		if err := r.Update(context.Background(), &s); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if !hasFinalizer(s) && s.Spec.Type == corev1.ServiceTypeLoadBalancer {
		s.Finalizers = append(s.Finalizers, ServiceFinalizer)
		if err := r.Update(context.Background(), &s); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	return r.Ctrl.Reconcile(s)
}

func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Watches(&source.Kind{Type: &corev1.Endpoints{}}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(&filter{mgr.GetClient()}).
		Complete(r)
}

type filter struct {
	c client.Client
}

func (f *filter) filterType(obj runtime.Object) bool {
	switch o := obj.(type) {
	case *corev1.Service:
		return o.Spec.Type == corev1.ServiceTypeLoadBalancer || hasFinalizer(*o)
	}
	k, _ := client.ObjectKeyFromObject(obj)
	s := corev1.Service{}
	if err := f.c.Get(context.Background(), k, &s); err != nil {
		return false
	}
	return f.filterType(&s)
}

func (f *filter) Create(event event.CreateEvent) bool {
	return f.filterType(event.Object)
}

func (f *filter) Delete(event event.DeleteEvent) bool {
	return true
}

func (f *filter) Update(event event.UpdateEvent) bool {
	return f.filterType(event.ObjectOld) || f.filterType(event.ObjectNew)
}

func (f *filter) Generic(_ event.GenericEvent) bool {
	return true
}

func hasFinalizer(s corev1.Service) bool {
	for _, v := range s.Finalizers {
		if v == ServiceFinalizer {
			return true
		}
	}
	return false
}

func removeFinalizer(s *corev1.Service) bool {
	for i, v := range s.ObjectMeta.Finalizers {
		if v != ServiceFinalizer {
			continue
		}
		if len(s.Finalizers) == 1 {
			s.Finalizers = nil
			return true
		}
		s.ObjectMeta.Finalizers = append(s.ObjectMeta.Finalizers[i:], s.ObjectMeta.Finalizers[i+1:]...)
		return true
	}
	return false
}
