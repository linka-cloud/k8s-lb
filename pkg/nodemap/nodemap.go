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

package nodemap

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
)

type Map interface {
	Load(name string) (corev1.Node, bool)
	Store(name string, node corev1.Node)
	LoadOrStore(name string, node corev1.Node) (actual corev1.Node, loaded bool)
	LoadAndDelete(name string) (node corev1.Node, loaded bool)
	Delete(node corev1.Node)
	Range(func(name string, node corev1.Node) (shouldContinue bool))
	Addresses() []string
}

func New() Map {
	return &nodemap{}
}

type nodemap struct {
	m sync.Map
}

func (n *nodemap) Load(name string) (corev1.Node, bool) {
	v, ok := n.m.Load(name)
	if !ok {
		return corev1.Node{}, false
	}
	return v.(corev1.Node), ok
}

func (n *nodemap) Store(name string, node corev1.Node) {
	n.m.Store(name, node)
}

func (n *nodemap) LoadOrStore(name string, node corev1.Node) (actual corev1.Node, loaded bool) {
	v, ok := n.m.LoadOrStore(name, node)
	if !ok {
		return corev1.Node{}, false
	}
	return v.(corev1.Node), ok
}

func (n *nodemap) LoadAndDelete(name string) (node corev1.Node, loaded bool) {
	v, ok := n.m.LoadAndDelete(name)
	if !ok {
		return corev1.Node{}, false
	}
	return v.(corev1.Node), ok
}

func (n *nodemap) Delete(node corev1.Node) {
	n.m.Delete(node)
}

func (n *nodemap) Range(f func(name string, node corev1.Node) (shouldContinue bool)) {
	n.m.Range(func(key, value interface{}) bool {
		return f(key.(string), value.(corev1.Node))
	})
}

func (n *nodemap) Addresses() []string {
	var addrs []string
	n.Range(func(_ string, node corev1.Node) bool {
		if a := NodeAddress(node); a != "" {
			addrs = append(addrs, a)
		}
		return true
	})
	return addrs
}

func NodeAddress(node corev1.Node) string {
	var address string
	for _, v := range node.Status.Addresses {
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
	return address
}
