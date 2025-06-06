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

package guest

import (
	"context"
	"fmt"

	"yunion.io/x/jsonutils"
	"yunion.io/x/log"

	"yunion.io/x/onecloud/pkg/apis/compute"
	api "yunion.io/x/onecloud/pkg/apis/compute"
	"yunion.io/x/onecloud/pkg/cloudcommon/db"
	"yunion.io/x/onecloud/pkg/cloudcommon/db/taskman"
	"yunion.io/x/onecloud/pkg/compute/models"
	"yunion.io/x/onecloud/pkg/util/logclient"
)

func init() {
	taskman.RegisterTask(GuestDiskSnapshotTask{})
}

type GuestDiskSnapshotTask struct {
	SGuestBaseTask
}

func (self *GuestDiskSnapshotTask) OnInit(ctx context.Context, obj db.IStandaloneModel, data jsonutils.JSONObject) {
	guest := obj.(*models.SGuest)
	self.DoDiskSnapshot(ctx, guest)
}

func (self *GuestDiskSnapshotTask) DoDiskSnapshot(ctx context.Context, guest *models.SGuest) {
	diskId, err := self.Params.GetString("disk_id")
	if err != nil {
		self.TaskFailed(ctx, guest, jsonutils.NewString(err.Error()))
		return
	}
	snapshotId, err := self.Params.GetString("snapshot_id")
	if err != nil {
		self.TaskFailed(ctx, guest, jsonutils.NewString(err.Error()))
		return
	}
	params := jsonutils.NewDict()
	params.Set("guest_old_status", jsonutils.NewString(guest.Status))
	self.SetStage("OnDiskSnapshotComplete", params)
	guest.SetStatus(ctx, self.UserCred, api.VM_SNAPSHOT, "")
	drv, err := guest.GetDriver()
	if err != nil {
		self.TaskFailed(ctx, guest, jsonutils.NewString(err.Error()))
		return
	}
	err = drv.RequestDiskSnapshot(ctx, guest, self, snapshotId, diskId)
	if err != nil {
		self.TaskFailed(ctx, guest, jsonutils.NewString(err.Error()))
		return
	}
}

func (self *GuestDiskSnapshotTask) OnDiskSnapshotComplete(ctx context.Context, guest *models.SGuest, data jsonutils.JSONObject) {
	res := data.(*jsonutils.JSONDict)
	snapshotId, _ := self.Params.GetString("snapshot_id")
	iSnapshot, _ := models.SnapshotManager.FetchById(snapshotId)
	snapshot := iSnapshot.(*models.SSnapshot)
	location, err := res.GetString("location")
	if err != nil {
		log.Infof("OnDiskSnapshotComplete called with data no location")
		return
	}
	var osType string
	if snapshot.DiskType == compute.DISK_TYPE_SYS {
		osType = guest.GetOS()
	}
	_, err = db.Update(snapshot, func() error {
		snapshot.Location = location
		snapshot.Status = api.SNAPSHOT_READY
		snapshot.OsType = osType
		return nil
	})
	if err != nil {
		self.TaskFailed(ctx, guest, jsonutils.NewString(fmt.Sprintf("update sanpshot failed: %s", err)))
		return
	}

	self.TaskComplete(ctx, guest, nil)
}

func (self *GuestDiskSnapshotTask) OnDiskSnapshotCompleteFailed(ctx context.Context, guest *models.SGuest, err jsonutils.JSONObject) {
	self.TaskFailed(ctx, guest, err)
}

func (self *GuestDiskSnapshotTask) TaskComplete(ctx context.Context, guest *models.SGuest, data jsonutils.JSONObject) {
	logclient.AddActionLogWithStartable(self, guest, logclient.ACT_DISK_CREATE_SNAPSHOT, nil, self.UserCred, true)
	status, _ := self.Params.GetString("guest_old_status")
	guest.SetStatus(ctx, self.UserCred, status, "on guest disk snapshot complete")
	self.SetStageComplete(ctx, nil)
}

func (self *GuestDiskSnapshotTask) TaskFailed(ctx context.Context, guest *models.SGuest, reason jsonutils.JSONObject) {
	guest.SetStatus(ctx, self.UserCred, api.VM_SNAPSHOT_FAILED, reason.String())
	logclient.AddActionLogWithStartable(self, guest, logclient.ACT_DISK_CREATE_SNAPSHOT, reason, self.UserCred, false)
	self.SetStageFailed(ctx, reason)
}
