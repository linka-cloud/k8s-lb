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
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Service struct {
	Key         client.ObjectKey
	Ports       []Port
	NodeIPs     []string
	RequestedIP string
	IP          string
	Private     bool
	Src         corev1.Service
}

type Port struct {
	Name     string
	Proto    corev1.Protocol
	NodePort int32
	Port     int32
}

func (p Port) String() string {
	return fmt.Sprintf("name: %s, port: %d, proto: %s, nodeport: %d", p.Name, p.Port, p.Proto, p.NodePort)
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
	sortPorts(s.Ports)
	sortPorts(o.Ports)
	return s.String() == o.String()
}

func (s Service) String() string {
	var ports []string
	for _, v := range s.Ports {
		ports = append(ports, v.String())
	}
	return fmt.Sprintf("%s [private: %v] ports: [%s], ips: %v", s.Key.String(), s.Private, strings.Join(ports, "; "), s.NodeIPs)
}

func (s Service) key() string {
	return s.Key.String()
}

func sortPorts(ports []Port) {
	sort.Slice(ports, func(i, j int) bool {
		return sort.StringsAreSorted([]string{ports[i].Name, ports[j].Name})
	})
}
