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

package models

import (
	"context"
	"fmt"

	"yunion.io/x/cloudmux/pkg/cloudprovider"
	"yunion.io/x/jsonutils"
	"yunion.io/x/log"
	"yunion.io/x/pkg/errors"
	"yunion.io/x/pkg/util/compare"
	"yunion.io/x/pkg/util/rbacscope"
	"yunion.io/x/pkg/utils"
	"yunion.io/x/sqlchemy"

	api "yunion.io/x/onecloud/pkg/apis/compute"
	"yunion.io/x/onecloud/pkg/cloudcommon/db"
	"yunion.io/x/onecloud/pkg/cloudcommon/db/lockman"
	"yunion.io/x/onecloud/pkg/cloudcommon/db/taskman"
	"yunion.io/x/onecloud/pkg/cloudcommon/validators"
	"yunion.io/x/onecloud/pkg/httperrors"
	"yunion.io/x/onecloud/pkg/mcclient"
	"yunion.io/x/onecloud/pkg/util/stringutils2"
)

// +onecloud:swagger-gen-model-singular=loadbalancerlistenerrule
// +onecloud:swagger-gen-model-plural=loadbalancerlistenerrules
type SLoadbalancerListenerRuleManager struct {
	SLoadbalancerLogSkipper
	db.SStatusStandaloneResourceBaseManager
	db.SExternalizedResourceBaseManager
	SLoadbalancerListenerResourceBaseManager
	SLoadbalancerCertificateResourceBaseManager
}

var LoadbalancerListenerRuleManager *SLoadbalancerListenerRuleManager

func init() {
	LoadbalancerListenerRuleManager = &SLoadbalancerListenerRuleManager{
		SStatusStandaloneResourceBaseManager: db.NewStatusStandaloneResourceBaseManager(
			SLoadbalancerListenerRule{},
			"loadbalancerlistenerrules_tbl",
			"loadbalancerlistenerrule",
			"loadbalancerlistenerrules",
		),
	}
	LoadbalancerListenerRuleManager.SetVirtualObject(LoadbalancerListenerRuleManager)
}

type SLoadbalancerListenerRule struct {
	db.SStatusStandaloneResourceBase
	db.SExternalizedResourceBase

	SLoadbalancerCertificateResourceBase
	SLoadbalancerListenerResourceBase `width:"36" charset:"ascii" nullable:"true" list:"user" create:"optional"`

	// 默认转发策略，目前只有aws用到其它云都是false
	IsDefault bool `default:"false" nullable:"true" list:"user" create:"optional"`

	// 默认后端服务器组
	BackendGroupId string `width:"36" charset:"ascii" nullable:"true" list:"user" create:"optional" update:"user"`

	// 后端服务器组列表
	BackendGroups *api.ListenerRuleBackendGroups `list:"user" update:"user" create:"optional"`

	// 域名
	Domain    string `width:"128" charset:"ascii" nullable:"true" list:"user" create:"optional"`
	Path      string `width:"128" charset:"ascii" nullable:"true" list:"user" create:"optional"`
	Condition string `charset:"ascii" nullable:"true" list:"user" create:"optional"`

	RedirectPool *api.ListenerRuleRedirectPool `list:"user" update:"user" create:"optional"`

	SLoadbalancerHealthCheck // 目前只有腾讯云HTTP、HTTPS类型的健康检查是和规则绑定的。
	SLoadbalancerHTTPRateLimiter
	SLoadbalancerHTTPRedirect
}

func (manager *SLoadbalancerListenerRuleManager) ResourceScope() rbacscope.TRbacScope {
	return rbacscope.ScopeProject
}

func (self *SLoadbalancerListenerRule) GetOwnerId() mcclient.IIdentityProvider {
	lis, err := self.GetLoadbalancerListener()
	if err != nil {
		return nil
	}
	return lis.GetOwnerId()
}

func (manager *SLoadbalancerListenerRuleManager) FetchOwnerId(ctx context.Context, data jsonutils.JSONObject) (mcclient.IIdentityProvider, error) {
	lisId, _ := data.GetString("listener_id")
	if len(lisId) > 0 {
		lis, err := db.FetchById(LoadbalancerListenerManager, lisId)
		if err != nil {
			return nil, errors.Wrapf(err, "db.FetchById(LoadbalancerListenerManager, %s)", lisId)
		}
		return lis.(*SLoadbalancerListener).GetOwnerId(), nil
	}
	return db.FetchProjectInfo(ctx, data)
}

func (man *SLoadbalancerListenerRuleManager) FilterByOwner(ctx context.Context, q *sqlchemy.SQuery, manager db.FilterByOwnerProvider, userCred mcclient.TokenCredential, ownerId mcclient.IIdentityProvider, scope rbacscope.TRbacScope) *sqlchemy.SQuery {
	if ownerId != nil {
		sq := LoadbalancerListenerManager.Query("id")
		lb := LoadbalancerManager.Query().SubQuery()
		sq = sq.Join(lb, sqlchemy.Equals(lb.Field("id"), sq.Field("loadbalancer_id")))
		switch scope {
		case rbacscope.ScopeProject:
			sq = sq.Filter(sqlchemy.Equals(lb.Field("tenant_id"), ownerId.GetProjectId()))
			return q.In("listener_id", sq.SubQuery())
		case rbacscope.ScopeDomain:
			sq = sq.Filter(sqlchemy.Equals(lb.Field("domain_id"), ownerId.GetProjectDomainId()))
			return q.In("listener_id", sq.SubQuery())
		}
	}
	return q
}

func ValidateListenerRuleConditions(condition string) error {
	// total limit 5
	// host-header  limit 1
	// path-pattern limit 1
	// source-ip limit 1
	// http-request-method limit 1
	// http-header  no limit
	// query-string no limit
	limitations := &map[string]int{
		"rules":               5,
		"http-header":         5,
		"query-string":        5,
		"path-pattern":        1,
		"http-request-method": 1,
		"host-header":         1,
		"source-ip":           1,
	}

	obj, err := jsonutils.ParseString(condition)
	if err != nil {
		return httperrors.NewInputParameterError("invalid conditions format,required json")
	}

	conditionArray, ok := obj.(*jsonutils.JSONArray)
	if !ok {
		return httperrors.NewInputParameterError("invalid conditions fromat,required json array")
	}

	if conditionArray.Length() > 5 {
		return httperrors.NewInputParameterError("condition values limit (5 per rule). %d given.", conditionArray.Length())
	}

	cs, _ := conditionArray.GetArray()
	for i := range cs {
		err := validateListenerRuleCondition(cs[i], limitations)
		if err != nil {
			return err
		}
	}

	return nil
}

func validateListenerRuleCondition(condition jsonutils.JSONObject, limitations *map[string]int) error {
	conditionDict, ok := condition.(*jsonutils.JSONDict)
	if !ok {
		return fmt.Errorf("invalid condition fromat,required dict. %#v", condition)
	}

	dict, _ := conditionDict.GetMap()
	field, ok := dict["field"]
	if !ok {
		return fmt.Errorf("parseCondition invalid condition, missing field: %#v", condition)
	}

	f, _ := field.GetString()
	switch f {
	case "http-header":
		return parseHttpHeaderCondition(conditionDict, limitations)
	case "path-pattern":
		return parsePathPatternCondition(conditionDict, limitations)
	case "http-request-method":
		return parseRequestModthdCondition(conditionDict, limitations)
	case "host-header":
		return parseHostHeaderCondition(conditionDict, limitations)
	case "query-string":
		return parseQueryStringCondition(conditionDict, limitations)
	case "source-ip":
		return parseSourceIpCondition(conditionDict, limitations)
	default:
		return fmt.Errorf("parseCondition invalid condition key %#v", field)
	}
}

func parseHttpHeaderCondition(conditon *jsonutils.JSONDict, limitations *map[string]int) error {
	(*limitations)["http-header"] = (*limitations)["http-header"] - 1
	if (*limitations)["http-header"] < 0 {
		return fmt.Errorf("http-header exceeded limiation.")
	}

	values, err := conditon.GetMap("httpHeaderConfig")
	if err != nil {
		return err
	}

	name, ok := values["HttpHeaderName"]
	if !ok {
		return fmt.Errorf("parseHttpHeaderCondition missing filed HttpHeaderName")
	}

	_, ok = name.(*jsonutils.JSONString)
	if !ok {
		return fmt.Errorf("parseHttpHeaderCondition missing invalid data %#v", name)
	}

	vs, ok := values["values"]
	if !ok {
		return fmt.Errorf("parseHttpHeaderCondition missing filed values")
	}

	err = parseConditionStringArrayValues(vs, limitations)
	if err != nil {
		return err
	}

	return nil
}

func parsePathPatternCondition(condition *jsonutils.JSONDict, limitations *map[string]int) error {
	(*limitations)["path-pattern"] = (*limitations)["path-pattern"] - 1
	if (*limitations)["path-pattern"] < 0 {
		return fmt.Errorf("path-pattern exceeded limiation.")
	}

	values, err := condition.GetMap("pathPatternConfig")
	if err != nil {
		return err
	}

	vs, ok := values["values"]
	if !ok {
		return fmt.Errorf("parsePathPatternCondition missing filed values")
	}

	err = parseConditionStringArrayValues(vs, limitations)
	if err != nil {
		return err
	}

	return nil

}

func parseRequestModthdCondition(condition *jsonutils.JSONDict, limitations *map[string]int) error {
	(*limitations)["http-request-method"] = (*limitations)["http-request-method"] - 1
	if (*limitations)["http-request-method"] < 0 {
		return fmt.Errorf("http-request-method exceeded limiation.")
	}

	values, err := condition.GetMap("httpRequestMethodConfig")
	if err != nil {
		return err
	}

	vs, ok := values["values"]
	if !ok {
		return fmt.Errorf("parseRequestModthdCondition missing filed values")
	}

	err = parseConditionStringArrayValues(vs, limitations)
	if err != nil {
		return err
	}

	return nil
}

func parseHostHeaderCondition(condition *jsonutils.JSONDict, limitations *map[string]int) error {
	(*limitations)["host-header"] = (*limitations)["host-header"] - 1
	if (*limitations)["host-header"] < 0 {
		return fmt.Errorf("host-header exceeded limiation.")
	}

	values, err := condition.GetMap("hostHeaderConfig")
	if err != nil {
		return err
	}

	vs, ok := values["values"]
	if !ok {
		return fmt.Errorf("parseHostHeaderCondition missing filed values")
	}

	err = parseConditionStringArrayValues(vs, limitations)
	if err != nil {
		return err
	}

	return nil
}

func parseQueryStringCondition(condition *jsonutils.JSONDict, limitations *map[string]int) error {
	(*limitations)["query-string"] = (*limitations)["query-string"] - 1
	if (*limitations)["query-string"] < 0 {
		return fmt.Errorf("query-string exceeded limiation.")
	}

	values, err := condition.GetMap("queryStringConfig")
	if err != nil {
		return err
	}

	vs, ok := values["values"]
	if !ok {
		return fmt.Errorf("parseQueryStringCondition missing filed values")
	}

	err = parseConditionDictArrayValues(vs, limitations)
	if err != nil {
		return err
	}

	return nil
}

func parseSourceIpCondition(condition *jsonutils.JSONDict, limitations *map[string]int) error {
	(*limitations)["source-ip"] = (*limitations)["source-ip"] - 1
	if (*limitations)["source-ip"] < 0 {
		return fmt.Errorf("source-ip exceeded limiation.")
	}

	values, err := condition.GetMap("sourceIpConfig")
	if err != nil {
		return err
	}

	vs, ok := values["values"]
	if !ok {
		return fmt.Errorf("parseSourceIpCondition missing filed values")
	}

	err = parseConditionStringArrayValues(vs, limitations)
	if err != nil {
		return err
	}

	return nil
}

func parseConditionStringArrayValues(values jsonutils.JSONObject, limitations *map[string]int) error {
	objs, ok := values.(*jsonutils.JSONArray)
	if !ok {
		return fmt.Errorf("parseConditionStringArrayValues invalid values format, required array: %#v", values)
	}

	vs, _ := objs.GetArray()
	for i := range vs {
		(*limitations)["rules"] = (*limitations)["rules"] - 1
		if (*limitations)["rules"] < 0 {
			return fmt.Errorf("rules exceeded limiation.")
		}

		v, ok := vs[i].(*jsonutils.JSONString)
		if !ok {
			return fmt.Errorf("parseConditionStringArrayValues invalid value, required string: %#v", v)
		}
	}

	return nil
}

func parseConditionDictArrayValues(values jsonutils.JSONObject, limitations *map[string]int) error {
	objs, ok := values.(*jsonutils.JSONArray)
	if !ok {
		return fmt.Errorf("parseConditionDictArrayValues invalid values format, required array: %#v", values)
	}

	vs, _ := objs.GetArray()
	for i := range vs {
		(*limitations)["rules"] = (*limitations)["rules"] - 1
		if (*limitations)["rules"] < 0 {
			return fmt.Errorf("rules exceeded limiation.")
		}

		v, ok := vs[i].(*jsonutils.JSONDict)
		if !ok {
			return fmt.Errorf("parseConditionDictArrayValues invalid value, required dict: %#v", v)
		}

		_, err := v.GetString("key")
		if err != nil {
			return err
		}

		_, err = v.GetString("value")
		if err != nil {
			return err
		}
	}

	return nil
}

// 负载均衡监听器规则列表
func (man *SLoadbalancerListenerRuleManager) ListItemFilter(
	ctx context.Context,
	q *sqlchemy.SQuery,
	userCred mcclient.TokenCredential,
	query api.LoadbalancerListenerRuleListInput,
) (*sqlchemy.SQuery, error) {
	q, err := man.SStatusStandaloneResourceBaseManager.ListItemFilter(ctx, q, userCred, query.StatusStandaloneResourceListInput)
	if err != nil {
		return nil, errors.Wrap(err, "SStatusStandaloneResourceBaseManager.ListItemFilter")
	}
	q, err = man.SExternalizedResourceBaseManager.ListItemFilter(ctx, q, userCred, query.ExternalizedResourceBaseListInput)
	if err != nil {
		return nil, errors.Wrap(err, "SExternalizedResourceBaseManager.ListItemFilter")
	}
	q, err = man.SLoadbalancerListenerResourceBaseManager.ListItemFilter(ctx, q, userCred, query.LoadbalancerListenerFilterListInput)
	if err != nil {
		return nil, errors.Wrap(err, "SLoadbalancerListenerResourceBaseManager.ListItemFilter")
	}

	// userProjId := userCred.GetProjectId()
	data := jsonutils.Marshal(query).(*jsonutils.JSONDict)
	q, err = validators.ApplyModelFilters(ctx, q, data, []*validators.ModelFilterOptions{
		// {Key: "listener", ModelKeyword: "loadbalancerlistener", OwnerId: userCred},
		{Key: "backend_group", ModelKeyword: "loadbalancerbackendgroup", OwnerId: userCred},
	})
	if err != nil {
		return nil, err
	}

	if query.IsDefault != nil {
		if *query.IsDefault {
			q = q.IsTrue("is_default")
		} else {
			q = q.IsFalse("is_default")
		}
	}
	if len(query.Domain) > 0 {
		q = q.In("domain", query.Domain)
	}
	if len(query.Path) > 0 {
		q = q.In("path", query.Path)
	}

	return q, nil
}

func (man *SLoadbalancerListenerRuleManager) OrderByExtraFields(
	ctx context.Context,
	q *sqlchemy.SQuery,
	userCred mcclient.TokenCredential,
	query api.LoadbalancerListenerRuleListInput,
) (*sqlchemy.SQuery, error) {
	var err error

	q, err = man.SStatusStandaloneResourceBaseManager.OrderByExtraFields(ctx, q, userCred, query.StatusStandaloneResourceListInput)
	if err != nil {
		return nil, errors.Wrap(err, "SStatusStandaloneResourceBaseManager.OrderByExtraFields")
	}
	q, err = man.SLoadbalancerListenerResourceBaseManager.OrderByExtraFields(ctx, q, userCred, query.LoadbalancerListenerFilterListInput)
	if err != nil {
		return nil, errors.Wrap(err, "SLoadbalancerListenerResourceBaseManager.OrderByExtraFields")
	}

	return q, nil
}

func (man *SLoadbalancerListenerRuleManager) QueryDistinctExtraField(q *sqlchemy.SQuery, field string) (*sqlchemy.SQuery, error) {
	var err error

	q, err = man.SStatusStandaloneResourceBaseManager.QueryDistinctExtraField(q, field)
	if err == nil {
		return q, nil
	}
	q, err = man.SLoadbalancerListenerResourceBaseManager.QueryDistinctExtraField(q, field)
	if err == nil {
		return q, nil
	}

	return q, httperrors.ErrNotFound
}

type sListenerRule struct {
	Name       string
	ListenerId string
}

func (self *SLoadbalancerListenerRule) GetUniqValues() jsonutils.JSONObject {
	return jsonutils.Marshal(sListenerRule{Name: self.Name, ListenerId: self.ListenerId})
}

func (manager *SLoadbalancerListenerRuleManager) FetchUniqValues(ctx context.Context, data jsonutils.JSONObject) jsonutils.JSONObject {
	info := sListenerRule{}
	data.Unmarshal(&info)
	return jsonutils.Marshal(info)
}

func (manager *SLoadbalancerListenerRuleManager) FilterByUniqValues(q *sqlchemy.SQuery, values jsonutils.JSONObject) *sqlchemy.SQuery {
	info := sListenerRule{}
	values.Unmarshal(&info)
	if len(info.ListenerId) > 0 {
		q = q.Equals("listener_id", info.ListenerId)
	}
	if len(info.Name) > 0 {
		q = q.Equals("name", info.Name)
	}
	return q
}

func (man *SLoadbalancerListenerRuleManager) ValidateCreateData(ctx context.Context, userCred mcclient.TokenCredential,
	ownerId mcclient.IIdentityProvider, query jsonutils.JSONObject,
	input *api.LoadbalancerListenerRuleCreateInput,
) (*api.LoadbalancerListenerRuleCreateInput, error) {
	var err error
	input.StatusStandaloneResourceCreateInput, err = man.SStatusStandaloneResourceBaseManager.ValidateCreateData(ctx, userCred, ownerId, query, input.StatusStandaloneResourceCreateInput)
	if err != nil {
		return nil, errors.Wrap(err, "SStatusStandaloneResourceBaseManager.ValidateCreateData")
	}
	if len(input.Status) == 0 {
		input.Status = api.LB_STATUS_ENABLED
	}
	listenerObj, err := validators.ValidateModel(ctx, userCred, LoadbalancerListenerManager, &input.ListenerId)
	if err != nil {
		return nil, errors.Wrap(err, "ValidateModel LoadbalancerListenerManager")
	}
	listener := listenerObj.(*SLoadbalancerListener)
	if listener.ListenerType != api.LB_LISTENER_TYPE_HTTP && listener.ListenerType != api.LB_LISTENER_TYPE_HTTPS {
		return nil, httperrors.NewInputParameterError("listener type must be http/https, got %s", listener.ListenerType)
	}
	region, err := listener.GetRegion()
	if err != nil {
		return nil, errors.Wrap(err, "listener.GetRegion")
	}
	if len(input.CertificateId) > 0 {
		_, err := validators.ValidateModel(ctx, userCred, LoadbalancerCertificateManager, &input.CertificateId)
		if err != nil {
			return nil, err
		}
	}
	if region.GetDriver().IsSupportLoadbalancerListenerRuleRedirect() {
		_, err := validators.ValidateModel(ctx, userCred, LoadbalancerBackendGroupManager, &input.BackendGroupId)
		if err != nil {
			return nil, errors.Wrap(err, "ValidateModel LoadbalancerBackendGroupManager")
		}
	}
	for i := range input.BackendGroups {
		groupObj, err := validators.ValidateModel(ctx, userCred, LoadbalancerBackendGroupManager, &input.BackendGroups[i].Id)
		if err != nil {
			return nil, errors.Wrap(err, "ValidateModel LoadbalancerBackendGroupManager")
		}
		group := groupObj.(*SLoadbalancerBackendGroup)
		input.BackendGroups[i].ExternalId = group.ExternalId
		input.BackendGroups[i].Name = group.Name
	}

	for poolName, pools := range input.RedirectPool.RegionPools {
		for i, group := range pools {
			groupObj, err := validators.ValidateModel(ctx, userCred, LoadbalancerBackendGroupManager, &group.Id)
			if err != nil {
				return nil, errors.Wrap(err, "ValidateModel LoadbalancerBackendGroupManager")
			}
			group := groupObj.(*SLoadbalancerBackendGroup)
			input.RedirectPool.RegionPools[poolName][i].ExternalId = group.ExternalId
			input.RedirectPool.RegionPools[poolName][i].Name = group.Name
		}
	}
	for poolName, pools := range input.RedirectPool.CountryPools {
		for i, group := range pools {
			groupObj, err := validators.ValidateModel(ctx, userCred, LoadbalancerBackendGroupManager, &group.Id)
			if err != nil {
				return nil, errors.Wrap(err, "ValidateModel LoadbalancerBackendGroupManager")
			}
			group := groupObj.(*SLoadbalancerBackendGroup)
			input.RedirectPool.CountryPools[poolName][i].ExternalId = group.ExternalId
			input.RedirectPool.CountryPools[poolName][i].Name = group.Name
		}
	}
	return region.GetDriver().ValidateCreateLoadbalancerListenerRuleData(ctx, userCred, ownerId, input)
}

func (lbr *SLoadbalancerListenerRule) PostCreate(ctx context.Context, userCred mcclient.TokenCredential, ownerId mcclient.IIdentityProvider, query jsonutils.JSONObject, data jsonutils.JSONObject) {
	lbr.SStatusStandaloneResourceBase.PostCreate(ctx, userCred, ownerId, query, data)

	lbr.SetStatus(ctx, userCred, api.LB_CREATING, "")
	if err := lbr.StartLoadBalancerListenerRuleCreateTask(ctx, userCred, ""); err != nil {
		log.Errorf("Failed to create loadbalancer listener rule error: %v", err)
	}
}

func (lbr *SLoadbalancerListenerRule) StartLoadBalancerListenerRuleCreateTask(ctx context.Context, userCred mcclient.TokenCredential, parentTaskId string) error {
	task, err := taskman.TaskManager.NewTask(ctx, "LoadbalancerListenerRuleCreateTask", lbr, userCred, nil, parentTaskId, "", nil)
	if err != nil {
		return err
	}
	return task.ScheduleRun(nil)
}

func (lbr *SLoadbalancerListenerRule) PerformPurge(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject, data jsonutils.JSONObject) (jsonutils.JSONObject, error) {
	parasm := jsonutils.NewDict()
	parasm.Add(jsonutils.JSONTrue, "purge")
	return nil, lbr.StartLoadBalancerListenerRuleDeleteTask(ctx, userCred, parasm, "")
}

func (lbr *SLoadbalancerListenerRule) CustomizeDelete(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject, data jsonutils.JSONObject) error {
	lbr.SetStatus(ctx, userCred, api.LB_STATUS_DELETING, "")
	return lbr.StartLoadBalancerListenerRuleDeleteTask(ctx, userCred, jsonutils.NewDict(), "")
}

func (lbr *SLoadbalancerListenerRule) StartLoadBalancerListenerRuleDeleteTask(ctx context.Context, userCred mcclient.TokenCredential, params *jsonutils.JSONDict, parentTaskId string) error {
	task, err := taskman.TaskManager.NewTask(ctx, "LoadbalancerListenerRuleDeleteTask", lbr, userCred, params, parentTaskId, "", nil)
	if err != nil {
		return err
	}
	task.ScheduleRun(nil)
	return nil
}

func (lbr *SLoadbalancerListenerRule) ValidateUpdateData(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject, input *api.LoadbalancerListenerRuleUpdateInput) (*api.LoadbalancerListenerRuleUpdateInput, error) {
	var err error
	input.StatusStandaloneResourceBaseUpdateInput, err = lbr.SStatusStandaloneResourceBase.ValidateUpdateData(ctx, userCred, query, input.StatusStandaloneResourceBaseUpdateInput)
	if err != nil {
		return nil, errors.Wrap(err, "SStatusStandaloneResourceBase.ValidateUpdateData")
	}
	region, err := lbr.GetRegion()
	if err != nil {
		return nil, err
	}
	return region.GetDriver().ValidateUpdateLoadbalancerListenerRuleData(ctx, userCred, input)
}

func (lbr *SLoadbalancerListenerRule) getMoreDetails(out api.LoadbalancerListenerRuleDetails) (api.LoadbalancerListenerRuleDetails, error) {
	if lbr.BackendGroupId == "" {
		log.Errorf("loadbalancer listener rule %s(%s): empty backend group field", lbr.Name, lbr.Id)
		return out, nil
	}
	lbbg, err := LoadbalancerBackendGroupManager.FetchById(lbr.BackendGroupId)
	if err != nil {
		log.Errorf("loadbalancer listener rule %s(%s): fetch backend group (%s) error: %s",
			lbr.Name, lbr.Id, lbr.BackendGroupId, err)
		return out, err
	}
	out.BackendGroup = lbbg.GetName()

	return out, nil
}

func (man *SLoadbalancerListenerRuleManager) FetchCustomizeColumns(
	ctx context.Context,
	userCred mcclient.TokenCredential,
	query jsonutils.JSONObject,
	objs []interface{},
	fields stringutils2.SSortedStrings,
	isList bool,
) []api.LoadbalancerListenerRuleDetails {
	rows := make([]api.LoadbalancerListenerRuleDetails, len(objs))

	stdRows := man.SStatusStandaloneResourceBaseManager.FetchCustomizeColumns(ctx, userCred, query, objs, fields, isList)
	listenerRows := man.SLoadbalancerListenerResourceBaseManager.FetchCustomizeColumns(ctx, userCred, query, objs, fields, isList)
	certificateRows := man.SLoadbalancerCertificateResourceBaseManager.FetchCustomizeColumns(ctx, userCred, query, objs, fields, isList)

	lbIds := make([]string, len(objs))
	for i := range rows {
		rows[i] = api.LoadbalancerListenerRuleDetails{
			StatusStandaloneResourceDetails:     stdRows[i],
			LoadbalancerListenerResourceInfo:    listenerRows[i],
			LoadbalancerCertificateResourceInfo: certificateRows[i],
		}
		rows[i], _ = objs[i].(*SLoadbalancerListenerRule).getMoreDetails(rows[i])
		lbIds[i] = rows[i].LoadbalancerId
	}

	lbs := map[string]SLoadbalancer{}
	err := db.FetchStandaloneObjectsByIds(LoadbalancerManager, lbIds, &lbs)
	if err != nil {
		return rows
	}

	virObjs := make([]interface{}, len(objs))
	for i := range rows {
		if lb, ok := lbs[lbIds[i]]; ok {
			virObjs[i] = &lb
			rows[i].ProjectId = lb.ProjectId
		}
	}

	return rows
}

func (lbr *SLoadbalancerListenerRule) GetRegion() (*SCloudregion, error) {
	listener, err := lbr.GetLoadbalancerListener()
	if err != nil {
		return nil, err
	}
	return listener.GetRegion()
}

func (lbr *SLoadbalancerListenerRule) GetLoadbalancerBackendGroup() *SLoadbalancerBackendGroup {
	group, err := LoadbalancerBackendGroupManager.FetchById(lbr.BackendGroupId)
	if err != nil {
		return nil
	}
	return group.(*SLoadbalancerBackendGroup)
}

func (lbr *SLoadbalancerListenerRule) Delete(ctx context.Context, userCred mcclient.TokenCredential) error {
	return nil
}

func (self *SLoadbalancerListenerRule) RealDelete(ctx context.Context, userCred mcclient.TokenCredential) error {
	return self.SStatusStandaloneResourceBase.Delete(ctx, userCred)
}

// Delete, Update

func (man *SLoadbalancerListenerRuleManager) getLoadbalancerListenerRulesByListener(listener *SLoadbalancerListener) ([]SLoadbalancerListenerRule, error) {
	rules := []SLoadbalancerListenerRule{}
	q := man.Query().Equals("listener_id", listener.Id)
	if err := db.FetchModelObjects(man, q, &rules); err != nil {
		log.Errorf("failed to get lb listener rules for listener %s error: %v", listener.Name, err)
		return nil, err
	}
	return rules, nil
}

func (man *SLoadbalancerListenerRuleManager) SyncLoadbalancerListenerRules(ctx context.Context, userCred mcclient.TokenCredential, provider *SCloudprovider, listener *SLoadbalancerListener, rules []cloudprovider.ICloudLoadbalancerListenerRule) compare.SyncResult {
	syncOwnerId := provider.GetOwnerId()

	lockman.LockRawObject(ctx, "listener-rules", listener.Id)
	defer lockman.ReleaseRawObject(ctx, "listener-rules", listener.Id)

	syncResult := compare.SyncResult{}

	dbRules, err := man.getLoadbalancerListenerRulesByListener(listener)
	if err != nil {
		syncResult.Error(err)
		return syncResult
	}

	removed := []SLoadbalancerListenerRule{}
	commondb := []SLoadbalancerListenerRule{}
	commonext := []cloudprovider.ICloudLoadbalancerListenerRule{}
	added := []cloudprovider.ICloudLoadbalancerListenerRule{}

	err = compare.CompareSets(dbRules, rules, &removed, &commondb, &commonext, &added)
	if err != nil {
		syncResult.Error(err)
		return syncResult
	}

	for i := 0; i < len(removed); i++ {
		err = removed[i].syncRemoveCloudLoadbalancerListenerRule(ctx, userCred)
		if err != nil {
			syncResult.DeleteError(err)
		} else {
			syncResult.Delete()
		}
	}
	for i := 0; i < len(commondb); i++ {
		err = commondb[i].SyncWithCloudLoadbalancerListenerRule(ctx, userCred, commonext[i], syncOwnerId, provider)
		if err != nil {
			syncResult.UpdateError(err)
		} else {
			syncMetadata(ctx, userCred, &commondb[i], commonext[i], false)
			syncResult.Update()
		}
	}
	for i := 0; i < len(added); i++ {
		local, err := man.newFromCloudLoadbalancerListenerRule(ctx, userCred, listener, added[i], syncOwnerId, provider)
		if err != nil {
			syncResult.AddError(err)
		} else {
			syncMetadata(ctx, userCred, local, added[i], false)
			syncResult.Add()
		}
	}

	return syncResult
}

func (lbr *SLoadbalancerListenerRule) constructFieldsFromCloudListenerRule(userCred mcclient.TokenCredential, extRule cloudprovider.ICloudLoadbalancerListenerRule) {
	// lbr.Name = extRule.GetName()
	lbr.IsDefault = extRule.IsDefault()
	lbr.Domain = extRule.GetDomain()
	lbr.Path = extRule.GetPath()
	lbr.Status = extRule.GetStatus()
	lbr.Condition = extRule.GetCondition()

	if utils.IsInStringArray(extRule.GetRedirect(), []string{api.LB_REDIRECT_OFF, api.LB_REDIRECT_RAW}) {
		lbr.Redirect = extRule.GetRedirect()
		lbr.RedirectCode = int(extRule.GetRedirectCode())
		lbr.RedirectScheme = extRule.GetRedirectScheme()
		lbr.RedirectHost = extRule.GetRedirectHost()
		lbr.RedirectPath = extRule.GetRedirectPath()
	}

	lis, err := lbr.GetLoadbalancerListener()
	if err != nil {
		return
	}

	if groupId := extRule.GetBackendGroupId(); len(groupId) > 0 {
		group, err := db.FetchByExternalIdAndManagerId(LoadbalancerBackendGroupManager, groupId, func(q *sqlchemy.SQuery) *sqlchemy.SQuery {
			return q.Equals("loadbalancer_id", lis.LoadbalancerId)
		})
		if err != nil {
			log.Errorf("Fetch loadbalancer backendgroup by external id %s failed: %s", groupId, err)
		} else {
			lbr.BackendGroupId = group.GetId()
		}
	}

	if groupIds, err := extRule.GetBackendGroups(); err == nil {
		groups := api.ListenerRuleBackendGroups{}
		for _, groupId := range groupIds {
			groupObj, err := db.FetchByExternalIdAndManagerId(LoadbalancerBackendGroupManager, groupId, func(q *sqlchemy.SQuery) *sqlchemy.SQuery {
				return q.Equals("loadbalancer_id", lis.LoadbalancerId)
			})
			if err != nil {
				log.Errorf("Fetch loadbalancer backendgroup by external id %s failed: %s", groupId, err)
				continue
			}
			group := groupObj.(*SLoadbalancerBackendGroup)
			groups = append(groups, api.ListenerRuleBackendGroup{
				Id:         group.Id,
				ExternalId: group.ExternalId,
				Name:       group.Name,
			})
		}
		lbr.BackendGroups = &groups
	}

	if redirectPool, err := extRule.GetRedirectPool(); err == nil {
		lbr.RedirectPool = &api.ListenerRuleRedirectPool{
			RegionPools:  map[string]api.ListenerRuleBackendGroups{},
			CountryPools: map[string]api.ListenerRuleBackendGroups{},
		}
		for poolName, poolIds := range redirectPool.RegionPools {
			groups := api.ListenerRuleBackendGroups{}
			for _, poolId := range poolIds {
				groupObj, err := db.FetchByExternalIdAndManagerId(LoadbalancerBackendGroupManager, poolId, func(q *sqlchemy.SQuery) *sqlchemy.SQuery {
					return q.Equals("loadbalancer_id", lis.LoadbalancerId)
				})
				if err != nil {
					log.Errorf("Fetch loadbalancer backendgroup by external id %s failed: %s", poolId, err)
					continue
				}
				group := groupObj.(*SLoadbalancerBackendGroup)
				groups = append(groups, api.ListenerRuleBackendGroup{
					Id:         group.Id,
					ExternalId: group.ExternalId,
					Name:       group.Name,
				})
			}
			lbr.RedirectPool.RegionPools[poolName] = groups
		}
		for poolName, poolIds := range redirectPool.CountryPools {
			groups := api.ListenerRuleBackendGroups{}
			for _, poolId := range poolIds {
				groupObj, err := db.FetchByExternalIdAndManagerId(LoadbalancerBackendGroupManager, poolId, func(q *sqlchemy.SQuery) *sqlchemy.SQuery {
					return q.Equals("loadbalancer_id", lis.LoadbalancerId)
				})
				if err != nil {
					log.Errorf("Fetch loadbalancer backendgroup by external id %s failed: %s", poolId, err)
					continue
				}
				group := groupObj.(*SLoadbalancerBackendGroup)
				groups = append(groups, api.ListenerRuleBackendGroup{
					Id:         group.Id,
					ExternalId: group.ExternalId,
					Name:       group.Name,
				})
			}
			lbr.RedirectPool.CountryPools[poolName] = groups
		}
	}
}

func (man *SLoadbalancerListenerRuleManager) newFromCloudLoadbalancerListenerRule(
	ctx context.Context,
	userCred mcclient.TokenCredential,
	listener *SLoadbalancerListener,
	extRule cloudprovider.ICloudLoadbalancerListenerRule,
	syncOwnerId mcclient.IIdentityProvider,
	provider *SCloudprovider,
) (*SLoadbalancerListenerRule, error) {
	lbr := &SLoadbalancerListenerRule{}
	lbr.SetModelManager(man, lbr)

	lbr.ExternalId = extRule.GetGlobalId()
	lbr.ListenerId = listener.Id

	lbr.constructFieldsFromCloudListenerRule(userCred, extRule)
	var err = func() error {
		lockman.LockRawObject(ctx, man.Keyword(), "name")
		defer lockman.ReleaseRawObject(ctx, man.Keyword(), "name")

		newName, err := db.GenerateName(ctx, man, syncOwnerId, extRule.GetName())
		if err != nil {
			return err
		}
		lbr.Name = newName

		return man.TableSpec().Insert(ctx, lbr)
	}()
	if err != nil {
		return nil, errors.Wrapf(err, "Insert")
	}

	db.OpsLog.LogEvent(lbr, db.ACT_CREATE, lbr.GetShortDesc(ctx), userCred)

	return lbr, nil
}

func (lbr *SLoadbalancerListenerRule) syncRemoveCloudLoadbalancerListenerRule(ctx context.Context, userCred mcclient.TokenCredential) error {
	lockman.LockObject(ctx, lbr)
	defer lockman.ReleaseObject(ctx, lbr)

	err := lbr.ValidateDeleteCondition(ctx, nil)
	if err != nil { // cannot delete
		lbr.SetStatus(ctx, userCred, api.LB_STATUS_UNKNOWN, "sync to delete")
		return errors.Wrapf(err, "ValidateDeleteCondition")
	}
	return lbr.RealDelete(ctx, userCred)
}

func (lbr *SLoadbalancerListenerRule) SyncWithCloudLoadbalancerListenerRule(
	ctx context.Context,
	userCred mcclient.TokenCredential,
	extRule cloudprovider.ICloudLoadbalancerListenerRule,
	syncOwnerId mcclient.IIdentityProvider,
	provider *SCloudprovider,
) error {
	diff, err := db.UpdateWithLock(ctx, lbr, func() error {
		lbr.constructFieldsFromCloudListenerRule(userCred, extRule)
		return nil
	})
	if err != nil {
		return err
	}

	db.OpsLog.LogSyncUpdate(lbr, diff, userCred)

	return nil
}

func (manager *SLoadbalancerListenerRuleManager) ListItemExportKeys(ctx context.Context,
	q *sqlchemy.SQuery,
	userCred mcclient.TokenCredential,
	keys stringutils2.SSortedStrings,
) (*sqlchemy.SQuery, error) {
	var err error
	q, err = manager.SStatusStandaloneResourceBaseManager.ListItemExportKeys(ctx, q, userCred, keys)
	if err != nil {
		return nil, errors.Wrap(err, "SStatusStandaloneResourceBaseManager.ListItemExportKeys")
	}
	if keys.ContainsAny(manager.SLoadbalancerListenerResourceBaseManager.GetExportKeys()...) {
		q, err = manager.SLoadbalancerListenerResourceBaseManager.ListItemExportKeys(ctx, q, userCred, keys)
		if err != nil {
			return nil, errors.Wrap(err, "SLoadbalancerListenerResourceBaseManager.ListItemExportKeys")
		}
	}
	return q, nil
}
