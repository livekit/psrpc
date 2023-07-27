// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package info

import (
	"sync"

	"github.com/livekit/psrpc"
)

type ServiceDefinition struct {
	Name    string
	ID      string
	Methods sync.Map
}

type MethodInfo struct {
	AffinityEnabled bool
	Multi           bool
	RequireClaim    bool
	Queue           bool
}

type RequestInfo struct {
	psrpc.RPCInfo
	AffinityEnabled bool
	RequireClaim    bool
	Queue           bool
}

func (s *ServiceDefinition) RegisterMethod(name string, affinityEnabled, multi, requireClaim, queue bool) {
	s.Methods.Store(name, &MethodInfo{
		AffinityEnabled: affinityEnabled,
		Multi:           multi,
		RequireClaim:    requireClaim,
		Queue:           queue,
	})
}

func (s *ServiceDefinition) GetInfo(rpc string, topic []string) *RequestInfo {
	v, _ := s.Methods.Load(rpc)
	m := v.(*MethodInfo)

	return &RequestInfo{
		RPCInfo: psrpc.RPCInfo{
			Service: s.Name,
			Method:  rpc,
			Topic:   topic,
			Multi:   m.Multi,
		},
		AffinityEnabled: m.AffinityEnabled,
		RequireClaim:    m.RequireClaim,
		Queue:           m.Queue,
	}
}
