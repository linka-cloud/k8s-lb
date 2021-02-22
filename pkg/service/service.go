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

package service

import (
	"fmt"
	"sort"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewMap() Map {
	return &svcmap{}
}

type Map interface {
	Load(service Service) (Service, bool)
	Store(svc Service)
	LoadOrStore(svc Service) (actual Service, loaded bool)
	LoadAndDelete(svc Service) (Service, bool)
	Delete(svc Service)
	Range(func(svc Service) (shouldContinue bool))
}

type svcmap struct {
	m sync.Map
}

func (s *svcmap) Load(service Service) (Service, bool) {
	i, ok := s.m.Load(service.key())
	if !ok {
		return Service{}, false
	}
	return i.(Service), ok
}

func (s *svcmap) Store(svc Service) {
	s.m.Store(svc.key(), svc)
}

func (s *svcmap) LoadOrStore(svc Service) (actual Service, loaded bool) {
	i, ok := s.m.LoadOrStore(svc.key(), svc)
	if !ok {
		return Service{}, false
	}
	return i.(Service), ok
}

func (s *svcmap) LoadAndDelete(svc Service) (Service, bool) {
	i, ok := s.m.LoadAndDelete(svc.key())
	if !ok {
		return Service{}, false
	}
	return i.(Service), ok
}

func (s *svcmap) Delete(svc Service) {
	s.m.Delete(svc.key())
}

func (s *svcmap) Range(f func(svc Service) (shouldContinue bool)) {
	s.m.Range(func(key, value interface{}) bool {
		return f(value.(Service))
	})
}

type Service struct {
	Key         client.ObjectKey
	Name        string
	Port        int32
	Proto       corev1.Protocol
	NodePort    int32
	NodeIPs     []string
	RequestedIP string
	Private     bool
}

func (s *Service) AddNodeIPs(ip ...string) {
	for _, v := range ip {
		s.addNodeIP(v)
	}
}

func (s *Service) addNodeIP(ip string) {
	for _, v := range s.NodeIPs {
		if v == ip {
			return
		}
	}
	s.NodeIPs = append(s.NodeIPs, ip)
	sort.Strings(s.NodeIPs)
}

func (s Service) Equals(o Service) bool {
	return s.String() == o.String()
}

func (s Service) String() string {
	return fmt.Sprintf("%s name: %s, port: %d, proto: %s, nodeport: %d, ips: %v", s.Key.String(), s.Name, s.Port, s.Proto, s.NodePort, s.NodeIPs)
}

func (s Service) key() string {
	return fmt.Sprintf("%s/%d/%s", s.Proto, s.Port, s.Key)
}
