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

	"yunion.io/x/jsonutils"

	api "yunion.io/x/onecloud/pkg/apis/compute"
	"yunion.io/x/onecloud/pkg/cloudcommon/db"
	"yunion.io/x/onecloud/pkg/cloudcommon/db/taskman"
	"yunion.io/x/onecloud/pkg/compute/models"
	"yunion.io/x/onecloud/pkg/util/logclient"
)

type GuestEjectISOTask struct {
	SGuestBaseTask
}

func init() {
	taskman.RegisterTask(GuestEjectISOTask{})
}

func (self *GuestEjectISOTask) OnInit(ctx context.Context, obj db.IStandaloneModel, data jsonutils.JSONObject) {
	self.startEjectIso(ctx, obj)
}

func (self *GuestEjectISOTask) startEjectIso(ctx context.Context, obj db.IStandaloneModel) {
	guest := obj.(*models.SGuest)
	cdromOrdinal, _ := self.Params.Int("cdrom_ordinal")
	if guest.EjectIso(cdromOrdinal, self.UserCred) && guest.Status == api.VM_RUNNING {
		self.SetStage("OnConfigSyncComplete", nil)
		drv, err := guest.GetDriver()
		if err != nil {
			self.SetStageFailed(ctx, jsonutils.NewString(err.Error()))
			return
		}
		drv.RequestGuestHotRemoveIso(ctx, guest, self)
	} else {
		self.SetStageComplete(ctx, nil)
	}
}

func (self *GuestEjectISOTask) OnConfigSyncComplete(ctx context.Context, obj db.IStandaloneModel, data jsonutils.JSONObject) {
	logclient.AddActionLogWithContext(ctx, obj, logclient.ACT_ISO_DETACH, nil, self.UserCred, true)
	self.SetStageComplete(ctx, nil)
}

func (self *GuestEjectISOTask) OnConfigSyncCompleteFailed(ctx context.Context, obj db.IStandaloneModel, data jsonutils.JSONObject) {
	logclient.AddActionLogWithContext(ctx, obj, logclient.ACT_ISO_DETACH, nil, self.UserCred, false)
	self.SetStageFailed(ctx, nil)
}
