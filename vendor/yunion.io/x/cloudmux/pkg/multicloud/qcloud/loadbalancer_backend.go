// Copyright 2019 Yunion
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

package qcloud

import (
	"context"
	"fmt"

	"yunion.io/x/jsonutils"

	api "yunion.io/x/cloudmux/pkg/apis/compute"
	"yunion.io/x/cloudmux/pkg/cloudprovider"
	"yunion.io/x/cloudmux/pkg/multicloud"
)

type SLBBackend struct {
	multicloud.SResourceBase
	multicloud.STagBase
	group *SLBBackendGroup

	PublicIPAddresses  []string `json:"PublicIpAddresses"`
	Weight             int      `json:"Weight"`
	InstanceId         string   `json:"InstanceId"`
	InstanceName       string   `json:"InstanceName"`
	PrivateIPAddresses []string `json:"PrivateIpAddresses"`
	RegisteredTime     string   `json:"RegisteredTime"`
	Type               string   `json:"Type"`
	Port               int      `json:"Port"`
	Domain             string
	Url                string
}

// ==========================================================
type SListenerBackend struct {
	Rules      []Rule       `json:"Rules"`
	Targets    []SLBBackend `json:"Targets"`
	Protocol   string       `json:"Protocol"`
	ListenerId string       `json:"ListenerId"`
	Port       int64        `json:"Port"`
}

type Rule struct {
	URL        string       `json:"Url"`
	Domain     string       `json:"Domain"`
	LocationId string       `json:"LocationId"`
	Targets    []SLBBackend `json:"Targets"`
}

// ==========================================================

// backend InstanceId + protocol  +Port + ip + rip全局唯一
func (self *SLBBackend) GetId() string {
	if len(self.Domain) == 0 {
		return fmt.Sprintf("%s/%s-%d", self.group.GetId(), self.InstanceId, self.Port)
	}
	return fmt.Sprintf("%s/%s-%d:%s%s", self.group.GetId(), self.InstanceId, self.Port, self.Domain, self.Url)
}

func (self *SLBBackend) GetName() string {
	return self.GetId()
}

func (self *SLBBackend) GetGlobalId() string {
	return self.GetId()
}

func (self *SLBBackend) GetStatus() string {
	return api.LB_STATUS_ENABLED
}

func (self *SLBBackend) Refresh() error {
	backends, err := self.group.GetBackends()
	if err != nil {
		return err
	}

	for _, backend := range backends {
		if backend.GetId() == self.GetId() {
			return jsonutils.Update(self, backend)
		}
	}

	return cloudprovider.ErrNotFound
}

func (self *SLBBackend) GetWeight() int {
	return self.Weight
}

func (self *SLBBackend) GetPort() int {
	return self.Port
}

func (self *SLBBackend) GetBackendType() string {
	return api.LB_BACKEND_GUEST
}

func (self *SLBBackend) GetBackendRole() string {
	return api.LB_BACKEND_ROLE_DEFAULT
}

func (self *SLBBackend) GetBackendId() string {
	return self.InstanceId
}

func (self *SLBBackend) GetIpAddress() string {
	for _, ip := range self.PrivateIPAddresses {
		if len(ip) > 0 {
			return ip
		}
	}
	return ""
}

// 应用型： https://cloud.tencent.com/document/product/214/30684
func (self *SRegion) GetBackends(lbId, listenerId string) ([]SLBBackend, error) {
	params := map[string]string{"LoadBalancerId": lbId}

	if len(listenerId) > 0 {
		params["ListenerIds.0"] = listenerId
	}

	resp, err := self.clbRequest("DescribeTargets", params)
	if err != nil {
		return nil, err
	}

	lbackends := []SListenerBackend{}
	err = resp.Unmarshal(&lbackends, "Listeners")
	if err != nil {
		return nil, err
	}
	backends := []SLBBackend{}
	for k := range lbackends {
		entry := lbackends[k]
		backends = append(backends, entry.Targets...)
		for i := range entry.Rules {
			for j := range entry.Rules[i].Targets {
				entry.Rules[i].Targets[j].Domain = entry.Rules[i].Domain
				entry.Rules[i].Targets[j].Url = entry.Rules[i].URL
				backends = append(backends, entry.Rules[i].Targets[j])
			}
		}
	}
	return backends, nil
}

func (self *SLBBackend) Update(ctx context.Context, opts *cloudprovider.SLoadbalancerBackend) error {
	self.Port = opts.Port
	self.Weight = opts.Weight
	return nil
}
