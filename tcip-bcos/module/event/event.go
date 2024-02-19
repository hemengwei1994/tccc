/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/

package event

import (
	chain_client "chainmaker.org/chainmaker/tcip-bcos/v2/module/chain-client"
	"chainmaker.org/chainmaker/tcip-go/v2/common/cross_chain"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"

	//tbis_event "chainmaker.org/chainmaker/tcip-chainmaker/v2/module/event"

	"chainmaker.org/chainmaker/tcip-bcos/v2/module/logger"
	"chainmaker.org/chainmaker/tcip-go/v2/common"
	"chainmaker.org/chainmaker/tcip-go/v2/common/relay_chain"
	"go.uber.org/zap"
)

const (
	eventKey    = "event"
	maxParamLen = 11
)

// EventManager 跨链触发器结构体
type EventManager struct {
	log *zap.SugaredLogger
}

// EventManagerV1 跨链触发器
var EventManagerV1 *EventManager

// InitEventManager 初始化跨链触发器
func InitEventManager() {
	EventManagerV1 = &EventManager{
		log: logger.GetLogger(logger.ModuleDb),
	}
}

// SaveEvent 保存event
//
//	@receiver e
//	@param event
//	@param isNew
//	@return error
func (e *EventManager) SaveEvent(event *common.NewCrossChain) error {
	err := checkEvent(event)
	if err != nil {
		e.log.Errorf("[SaveEvent] %s", err.Error())
		return err
	}
	saveEventString, saveReqString, err := newCrossChain(event)
	if err != nil {
		e.log.Errorf("[SaveEvent] %s", err.Error())
		return err
	}
	argsArr := make([]string, 0)
	argsArr = append(argsArr, saveEventString)
	argsArr = append(argsArr, saveReqString)
	argsArr = append(argsArr, event.CrossId)
	argsStr, _ := json.Marshal(argsArr)
	_, _, err = chain_client.ChainClientV1.InvokeContract(event.SrcChainRid, event.ConfigConstractName,
		cross_chain.CrossContractFuncName_newCrossChain.String(), event.SrcAbi, string(argsStr), false)
	if err != nil {
		e.log.Errorf("[SaveEvent] %s", err.Error())
		return err
	}
	return nil
}

// DeleteEvent 删除event
//
//	@receiver e
//	@param event
//	@return error
func (e *EventManager) DeleteEvent(crossId, chainRid, configConstractName, abi string) error {
	argsArr := make([]string, 0)
	argsArr = append(argsArr, crossId)
	argsStr, _ := json.Marshal(argsArr)
	_, _, err := chain_client.ChainClientV1.InvokeContract(chainRid, configConstractName,
		cross_chain.CrossContractFuncName_deleteCrossChain.String(), abi, string(argsStr), false)
	if err != nil {
		e.log.Errorf("[SaveEvent] %s", err.Error())
		return err
	}
	return nil
}

// GetEvent 获取跨链事件
//
//	@receiver e
//	@param crossChainEventId
//	@return []*common.CrossChainEvent
//	@return error
func (e *EventManager) GetEvent(crossId, chainRid, configConstractName, abi string) (*common.NewCrossChain, error) {
	argsArr := make([]string, 0)
	argsArr = append(argsArr, crossId)
	argsStr, _ := json.Marshal(argsArr)
	res, _, err := chain_client.ChainClientV1.InvokeContract(chainRid, configConstractName,
		cross_chain.CrossContractFuncName_queryCrossChain.String(), abi, string(argsStr), false)
	if err != nil {
		e.log.Errorf("[SaveEvent] %s", err.Error())
		return nil, err
	}
	event := &common.NewCrossChain{}
	err = proto.Unmarshal([]byte(res[0]), event)
	if err != nil {
		e.log.Errorf("[GetEvent] json.Unmarshal error %s", err.Error())
		return nil, err
	}
	return event, nil
}

func newCrossChain(crossConfig *common.NewCrossChain) (string, string, error) {
	crossChainMsg := make([]*common.CrossChainMsg, 1)
	crossChainMsg[0] = &common.CrossChainMsg{
		GatewayId:    crossConfig.DestGatewayId,
		ChainRid:     crossConfig.DestChainRid,
		ContractName: crossConfig.DestContractName,
		Method:       crossConfig.DestTryMethod,
		Abi:          crossConfig.DestAbi,
		ConfirmInfo: &common.ConfirmInfo{
			ChainRid:     crossConfig.DestChainRid,
			ContractName: crossConfig.DestContractName,
			Method:       crossConfig.DestConfirmMethod,
			Abi:          crossConfig.DestAbi,
		},
		CancelInfo: &common.CancelInfo{
			ChainRid:     crossConfig.DestChainRid,
			ContractName: crossConfig.DestContractName,
			Method:       crossConfig.DestCancelMethod,
			Abi:          crossConfig.DestAbi,
		},
	}
	saveReqData := &relay_chain.BeginCrossChainRequest{
		Version:        common.Version_V1_0_0,
		CrossChainName: crossConfig.Desc,
		CrossChainFlag: crossConfig.Desc,
		From:           crossConfig.SrcGatewayId,
		CrossChainMsg:  crossChainMsg, ConfirmInfo: &common.ConfirmInfo{
			ChainRid:     crossConfig.SrcChainRid,
			ContractName: crossConfig.SrcContractName,
			Method:       crossConfig.SrcConfirmMethod,
			Abi:          crossConfig.SrcAbi,
		},
		CancelInfo: &common.CancelInfo{
			ChainRid:     crossConfig.SrcChainRid,
			ContractName: crossConfig.SrcContractName,
			Method:       crossConfig.SrcCancelMethod,
			Abi:          crossConfig.SrcAbi,
		},
		CrossType: crossConfig.TriggerCrossType,
	}
	saveReqByte, err := proto.Marshal(saveReqData)
	if err != nil {
		return "", "", errors.New("fail to marshal saveReqData " + err.Error())
	}
	saveEventByte, err := proto.Marshal(crossConfig)
	if err != nil {
		return "", "", errors.New("fail to marshal crossConfig " + err.Error())
	}
	saveEventString := base64.StdEncoding.EncodeToString(saveEventByte)
	saveReqString := base64.StdEncoding.EncodeToString(saveReqByte)
	return saveEventString, saveReqString, nil
}

// checkEvent 检查事件配置是否合法
//
//	@param event
//	@return error
func checkEvent(event *common.NewCrossChain) error {
	if event.CrossId == "" {
		return fmt.Errorf("CrossId is required")
	}
	if event.DestGatewayId == "" {
		return fmt.Errorf("DestGatewayId is required")
	}
	if event.DestContractName == "" {
		return fmt.Errorf("DestTryMethod is required")
	}
	if event.DestTryMethod == "" {
		return fmt.Errorf("DestTryMethod is required")
	}
	if event.DestGatewayId == common.MainGateway_MAIN_GATEWAY_ID.String() {
		return nil
	}
	if event.DestChainRid == "" {
		return fmt.Errorf("DestChainRid is required")
	}
	return nil
}
