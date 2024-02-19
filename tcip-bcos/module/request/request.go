/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/

package request

import (
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/db"
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/utils"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	bcostypes "github.com/FISCO-BCOS/go-sdk/core/types"
	"github.com/gogo/protobuf/proto"
	"time"

	"chainmaker.org/chainmaker/tcip-bcos/v2/module/conf"
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/logger"
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/request/grpcrequest"
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/request/restrequest"
	"chainmaker.org/chainmaker/tcip-go/v2/common"
	"chainmaker.org/chainmaker/tcip-go/v2/common/relay_chain"
	"go.uber.org/zap"
)

// Request 请求接口
type Request interface {
	BeginCrossChain(req *relay_chain.BeginCrossChainRequest) (*relay_chain.BeginCrossChainResponse, error)
	SyncBlockHeader(req *relay_chain.SyncBlockHeaderRequest) (*relay_chain.SyncBlockHeaderResponse, error)
}

// RequestManager 请求管理结构体
type RequestManager struct {
	log     *zap.SugaredLogger
	request Request
}

// RequestV1 rquest模块对象
var RequestV1 *RequestManager

// InitRequestManager 初始化request
//
//	@return error
func InitRequestManager() error {
	log := logger.GetLogger(logger.ModuleRequest)
	var request Request
	if conf.Config.Relay.CallType == conf.GrpcCallType {
		request = grpcrequest.NewGrpcRequest(log)
	} else if conf.Config.Relay.CallType == conf.RestCallType {
		request = restrequest.NewRestRequest(log)
	} else {
		panic("unsupport call_type:" + conf.Config.Relay.CallType)
	}
	RequestV1 = &RequestManager{
		request: request,
		log:     log,
	}
	return nil
}

// BeginCrossChain 如果要保存跨链信息的话，在这个函数里面实现就可以,可以根据结果写入数据库或者文件什么的都可以
//
//	@receiver r
//	@param eventInfo
func (r *RequestManager) BeginCrossChain(eventInfo *utils.EventInfo) {
	beginCrossChainRequest, err := r.buildCrossChainMsg(eventInfo)
	if err != nil {
		r.log.Errorf("[BeginCrossChain] %s", err.Error())
		return
	}
	if beginCrossChainRequest == nil {
		r.log.Warnf("[BeginCrossChain] build beginCrossChainRequest failed: topic %s", eventInfo.Topic)
		return
	}
	r.log.Info("[BeginCrossChain] Call tcip-relayer BeginCrossChain method start: topic %s, request %+v",
		eventInfo.Topic, beginCrossChainRequest)

	resString := []byte("")
	// 5秒重试一次
	for {
		res, err := r.request.BeginCrossChain(beginCrossChainRequest)
		// 这个错就是网络问题，也得重试
		if err != nil {
			r.log.Errorf("[BeginCrossChain] Call tcip-relayer BeginCrossChain method "+
				"error: topic %s, error %s, txId: %s",
				eventInfo.Topic, err.Error(), eventInfo.TxId)
			time.Sleep(time.Second * 5)
			continue
		}
		resString, _ = json.Marshal(res)
		// 这里出错有两种可能，一种是中继链上链失败，中继链挂了或者中继网关和中继链网络断了，那没办法，重试就行了
		// 另一种是参数错误，几乎不可能
		if res.Code != common.Code_GATEWAY_SUCCESS {
			if res.Code == common.Code_GATEWAY_DISABLED {
				panic("this gateway is disabled, please contact admin")
			}
			r.log.Errorf("[BeginCrossChain] Call tcip-relayer BeginCrossChain method "+
				"error: topic %s, response %s, txId: %s",
				eventInfo.Topic, string(resString), eventInfo.TxId)
			time.Sleep(time.Second * 5)
			continue
		}
		_ = r.setLaseCrossHeight(eventInfo.ChainRid, eventInfo.BlockHeight)
		break
	}
	r.log.Infof("[BeginCrossChain] Call tcip-relayer BeginCrossChain method "+
		"success: topic %s, response %s, txId %s",
		eventInfo.Topic, string(resString), eventInfo.TxId)
}

// SyncBlockHeader 同步区块头
//
//	@receiver r
//	@param blockHeader
//	@param chainRid
func (r *RequestManager) SyncBlockHeader(blockHeader *bcostypes.Block,
	blockHeaderBatch []string, chainRid string, successHeight uint64) {
	var (
		blockHeaderByte      []byte
		blockHeaderBatchByte []byte
		err                  error
	)

	r.log.Infof("[SyncBlockHeader] start: chainId: %s, blockHeader: %d", chainRid, successHeight)
	beginTime := time.Now().Unix()
	if blockHeaderBatch != nil {
		blockHeaderBatchByte, err = json.Marshal(blockHeaderBatch)
		if err != nil {
			r.log.Errorf("[SyncBlockHeader]Marshal blockHeaderBatch failed: error: %s, chainId: %s",
				err.Error(), chainRid)
			return
		}
	} else {
		blockHeaderByte, err = json.Marshal(blockHeader)
		if err != nil {
			r.log.Errorf("[SyncBlockHeader]Marshal blockHeader failed: error: %s, chainId: %s",
				err.Error(), chainRid)
			return
		}
	}
	request := &relay_chain.SyncBlockHeaderRequest{
		Version:         common.Version_V1_0_0,
		GatewayId:       conf.Config.BaseConfig.GatewayID,
		ChainRid:        chainRid,
		BlockHeight:     successHeight,
		BlockHeader:     blockHeaderByte,
		BlockHeaderBath: blockHeaderBatchByte,
	}
	for {
		res, err := r.request.SyncBlockHeader(request)
		if err != nil {
			r.log.Errorf("[SyncBlockHeader]Request SyncBlockHeader failed: error: %s, chainId: %s",
				err.Error(), chainRid)
			time.Sleep(time.Second * 5)
			continue
		}
		if res.Code == common.Code_GATEWAY_SUCCESS {
			r.log.Infof("[SyncBlockHeader]SyncBlockHeader success: chainId: %s, blockHeight: %d,"+
				" message: %s, timeUsed: %d",
				chainRid, successHeight, res.Message, time.Now().Unix()-beginTime)
			_ = r.setLaseBlockHeaderHeight(chainRid, int64(successHeight))
			return
		}
		time.Sleep(time.Second * 5)
		r.log.Errorf("[SyncBlockHeader]Request SyncBlockHeader failed: code: %d, error: %s, chainId: %s, blockHeight: %d",
			res.Code, res.Message, chainRid, successHeight)
	}
}

// BuildCrossChainMsg 创建跨链信息
//
//	@receiver e
//	@param eventInfo
//	@return req
//	@return err
func (r *RequestManager) buildCrossChainMsg(eventInfo *utils.EventInfo) (req *relay_chain.BeginCrossChainRequest, err error) {
	return r.buildBeginCrossChainRequestFromEvent(eventInfo)
}

// buildBeginCrossChainRequestFromEvent 构建跨链请求参数
//
//	@receiver e
//	@param event
//	@param eventInfo
//	@return *relay_chain.BeginCrossChainRequest
//	@return error
func (r *RequestManager) buildBeginCrossChainRequestFromEvent(
	eventInfo *utils.EventInfo) (*relay_chain.BeginCrossChainRequest, error) {
	if len(eventInfo.Data) != 2 {
		msg := fmt.Sprintf("[buildBeginCrossChainRequestFromEvent] This topic is not a "+
			"cross chain event: topic %s, date length not 2", eventInfo.Topic)
		r.log.Warnf(msg)
		return nil, errors.New(msg)
	}
	reqData, err := base64.StdEncoding.DecodeString(eventInfo.Data[0])
	if err != nil {
		msg := fmt.Sprintf("[buildBeginCrossChainRequestFromEvent] This topic is not a "+
			"cross chain event: topic %s, reqData need base64: %s", eventInfo.Topic, eventInfo.Data[0])
		r.log.Warnf(msg)
		return nil, errors.New(msg)
	}
	paramData, err := base64.StdEncoding.DecodeString(eventInfo.Data[1])
	if err != nil {
		msg := fmt.Sprintf("[buildBeginCrossChainRequestFromEvent] This topic is not a "+
			"cross chain event: topic %s, paramData need base64: %s", eventInfo.Topic, eventInfo.Data[0])
		r.log.Warnf(msg)
		return nil, errors.New(msg)
	}
	var beginCrossChainRequest relay_chain.BeginCrossChainRequest
	err = proto.Unmarshal(reqData, &beginCrossChainRequest)
	// 反序列化请求失败，表明数据错误
	if err != nil {
		msg := fmt.Sprintf("[buildBeginCrossChainRequestFromEvent] This topic is not a "+
			"cross chain event: topic %s, error %s", eventInfo.Topic, err.Error())
		r.log.Warnf(msg)
		return nil, errors.New(msg)
	}
	if beginCrossChainRequest.From != conf.Config.BaseConfig.GatewayID {
		msg := fmt.Sprintf("[buildBeginCrossChainRequestFromEvent] This topic is not a "+
			"cross chain event: topic %s, from %s", eventInfo.Topic, beginCrossChainRequest.From)
		r.log.Warnf(msg)
		return nil, errors.New(msg)
	}
	var triggerInfo common.TriggerInfo
	err = proto.Unmarshal(paramData, &triggerInfo)
	// 反序列化请求失败，表明数据错误
	if err != nil {
		msg := fmt.Sprintf("[buildBeginCrossChainRequestFromEvent] This topic is not a "+
			"cross chain event: topic %s, error %s", eventInfo.Topic, err.Error())
		r.log.Warnf(msg)
		return nil, errors.New(msg)
	}
	// 如果tx是空的，表明接收到事件的时候，没有正确的获取到tx内容，需要检查一下具体的问题
	if eventInfo.Tx == nil {
		msg := fmt.Sprintf("[buildBeginCrossChainRequestFromEvent] This tx is nil, event %+v", eventInfo)
		r.log.Warnf(msg)
		return nil, errors.New(msg)
	}
	beginCrossChainRequest.TxContent = &common.TxContent{
		TxId:        eventInfo.TxId,
		Tx:          eventInfo.Tx,
		TxResult:    common.TxResultValue_TX_SUCCESS,
		GatewayId:   conf.Config.BaseConfig.GatewayID,
		ChainRid:    eventInfo.ChainRid,
		TxProve:     eventInfo.TxProve,
		BlockHeight: eventInfo.BlockHeight,
	}
	beginCrossChainRequest.Timeout = int64(conf.Config.BaseConfig.DefaultTimeout)
	beginCrossChainRequest.ConfirmInfo.Parameter = triggerInfo.SrcConfirmParam
	beginCrossChainRequest.CancelInfo.Parameter = triggerInfo.SrcCancelParam
	beginCrossChainRequest.CrossChainMsg[0].Parameter = triggerInfo.DestTryParam
	beginCrossChainRequest.CrossChainMsg[0].ConfirmInfo.Parameter = triggerInfo.DestConfirmParam
	beginCrossChainRequest.CrossChainMsg[0].CancelInfo.Parameter = triggerInfo.DestCancelParam
	return &beginCrossChainRequest, nil
}

func (r *RequestManager) setLaseBlockHeaderHeight(chainRid string, height int64) error {
	err := db.Db.Put([]byte(fmt.Sprintf("%s_last_block_header_height", chainRid)),
		[]byte(fmt.Sprintf("%d", height)))
	if err != nil {
		r.log.Errorf("[getLaseBlockHeaderHeight] %s", err.Error())
		return fmt.Errorf("[getLaseBlockHeaderHeight] %s", err.Error())
	}
	return nil
}

func (r *RequestManager) setLaseCrossHeight(chainRid string, height int64) error {
	err := db.Db.Put([]byte(fmt.Sprintf("%s_last_cross_height", chainRid)),
		[]byte(fmt.Sprintf("%d", height)))
	if err != nil {
		r.log.Errorf("[getLaseBlockHeaderHeight] %s", err.Error())
		return fmt.Errorf("[getLaseBlockHeaderHeight] %s", err.Error())
	}
	return nil
}
