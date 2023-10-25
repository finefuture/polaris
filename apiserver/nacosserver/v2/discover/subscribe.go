/**
 * Tencent is pleased to support the open source community by making Polaris available.
 *
 * Copyright (C) 2019 THL A29 Limited, a Tencent company. All rights reserved.
 *
 * Licensed under the BSD 3-Clause License (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * https://opensource.org/licenses/BSD-3-Clause
 *
 * Unless required by applicable law or agreed to in writing, software distributed
 * under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
 * CONDITIONS OF ANY KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations under the License.
 */

package discover

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"github.com/polarismesh/polaris/apiserver/nacosserver/core"
	nacosmodel "github.com/polarismesh/polaris/apiserver/nacosserver/model"
	nacospb "github.com/polarismesh/polaris/apiserver/nacosserver/v2/pb"
	"github.com/polarismesh/polaris/apiserver/nacosserver/v2/remote"
	"github.com/polarismesh/polaris/common/utils"
)

func (h *DiscoverServer) handleSubscribeServiceReques(ctx context.Context, req nacospb.BaseRequest,
	meta nacospb.RequestMeta) (nacospb.BaseResponse, error) {
	subReq, ok := req.(*nacospb.SubscribeServiceRequest)
	if !ok {
		return nil, remote.ErrorInvalidRequestBodyType
	}
	namespace := nacosmodel.ToPolarisNamespace(subReq.Namespace)
	service := subReq.ServiceName
	group := subReq.GroupName
	nacoslog.Info("[NACOS-V2][Instance] subscribe service request", zap.String("namespace", namespace),
		zap.String("service", service), zap.String("group", group))

	subscriber := core.Subscriber{
		Key:         remote.ValueConnID(ctx),
		ConnID:      remote.ValueConnID(ctx),
		AddrStr:     meta.ClientIP,
		Agent:       meta.ClientVersion,
		App:         nacosmodel.DefaultString(req.GetHeaders()["app"], "unknown"),
		Ip:          meta.ClientIP,
		NamespaceId: namespace,
		Group:       group,
		Service:     service,
		Cluster:     subReq.Clusters,
		Type:        core.GRPCPush,
	}
	if subReq.Subscribe {
		h.pushCenter.AddSubscriber(subscriber)
	} else {
		h.pushCenter.RemoveSubscriber(subscriber)
	}

	filterCtx := &core.FilterContext{
		Service:     core.ToNacosService(h.discoverSvr.Cache(), namespace, service, group),
		Clusters:    strings.Split(subReq.Clusters, ","),
		EnableOnly:  true,
		HealthyOnly: true,
	}
	// 默认只下发 enable 的实例
	result := h.store.ListInstances(filterCtx, core.SelectInstancesWithHealthyProtection)

	return &nacospb.SubscribeServiceResponse{
		Response: &nacospb.Response{
			ResultCode: int(nacosmodel.Response_Success.Code),
			Message:    "success",
		},
		ServiceInfo: *result,
	}, nil
}

func (h *DiscoverServer) sendPushData(sub core.Subscriber, data *core.PushData) error {
	client, ok := h.connMgr.GetClient(sub.ConnID)
	if !ok {
		nacoslog.Error("[NACOS-V2][PushCenter] notify subscriber client not found", zap.String("conn-id", sub.ConnID))
		return nil
	}
	stream, ok := client.LoadStream()
	if !ok {
		nacoslog.Error("[NACOS-V2][PushCenter] notify subscriber not register gRPC stream",
			zap.String("conn-id", sub.ConnID))
		return nil
	}
	namespace := nacosmodel.ToNacosNamespace(data.ServiceInfo.Namespace)
	watcher := sub
	svr := stream
	req := &nacospb.NotifySubscriberRequest{
		NamingRequest: nacospb.NewBasicNamingRequest(utils.NewUUID(), namespace, data.ServiceInfo.Name,
			data.ServiceInfo.GroupName),
		ServiceInfo: data.ServiceInfo,
	}
	nacoslog.Info("[NACOS-V2][PushCenter] notify subscriber new service info", zap.String("conn-id", watcher.ConnID),
		zap.String("req-id", req.RequestId),
		zap.String("namespace", data.Service.Namespace), zap.String("svc", data.Service.Name))

	connCtx := context.WithValue(context.TODO(), remote.ConnIDKey{}, watcher.ConnID)
	callback := func(resp nacospb.BaseResponse, err error) {
		if err != nil {
			nacoslog.Error("[NACOS-V2][PushCenter] receive client push error",
				zap.String("req-id", req.RequestId),
				zap.String("namespace", data.Service.Namespace), zap.String("svc", data.Service.Name),
				zap.Error(err))
		} else {
			h.connMgr.RefreshClient(connCtx)
			nacoslog.Info("[NACOS-V2][PushCenter] receive client push ack", zap.String("req-id", req.RequestId),
				zap.String("namespace", data.Service.Namespace), zap.String("svc", data.Service.Name),
				zap.Any("resp", resp))
		}
	}

	// add inflight first
	err := h.connMgr.InFlights().AddInFlight(&remote.InFlight{
		ConnID:    watcher.ConnID,
		RequestID: req.RequestId,
		Callback:  callback,
	})
	if err != nil {
		nacoslog.Error("[NACOS-V2][PushCenter] add inflight client error", zap.String("conn-id", watcher.ConnID),
			zap.String("req-id", req.RequestId),
			zap.String("namespace", data.Service.Namespace), zap.String("svc", data.Service.Name),
			zap.Error(err))
	}

	clientResp, err := remote.MarshalPayload(req)
	if err != nil {
		return err
	}
	return svr.SendMsg(clientResp)
}