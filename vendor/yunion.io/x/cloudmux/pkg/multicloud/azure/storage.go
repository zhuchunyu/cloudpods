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

package azure

import (
	"fmt"
	"strings"

	"yunion.io/x/jsonutils"

	api "yunion.io/x/cloudmux/pkg/apis/compute"
	"yunion.io/x/cloudmux/pkg/cloudprovider"
	"yunion.io/x/cloudmux/pkg/multicloud"
)

type Capabilitie struct {
	Name  string
	Value string
}

const (
	STORAGE_STD_LRS = "Standard_LRS"
	STORAGE_PRE_LRS = "Premium_LRS"
	STORAGE_STD_SSD = "StandardSSD_LRS"
)

var STORAGETYPES = []string{STORAGE_STD_LRS, STORAGE_PRE_LRS, STORAGE_STD_SSD}

type SStorage struct {
	multicloud.SStorageBase
	AzureTags
	zone *SZone

	storageType  string
	ResourceType string
	Tier         string
	Kind         string
	Locations    []string
	Capabilities []Capabilitie
}

func (self *SStorage) GetId() string {
	return fmt.Sprintf("%s/%s", self.zone.GetGlobalId(), strings.ToLower(self.storageType))
}

func (self *SStorage) GetName() string {
	return fmt.Sprintf("%s-%s", self.zone.region.client.cpcfg.Name, strings.ToLower(self.storageType))
}

func (self *SStorage) GetGlobalId() string {
	return fmt.Sprintf("%s/%s/%s", self.zone.region.GetGlobalId(), self.zone.region.client.subscriptionId, strings.ToLower(self.storageType))
}

func (self *SStorage) IsEmulated() bool {
	return true
}

func (self *SStorage) GetIZone() cloudprovider.ICloudZone {
	return self.zone
}

func (self *SStorage) GetEnabled() bool {
	return true
}

func (self *SStorage) GetCapacityMB() int64 {
	return 0 // unlimited
}

func (self *SStorage) GetCapacityUsedMB() int64 {
	return 0
}

func (self *SStorage) CreateIDisk(opts *cloudprovider.DiskCreateConfig) (cloudprovider.ICloudDisk, error) {
	disk, err := self.zone.region.CreateDisk(self.storageType, opts)
	if err != nil {
		return nil, err
	}
	disk.storage = self
	return disk, nil
}

func (self *SStorage) GetIDiskById(diskId string) (cloudprovider.ICloudDisk, error) {
	if disk, err := self.zone.region.GetDisk(diskId); err != nil {
		return nil, err
	} else {
		disk.storage = self
		return disk, nil
	}
}

func (self *SStorage) GetIDisks() ([]cloudprovider.ICloudDisk, error) {
	disks, err := self.zone.region.GetDisks()
	if err != nil {
		return nil, err
	}
	idisks := make([]cloudprovider.ICloudDisk, 0)
	for i := 0; i < len(disks); i++ {
		storageType := strings.ToLower(string(disks[i].Sku.Name))
		if storageType == strings.ToLower(self.storageType) {
			disks[i].storage = self
			idisks = append(idisks, &disks[i])
		}
	}
	return idisks, nil
}

func (self *SStorage) GetIStoragecache() cloudprovider.ICloudStoragecache {
	return self.zone.region.getStoragecache()
}

func (self *SStorage) GetMediumType() string {
	if strings.HasPrefix(self.storageType, "premium") {
		return api.DISK_TYPE_SSD
	}
	return api.DISK_TYPE_ROTATE
}

func (self *SStorage) GetStorageConf() jsonutils.JSONObject {
	conf := jsonutils.NewDict()
	return conf
}

func (self *SStorage) GetStatus() string {
	return api.STORAGE_ONLINE
}

func (self *SStorage) GetStorageType() string {
	return strings.ToLower(self.storageType)
}

func (self *SStorage) Refresh() error {
	// do nothing
	return nil
}

func (self *SStorage) GetMountPoint() string {
	return ""
}

func (self *SStorage) IsSysDiskStore() bool {
	return true
}
