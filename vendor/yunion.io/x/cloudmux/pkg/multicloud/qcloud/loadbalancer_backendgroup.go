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
	"strconv"
	"strings"
	"time"

	api "yunion.io/x/cloudmux/pkg/apis/compute"
	"yunion.io/x/cloudmux/pkg/cloudprovider"
	"yunion.io/x/cloudmux/pkg/multicloud"
)

type SLBBackendGroup struct {
	multicloud.SLoadbalancerBackendGroupBase
	QcloudTags
	lb       *SLoadbalancer // 必须不能为nil
	listener *SLBListener   // 可能为nil
	domain   string
	path     string
}

func (self *SLBBackendGroup) GetScheduler() string {
	return ""
}

// 返回requestid
func (self *SLBBackendGroup) appLBBackendServer(action string, serverId string, weight int, port int, domain, url string) (string, error) {
	params := map[string]string{
		"LoadBalancerId":       self.lb.LoadBalancerId,
		"ListenerId":           self.listener.ListenerId,
		"Targets.0.InstanceId": serverId,
		"Targets.0.Port":       strconv.Itoa(port),
		"Targets.0.Weight":     strconv.Itoa(weight),
	}
	if len(domain) > 0 {
		params["Domain"] = domain
		params["Url"] = url
	}

	resp, err := self.lb.region.clbRequest(action, params)
	if err != nil {
		return "", err
	}

	return resp.GetString("RequestId")
}

// https://cloud.tencent.com/document/product/214/30677
func (self *SLBBackendGroup) updateBackendServerWeight(action string, serverId string, weight int, port int) (string, error) {
	if len(serverId) == 0 {
		return "", fmt.Errorf("loadbalancer backend instance id should not be empty.")
	}

	params := map[string]string{
		"LoadBalancerId":       self.lb.GetId(),
		"ListenerId":           self.listener.ListenerId,
		"Targets.0.InstanceId": serverId,
		"Targets.0.Port":       strconv.Itoa(port),
		"Targets.0.Weight":     strconv.Itoa(weight),
	}

	resp, err := self.lb.region.clbRequest(action, params)
	if err != nil {
		return "", err
	}

	return resp.GetString("RequestId")
}

// https://cloud.tencent.com/document/product/214/30678
func (self *SLBBackendGroup) updateBackendServerPort(action string, serverId string, oldPort int, newPort int) (string, error) {
	params := map[string]string{
		"LoadBalancerId":       self.lb.GetId(),
		"ListenerId":           self.listener.ListenerId,
		"Targets.0.InstanceId": serverId,
		"Targets.0.Port":       strconv.Itoa(oldPort),
		"NewPort":              strconv.Itoa(newPort),
	}

	resp, err := self.lb.region.clbRequest(action, params)
	if err != nil {
		return "", err
	}

	return resp.GetString("RequestId")
}

// https://cloud.tencent.com/document/product/214/30676
// https://cloud.tencent.com/document/product/214/31789
func (self *SLBBackendGroup) AddBackendServer(opts *cloudprovider.SLoadbalancerBackend) (cloudprovider.ICloudLoadbalancerBackend, error) {
	requestId, err := self.appLBBackendServer("RegisterTargets", opts.ExternalId, opts.Weight, opts.Port, self.domain, self.path)
	if err != nil {
		return nil, err
	}
	err = self.lb.region.WaitLBTaskSuccess(requestId, 5*time.Second, 60*time.Second)
	if err != nil {
		return nil, err
	}
	backends, err := self.GetBackends()
	if err != nil {
		return nil, err
	}
	for _, backend := range backends {
		if strings.HasSuffix(backend.GetId(), fmt.Sprintf("%s-%d", opts.ExternalId, opts.Port)) {
			return &backend, nil
		}
	}
	return nil, cloudprovider.ErrNotFound
}

// https://cloud.tencent.com/document/product/214/30687
// https://cloud.tencent.com/document/product/214/31794
func (self *SLBBackendGroup) RemoveBackendServer(opts *cloudprovider.SLoadbalancerBackend) error {
	requestId, err := self.appLBBackendServer("DeregisterTargets", opts.ExternalId, opts.Weight, opts.Port, self.domain, self.path)
	if err != nil {
		if strings.Contains(err.Error(), "not registered") {
			return nil
		}
		return err
	}
	return self.lb.region.WaitLBTaskSuccess(requestId, 5*time.Second, 60*time.Second)
}

// 腾讯云无后端服务器组。
func (self *SLBBackendGroup) Delete(ctx context.Context) error {
	return nil
}

func backendGroupIdGen(lbid string, secondId string) string {
	if len(secondId) > 0 {
		return fmt.Sprintf("%s", secondId)
	} else {
		return lbid
	}
}

func (self *SLBBackendGroup) GetId() string {
	if len(self.domain) == 0 {
		return self.listener.GetGlobalId()
	}
	return fmt.Sprintf("%s:%s%s", self.listener.GetGlobalId(), self.domain, self.path)
}

func (self *SLBBackendGroup) GetName() string {
	return self.GetId()
}

func (self *SLBBackendGroup) GetGlobalId() string {
	return self.GetId()
}

func (self *SLBBackendGroup) GetStatus() string {
	return api.LB_STATUS_ENABLED
}

func (self *SLBBackendGroup) Refresh() error {
	return nil
}

func (self *SLBBackendGroup) IsDefault() bool {
	return false
}

func (self *SLBBackendGroup) GetType() string {
	return api.LB_BACKENDGROUP_TYPE_NORMAL
}

func (self *SLBBackendGroup) GetILoadbalancerBackends() ([]cloudprovider.ICloudLoadbalancerBackend, error) {
	backends, err := self.GetBackends()
	if err != nil {
		return nil, err
	}

	ret := []cloudprovider.ICloudLoadbalancerBackend{}
	for i := range backends {
		backends[i].group = self
		ret = append(ret, &backends[i])
	}

	return ret, nil
}

func (self *SLBBackendGroup) GetILoadbalancerBackendById(backendId string) (cloudprovider.ICloudLoadbalancerBackend, error) {
	backends, err := self.GetILoadbalancerBackends()
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(backends); i++ {
		if backends[i].GetGlobalId() == backendId {
			return backends[i], nil
		}
	}
	return nil, cloudprovider.ErrNotFound
}

func (self *SLBBackendGroup) GetBackends() ([]SLBBackend, error) {
	backends, err := self.lb.region.GetBackends(self.lb.GetId(), self.listener.ListenerId)
	if err != nil {
		return nil, err
	}
	ret := []SLBBackend{}
	for i := range backends {
		if backends[i].Domain == self.domain && backends[i].Url == self.path {
			backends[i].group = self
			ret = append(ret, backends[i])
		}
	}
	return ret, nil
}
