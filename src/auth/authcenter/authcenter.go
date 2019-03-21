/*
 * Tencent is pleased to support the open source community by making 蓝鲸 available.
 * Copyright (C) 2017-2019 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package authcenter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"configcenter/src/apimachinery/flowctrl"
	"configcenter/src/apimachinery/rest"
	"configcenter/src/apimachinery/util"
	"configcenter/src/auth/authcenter/permit"
	"configcenter/src/auth/meta"
	"configcenter/src/common/blog"
)

const (
	authAppCodeHeaderKey   string = "X-BK-APP-CODE"
	authAppSecretHeaderKey string = "X-BK-APP-SECRET"
	cmdbUser               string = "user"
	cmdbUserID             string = "system"
)

// ParseConfigFromKV returns a new config
func ParseConfigFromKV(prefix string, configmap map[string]string) (AuthConfig, error) {
	var cfg AuthConfig
	enable, exist := configmap[prefix+".enable"]
	if !exist {
		return AuthConfig{}, nil
	}

	var err error
	cfg.Enable, err = strconv.ParseBool(enable)
	if err != nil {
		return AuthConfig{}, errors.New(`invalid auth "enable" value`)
	}

	if !cfg.Enable {
		return AuthConfig{}, nil
	}

	address, exist := configmap[prefix+".address"]
	if !exist {
		return cfg, errors.New(`missing "address" configuration for auth center`)
	}

	cfg.Address = strings.Split(strings.Replace(address, " ", "", -1), ",")
	if len(cfg.Address) == 0 {
		return cfg, errors.New(`invalid "address" configuration for auth center`)
	}
	for i := range cfg.Address {
		if !strings.HasSuffix(cfg.Address[i], "/") {
			cfg.Address[i] = cfg.Address[i] + "/"
		}
	}

	cfg.AppSecret, exist = configmap[prefix+".appSecret"]
	if !exist {
		return cfg, errors.New(`missing "appSecret" configuration for auth center`)
	}

	if len(cfg.AppSecret) == 0 {
		return cfg, errors.New(`invalid "appSecret" configuration for auth center`)
	}

	cfg.AppCode, exist = configmap[prefix+".appCode"]
	if !exist {
		return cfg, errors.New(`missing "appCode" configuration for auth center`)
	}

	if len(cfg.AppCode) == 0 {
		return cfg, errors.New(`invalid "appCode" configuration for auth center`)
	}

	cfg.SystemID = SystemIDCMDB

	return cfg, nil
}

// NewAuthCenter create a instance to handle resources with blueking's AuthCenter.
func NewAuthCenter(tls *util.TLSClientConfig, cfg AuthConfig) (*AuthCenter, error) {
	blog.V(5).Infof("new auth center client with parameters tls: %+v, cfg: %+v", tls, cfg)
	if !cfg.Enable {
		return new(AuthCenter), nil
	}
	client, err := util.NewClient(tls)
	if err != nil {
		return nil, err
	}

	c := &util.Capability{
		Client: client,
		Discover: &acDiscovery{
			servers: cfg.Address,
		},
		Throttle: flowctrl.NewRateLimiter(1000, 1000),
		Mock: util.MockInfo{
			Mocked: false,
		},
	}

	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	header.Set(authAppCodeHeaderKey, cfg.AppCode)
	header.Set(authAppSecretHeaderKey, cfg.AppSecret)

	return &AuthCenter{
		Config: cfg,
		authClient: &authClient{
			client:      rest.NewRESTClient(c, ""),
			Config:      cfg,
			basicHeader: header,
		},
	}, nil
}

// AuthCenter means BlueKing's authorize center,
// which is also a open source product.
type AuthCenter struct {
	Config AuthConfig
	// http client instance
	client rest.ClientInterface
	// http header info
	header     http.Header
	authClient *authClient
}

func (ac *AuthCenter) Authorize(ctx context.Context, a *meta.AuthAttribute) (decision meta.Decision, err error) {
	blog.V(5).Infof("AuthCenter Config is: %+v", ac.Config)
	if !ac.Config.Enable {
		blog.V(5).Infof("AuthCenter Config is disabled. config: %+v", ac.Config)
		return meta.Decision{Authorized: true}, nil
	}
	resources := make([]meta.ResourceAttribute, 0)
	for _, rsc := range a.Resources {
		// check whether this request is in whitelist, so that it can be skip directly.
		if !permit.IsPermit(&rsc) {
			resources = append(resources, rsc)
		}
	}

	if len(resources) == 0 {
		// no resources need to be authorized.
		return meta.Decision{Authorized: true}, nil
	}

	// there still have resource need to be authorized.
	// so, update the resources.
	a.Resources = resources

	info := &AuthBatch{
		Principal: Principal{
			Type: "user",
			ID:   a.User.UserName,
		},
	}

	// TODO: this operation may be wrong, because some api filters does not
	// fill the business id field, so these api should be normalized.
	if a.BusinessID != 0 {
		info.ScopeType = "biz"
		info.ScopeID = strconv.FormatInt(a.BusinessID, 10)
	} else {
		info.ScopeType = "system"
		info.ScopeID = "bk_cmdb"
	}

	info.ResourceActions = make([]ResourceAction, 0)
	for _, rsc := range a.Resources {

		rscInfo, err := adaptor(&rsc)
		if err != nil {
			return meta.Decision{}, fmt.Errorf("adaptor resource info failed, err: %v", err)
		}

		actionID, err := adaptorAction(&rsc)
		if err != nil {
			return meta.Decision{}, fmt.Errorf("adaptor action failed, err: %v", err)
		}

		scope, err := ac.getScopeInfo(&rsc)
		if err != nil {
			return meta.Decision{}, err
		}

		entity := ResourceEntity{}
		entity.ScopeInfo.ScopeID = scope.ScopeID
		entity.ScopeInfo.ScopeType = scope.ScopeType
		entity.ResourceType = rscInfo.ResourceType
		entity.ResourceID = rscInfo.ResourceID
		entity.ResourceName = rscInfo.ResourceName
		
		info.ResourceActions = append(info.ResourceActions, ResourceAction{
			ActionID:     actionID,
			ResourceInfo: ResourceInfo{
				ResourceEntity: entity,
				ResourceType: rscInfo.ResourceType,
			},
		})
	}

	header := http.Header{}
	header.Set(AuthSupplierAccountHeaderKey, a.User.SupplierAccount)

	batchresult, err := ac.authClient.verifyInList(ctx, header, info)

	noAuth := make([]ResourceTypeID, 0)
	for _, item := range batchresult {
		if !item.IsPass {
			noAuth = append(noAuth, item.ResourceType)
		}
	}

	if len(noAuth) != 0 {
		return meta.Decision{
			Authorized: false,
			Reason:     fmt.Sprintf("resource [%v] do not have permission", noAuth),
		}, nil
	}

	return meta.Decision{Authorized: true}, nil
}

func (ac *AuthCenter) RegisterResource(ctx context.Context, rs ...meta.ResourceAttribute) error {
	if ac.Config.Enable == false {
		blog.V(5).Infof("auth disabled, auth config: %+", ac.Config)
		return nil
	}
	
	registerInfo, err := ac.DryRunRegisterResource(ctx, rs...)
	if err != nil {
		return err
	}
	
	header := http.Header{}
	header.Set(AuthSupplierAccountHeaderKey, rs[0].SupplierAccount)
	
	entities := &registerInfo.Resources
	for _, entity := range *entities {
		registerInfo.Resources = make([]ResourceEntity, 0)
		registerInfo.Resources = append(registerInfo.Resources, entity)
		ac.authClient.registerResource(ctx, header, registerInfo)
	}
	return nil
}

func (ac *AuthCenter) DryRunRegisterResource(ctx context.Context, rs ...meta.ResourceAttribute) (*RegisterInfo, error) {
	if ac.Config.Enable == false {
		blog.V(5).Infof("auth disabled, auth config: %+v", ac.Config)
		return nil, nil
	}

	if len(rs) <= 0 {
		blog.V(5).Info("no resource should be register")
		return nil, nil
	}
	info := RegisterInfo{}
	info.CreatorType = cmdbUser
	info.CreatorID = cmdbUserID
	info.Resources = make([]ResourceEntity, 0)
	for _, r := range rs {
		if len(r.Basic.Type) == 0 {
			return nil, errors.New("invalid resource attribute with empty object")
		}
		scope, err := ac.getScopeInfo(&r)
		if err != nil {
			return nil, err
		}

		rscInfo, err := adaptor(&r)
		if err != nil {
			return nil, fmt.Errorf("adaptor resource info failed, err: %v", err)
		}
		entity := ResourceEntity{}
		entity.ScopeInfo.ScopeID = scope.ScopeID
		entity.ScopeInfo.ScopeType = scope.ScopeType
		entity.ResourceType = rscInfo.ResourceType
		entity.ResourceID = rscInfo.ResourceID
		entity.ResourceName = rscInfo.ResourceName

		// TODO replace register with batch create or update interface, currently is register one by one.
		info.Resources = append(info.Resources, entity)
	}
	return &info, nil
}

func (ac *AuthCenter) DeregisterResource(ctx context.Context, rs ...meta.ResourceAttribute) error {
	if !ac.Config.Enable {
		return nil
	}
	if len(rs) <= 0 {
		// not resource should be deregister
		return nil
	}
	info := DeregisterInfo{}
	header := http.Header{}
	for _, r := range rs {
		if len(r.Basic.Type) == 0 {
			return errors.New("invalid resource attribute with empty object")
		}

		scope, err := ac.getScopeInfo(&r)
		if err != nil {
			return err
		}

		rscInfo, err := adaptor(&r)
		if err != nil {
			return fmt.Errorf("adaptor resource info failed, err: %v", err)
		}

		entity := ResourceEntity{}
		entity.ScopeID = scope.ScopeID
		entity.ScopeType = scope.ScopeType
		entity.ResourceType = rscInfo.ResourceType
		entity.ResourceID = rscInfo.ResourceID
		entity.ResourceName = rscInfo.ResourceName

		info.Resources = append(info.Resources, entity)

		header.Set(AuthSupplierAccountHeaderKey, r.SupplierAccount)
	}

	return ac.authClient.deregisterResource(ctx, header, &info)
}

func (ac *AuthCenter) UpdateResource(ctx context.Context, r *meta.ResourceAttribute) error {
	if !ac.Config.Enable {
		return nil
	}

	if len(r.Basic.Type) == 0 || len(r.Basic.Name) == 0 {
		return errors.New("invalid resource attribute with empty object or object name")
	}

	scope, err := ac.getScopeInfo(r)
	if err != nil {
		return err
	}

	rscInfo, err := adaptor(r)
	if err != nil {
		return fmt.Errorf("adaptor resource info failed, err: %v", err)
	}
	info := &UpdateInfo{
		ScopeInfo:    *scope,
		ResourceInfo: *rscInfo,
	}

	header := http.Header{}
	header.Set(AuthSupplierAccountHeaderKey, r.SupplierAccount)
	return ac.authClient.updateResource(ctx, header, info)
}

func (ac *AuthCenter) Get(ctx context.Context) error {
	panic("implement me")
}

func (ac *AuthCenter) ListResources(ctx context.Context, r *meta.ResourceAttribute) ([]meta.BackendResource, error) {
	if !ac.Config.Enable {
		return nil, nil
	}

	scopeInfo, err := ac.getScopeInfo(r)
	if err != nil {
		return nil, err
	}
	resourceType, err := convertResourceType(r.Type, r.BusinessID)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	resourceID, err := GenerateResourceID(*resourceType, r)
	if err != nil {
		return nil, err
	}
	blog.Infof("GenerateResourceID result: %+v", resourceID)
	searchCondition := SearchCondition{
		ScopeInfo:       *scopeInfo,
		ResourceType:    *resourceType,
		ParentResources: resourceID[:len(resourceID)-1],
	}
	result, err := ac.authClient.ListResources(ctx, header, searchCondition)
	return result, err
}

func (ac *AuthCenter) getScopeInfo(r *meta.ResourceAttribute) (*ScopeInfo, error) {
	s := new(ScopeInfo)
	// TODO: this operation may be wrong, because some api filters does not
	// fill the business id field, so these api should be normalized.
	if r.BusinessID != 0 {
		s.ScopeType = ScopeTypeIDBiz
		s.ScopeID = strconv.FormatInt(r.BusinessID, 10)
	} else {
		s.ScopeType = ScopeTypeIDSystem
		s.ScopeID = SystemIDCMDB
	}
	return s, nil
}

type acDiscovery struct {
	// auth's servers address, must prefixed with http:// or https://
	servers []string
	index   int
	sync.Mutex
}

func (s *acDiscovery) GetServers() ([]string, error) {
	s.Lock()
	defer s.Unlock()

	num := len(s.servers)
	if num == 0 {
		return []string{}, errors.New("oops, there is no server can be used")
	}

	if s.index < num-1 {
		s.index = s.index + 1
		return append(s.servers[s.index-1:], s.servers[:s.index-1]...), nil
	} else {
		s.index = 0
		return append(s.servers[num-1:], s.servers[:num-1]...), nil
	}
}

func (ac *AuthCenter) RawDeregisterResource(ctx context.Context, scope ScopeInfo, rs ...meta.BackendResource) error {
	if !ac.Config.Enable {
		return nil
	}
	if len(rs) <= 0 {
		// not resource should be deregister
		return nil
	}
	info := DeregisterInfo{}
	header := http.Header{}
	for _, r := range rs {
		entity := ResourceEntity{}
		entity.ScopeID = scope.ScopeID
		entity.ScopeType = scope.ScopeType
		entity.ResourceType = ResourceTypeID(r[len(r)-1].ResourceType)
		resourceID := make([]ResourceID, 0)
		for _, item := range r {
			resourceID = append(resourceID, ResourceID{
				ResourceType: ResourceTypeID(item.ResourceType),
				ResourceID: item.ResourceID,
			})
		}
		entity.ResourceID = resourceID

		info.Resources = append(info.Resources, entity)
	}

	return ac.authClient.deregisterResource(ctx, header, &info)
}
