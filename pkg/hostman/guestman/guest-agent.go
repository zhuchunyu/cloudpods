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

package guestman

import (
	"context"

	"yunion.io/x/jsonutils"
	"yunion.io/x/pkg/errors"

	"yunion.io/x/onecloud/pkg/hostman/guestman/desc"
	deployapi "yunion.io/x/onecloud/pkg/hostman/hostdeployer/apis"
	"yunion.io/x/onecloud/pkg/hostman/monitor"
	"yunion.io/x/onecloud/pkg/httperrors"
)

func (m *SGuestManager) checkAndInitGuestQga(sid string) (*SKVMGuestInstance, error) {
	guest, _ := m.GetKVMServer(sid)
	if guest == nil {
		return nil, httperrors.NewNotFoundError("Not found guest by id %s", sid)
	}
	if !guest.IsRunning() {
		return nil, httperrors.NewBadRequestError("Guest %s is not in state running", sid)
	}
	if guest.guestAgent == nil {
		if err := guest.InitQga(); err != nil {
			return nil, errors.Wrap(err, "init qga")
		}
	}
	return guest, nil
}

func (m *SGuestManager) QgaGuestSetPassword(ctx context.Context, params interface{}) (jsonutils.JSONObject, error) {
	input := params.(*SQgaGuestSetPassword)
	guest, err := m.checkAndInitGuestQga(input.Sid)
	if err != nil {
		return nil, err
	}

	err = guest.guestAgent.GuestSetUserPassword(input.Username, input.Password, input.Crypted)
	if err != nil {
		return nil, errors.Wrap(err, "qga set user password")
	}
	return nil, nil
}

func (m *SGuestManager) QgaGuestPing(ctx context.Context, params interface{}) (jsonutils.JSONObject, error) {
	input := params.(*SBaseParams)
	guest, err := m.checkAndInitGuestQga(input.Sid)
	if err != nil {
		return nil, err
	}

	timeout := -1
	if to, err := input.Body.Int("timeout"); err == nil {
		timeout = int(to)
	}
	err = guest.guestAgent.GuestPing(timeout)
	if err != nil {
		return nil, errors.Wrap(err, "qga guest ping")
	}
	return nil, nil
}

func (m *SGuestManager) QgaCommand(cmd *monitor.Command, sid string, execTimeout int) (string, error) {
	guest, err := m.checkAndInitGuestQga(sid)
	if err != nil {
		return "", err
	}
	var res []byte
	res, err = guest.guestAgent.QgaCommand(cmd, execTimeout)
	if err != nil {
		err = errors.Wrapf(err, "exec qga command %s", cmd.Execute)
	}
	return string(res), err
}

func (m *SGuestManager) QgaGuestInfoTask(sid string) (string, error) {
	guest, err := m.checkAndInitGuestQga(sid)
	if err != nil {
		return "", err
	}
	var res []byte
	res, err = guest.guestAgent.GuestInfoTask()
	if err != nil {
		return "", errors.Wrap(err, "qga guest info task")
	}
	return string(res), nil
}

func (m *SGuestManager) QgaSetNetwork(ctx context.Context, params interface{}) (jsonutils.JSONObject, error) {
	input := params.(*SQgaGuestSetNetwork)
	netmod := &monitor.NetworkModify{
		Device:  input.Device,
		Ipmask:  input.Ipmask,
		Gateway: input.Gateway,
	}

	guest, err := m.checkAndInitGuestQga(input.Sid)
	if err != nil {
		return nil, err
	}
	err = guest.guestAgent.QgaSetNetwork(netmod, deployapi.GuestNicsToServerNics(guest.Desc.Nics))
	if err != nil {
		return nil, errors.Wrapf(err, "modify %s network failed", netmod.Device)
	}
	return nil, nil
}

func (m *SGuestManager) QgaGetNetwork(sid string) (string, error) {
	guest, err := m.checkAndInitGuestQga(sid)
	if err != nil {
		return "", err
	}
	var res []byte
	res, err = guest.guestAgent.QgaGetNetwork()
	if err != nil {
		return "", errors.Wrap(err, "qga get network fail")
	}
	return string(res), nil
}

func (m *SGuestManager) QgaGetOsInfo(sid string) (jsonutils.JSONObject, error) {
	guest, err := m.checkAndInitGuestQga(sid)
	if err != nil {
		return nil, err
	}

	res, err := guest.guestAgent.QgaGuestGetOsInfo()
	if err != nil {
		return nil, errors.Wrap(err, "qga get os info fail")
	}
	return jsonutils.Marshal(res), nil
}

func (guest *SKVMGuestInstance) QgaAddNicsConfigure(addNics []*desc.SGuestNetwork) error {
	if guest.guestAgent == nil {
		if err := guest.InitQga(); err != nil {
			return errors.Wrap(err, "init qga")
		}
	}
	if err := guest.guestAgent.GuestPing(1); err != nil {
		return errors.Wrap(err, "Qga ping")
	}
	return guest.guestAgent.QgaDeployNics(deployapi.GuestNicsToServerNics(addNics))
}
