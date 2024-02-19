/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"chainmaker.org/chainmaker/tcip-bcos/v2/module/utils"

	//tbis_event "chainmaker.org/chainmaker/tcip-chainmaker/v2/module/event"

	"chainmaker.org/chainmaker/tcip-bcos/v2/module/conf"

	chain_client "chainmaker.org/chainmaker/tcip-bcos/v2/module/chain-client"

	"google.golang.org/grpc/peer"

	"chainmaker.org/chainmaker/tcip-go/v2/common"

	"chainmaker.org/chainmaker/tcip-bcos/v2/module/event"
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/logger"
	"go.uber.org/zap"

	"chainmaker.org/chainmaker/tcip-go/v2/common/cross_chain"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Handler handler结构体
type Handler struct {
	log *zap.SugaredLogger
}

const nilParam = "{}"

// NewHandler 初始化handler模块
//
//	@Description:
func NewHandler() *Handler {
	return &Handler{
		log: logger.GetLogger(logger.ModuleHandler),
	}
}

// CrossChainTry 接收跨链请求的接口
//
//	@receiver h
//	@param ctx
//	@param req
//	@return *cross_chain.CrossChainTryResponse
//	@return error
func (h *Handler) CrossChainTry(ctx context.Context,
	req *cross_chain.CrossChainTryRequest) (*cross_chain.CrossChainTryResponse, error) {
	h.printRequest(ctx, "CrossChainTry", fmt.Sprintf("%+v", req))

	switch req.Version {
	case common.Version_V1_0_0:
		tryResult, tx, err := chain_client.ChainClientV1.InvokeContract(req.CrossChainMsg.ChainRid,
			req.CrossChainMsg.ContractName, req.CrossChainMsg.Method,
			req.CrossChainMsg.Abi, req.CrossChainMsg.Parameter, true)
		if err != nil {
			h.log.Errorf("[CrossChainTry] Failed to execute cross-chain transaction: cross chain id: %s",
				req.CrossChainId)
			return getCrossChainTryReturn(common.Code_INTERNAL_ERROR,
				req.CrossChainId, req.CrossChainName, req.CrossChainFlag,
				err.Error(), nil, nil)
		}
		txByte, _ := json.Marshal(tx)
		blockNumber := strings.Replace(tx.BlockNumber, "0x", "", -1)
		h, _ := strconv.ParseUint(blockNumber, 16, 64)
		return getCrossChainTryReturn(common.Code_GATEWAY_SUCCESS,
			req.CrossChainId, req.CrossChainName,
			req.CrossChainFlag, common.Code_GATEWAY_SUCCESS.String(), &common.TxContent{
				TxId:        tx.Hash,
				Tx:          txByte,
				TxResult:    common.TxResultValue_TX_SUCCESS,
				GatewayId:   conf.Config.BaseConfig.GatewayID,
				ChainRid:    req.CrossChainMsg.ChainRid,
				TxProve:     chain_client.ChainClientV1.GetTxProve(tx, req.CrossChainMsg.ChainRid),
				BlockHeight: int64(h),
			}, tryResult)
	default:
		return getCrossChainTryReturn(common.Code_INVALID_PARAMETER,
			req.CrossChainId, req.CrossChainName,
			req.CrossChainFlag, utils.UnsupportVersion(req.Version), nil, nil)
	}
}

// CrossChainConfirm 跨链结果确认
//
//	@receiver h
//	@param ctx
//	@param req
//	@return *cross_chain.CrossChainConfirmResponse
//	@return error
func (h *Handler) CrossChainConfirm(ctx context.Context,
	req *cross_chain.CrossChainConfirmRequest) (*cross_chain.CrossChainConfirmResponse, error) {
	h.printRequest(ctx, "CrossChainConfirm", fmt.Sprintf("%+v", req))
	// 根据业务做一些处理，这里模拟调用一个confirm方法，这样就把业务的逻辑放在了合约中，减少跨链网关的定制化开发
	switch req.Version {
	case common.Version_V1_0_0:
		if req.ConfirmInfo == nil || req.ConfirmInfo.ChainRid == "" {
			return &cross_chain.CrossChainConfirmResponse{
				Code:    common.Code_GATEWAY_SUCCESS,
				Message: common.Code_GATEWAY_SUCCESS.String(),
			}, nil
		}
		var (
			param string
			err   error
			err1  error
		)
		if req.CrossChainFlag == "tbis_event" {
			//param, err1 = fillTbisResult(req.ConfirmInfo.Parameter, req.ConfirmInfo.ChainRid,
			//	tbis_event.SubSuccess, tbis_event.SubSuccess, req.TryResult[0])
			//if err1 != nil {
			//	h.log.Errorf("[CrossChainConfirm] %s", err1.Error())
			//	return &cross_chain.CrossChainConfirmResponse{
			//		Code:    common.Code_INTERNAL_ERROR,
			//		Message: err1.Error(),
			//	}, nil
			//}
			h.log.Errorf("not support")
		} else {
			param, err1 = fillTryResult(req.ConfirmInfo.Parameter, req.TryResult, req.CrossType)
			if err1 != nil {
				h.log.Errorf("[CrossChainConfirm] %s", err1.Error())
				return &cross_chain.CrossChainConfirmResponse{
					Code:    common.Code_INTERNAL_ERROR,
					Message: err.Error(),
				}, nil
			}
		}
		_, tx, err := chain_client.ChainClientV1.InvokeContract(req.ConfirmInfo.ChainRid,
			req.ConfirmInfo.ContractName, req.ConfirmInfo.Method, req.ConfirmInfo.Abi,
			param, true)
		if err != nil {
			h.log.Errorf("[CrossChainTry] Failed to execute cross-chain transaction: cross chain id: %s",
				req.CrossChainId)
			return &cross_chain.CrossChainConfirmResponse{
				Code:    common.Code_INTERNAL_ERROR,
				Message: err.Error(),
			}, nil
		}
		txByte, _ := json.Marshal(tx)
		blockHeight, _ := strconv.Atoi(tx.BlockNumber)
		return &cross_chain.CrossChainConfirmResponse{
			Code:    common.Code_GATEWAY_SUCCESS,
			Message: common.Code_GATEWAY_SUCCESS.String(),
			TxContent: &common.TxContent{
				TxId:      tx.Hash,
				Tx:        txByte,
				TxResult:  common.TxResultValue_TX_SUCCESS,
				GatewayId: conf.Config.BaseConfig.GatewayID,
				ChainRid:  req.ConfirmInfo.ChainRid,
				// 这里不验证不需要填
				TxProve:     "",
				BlockHeight: int64(blockHeight),
			},
		}, nil
	default:
		return &cross_chain.CrossChainConfirmResponse{
			Code:    common.Code_INVALID_PARAMETER,
			Message: utils.UnsupportVersion(req.Version),
		}, nil
	}
}

// CrossChainCancel 跨链结果确认
//
//	@receiver h
//	@param ctx
//	@param req
//	@return *cross_chain.CrossChainCancelResponse
//	@return error
func (h *Handler) CrossChainCancel(
	ctx context.Context, req *cross_chain.CrossChainCancelRequest) (*cross_chain.CrossChainCancelResponse, error) {
	h.printRequest(ctx, "CrossChainCancel", fmt.Sprintf("%+v", req))
	// 根据业务做一些处理,这里模拟调用一个合约中的cancel方法，这样就把业务的逻辑放在了合约中，减少跨链网关的定制化开发
	switch req.Version {
	case common.Version_V1_0_0:
		if req.CancelInfo == nil || req.CancelInfo.ChainRid == "" {
			return &cross_chain.CrossChainCancelResponse{
				Code:    common.Code_GATEWAY_SUCCESS,
				Message: common.Code_GATEWAY_SUCCESS.String(),
			}, nil
		}
		param := nilParam
		if req.CancelInfo.Parameter != "" {
			param = req.CancelInfo.Parameter
		}
		//var err1 error
		if req.CrossChainFlag == "tbis_event.TbisFlag" {
			//param, err1 = fillTbisResult(req.CancelInfo.Parameter, req.CancelInfo.ChainRid,
			//	tbis_event.SubFailed, tbis_event.SubFailed, "failed")
			//if err1 != nil {
			//	h.log.Errorf("[CrossChainConfirm] %s", err1.Error())
			//	return &cross_chain.CrossChainCancelResponse{
			//		Code:    common.Code_INTERNAL_ERROR,
			//		Message: err1.Error(),
			//	}, nil
			//}
			h.log.Errorf("not support")
		}
		_, tx, err := chain_client.ChainClientV1.InvokeContract(req.CancelInfo.ChainRid,
			req.CancelInfo.ContractName, req.CancelInfo.Method,
			req.CancelInfo.Abi, param, true)
		if err != nil {
			h.log.Errorf("[CrossChainTry] Failed to execute cross-chain transaction: cross chain id: %s",
				req.CrossChainId)
			return &cross_chain.CrossChainCancelResponse{
				Code:    common.Code_INTERNAL_ERROR,
				Message: err.Error(),
			}, nil
		}
		txByte, _ := json.Marshal(tx)
		blockHeight, _ := strconv.Atoi(tx.BlockNumber)
		return &cross_chain.CrossChainCancelResponse{
			Code:    common.Code_GATEWAY_SUCCESS,
			Message: common.Code_GATEWAY_SUCCESS.String(),
			TxContent: &common.TxContent{
				TxId:      tx.Hash,
				Tx:        txByte,
				TxResult:  common.TxResultValue_TX_SUCCESS,
				GatewayId: conf.Config.BaseConfig.GatewayID,
				ChainRid:  req.CancelInfo.ChainRid,
				// 这里不验证不需要填
				TxProve:     "",
				BlockHeight: int64(blockHeight),
			},
		}, nil
	default:
		return &cross_chain.CrossChainCancelResponse{
			Code:    common.Code_INVALID_PARAMETER,
			Message: utils.UnsupportVersion(req.Version),
		}, nil
	}
}

// CrossChainEvent 跨链触发事件管理
//
//	@receiver h
//	@param ctx
//	@param req
//	@return *cross_chain.CrossChainEventResponse
//	@return error
func (h *Handler) CrossChainEvent(ctx context.Context,
	req *cross_chain.CrossChainEventRequest) (*cross_chain.CrossChainEventResponse, error) {
	h.printRequest(ctx, "CrossChainEvent", fmt.Sprintf("%+v", req))

	switch req.Version {
	case common.Version_V1_0_0:
		switch req.Operate {
		case common.Operate_GET:
			crossChainEvent, err := event.EventManagerV1.GetEvent(req.CrossChainEvent.CrossId,
				req.CrossChainEvent.SrcChainRid, req.CrossChainEvent.ConfigConstractName,
				req.CrossChainEvent.SrcAbi)
			if err != nil {
				return &cross_chain.CrossChainEventResponse{
					Code:    common.Code_INTERNAL_ERROR,
					Message: err.Error(),
				}, nil
			}
			return &cross_chain.CrossChainEventResponse{
				Code:            common.Code_GATEWAY_SUCCESS,
				Message:         common.Code_GATEWAY_SUCCESS.String(),
				CrossChainEvent: crossChainEvent,
			}, nil
		case common.Operate_DELETE:
			err := event.EventManagerV1.DeleteEvent(req.CrossChainEvent.CrossId, req.CrossChainEvent.SrcChainRid,
				req.CrossChainEvent.ConfigConstractName, req.CrossChainEvent.SrcAbi)
			if err != nil {
				return &cross_chain.CrossChainEventResponse{
					Code:    common.Code_INTERNAL_ERROR,
					Message: err.Error(),
				}, nil
			}
			return &cross_chain.CrossChainEventResponse{
				Code:    common.Code_GATEWAY_SUCCESS,
				Message: common.Code_GATEWAY_SUCCESS.String(),
			}, nil
		case common.Operate_SAVE:
			err := event.EventManagerV1.SaveEvent(req.CrossChainEvent)
			if err != nil {
				return &cross_chain.CrossChainEventResponse{
					Code:    common.Code_INTERNAL_ERROR,
					Message: err.Error(),
				}, nil
			}
			return &cross_chain.CrossChainEventResponse{
				Code:    common.Code_GATEWAY_SUCCESS,
				Message: common.Code_GATEWAY_SUCCESS.String(),
			}, nil
		//case common.Operate_UPDATE:
		//err := event.EventManagerV1.SaveEvent(req.CrossChainEvent)
		//if err != nil {
		//	return &cross_chain.CrossChainEventResponse{
		//		Code:    common.Code_INTERNAL_ERROR,
		//		Message: err.Error(),
		//	}, nil
		//}
		//return &cross_chain.CrossChainEventResponse{
		//	Code:    common.Code_GATEWAY_SUCCESS,
		//	Message: common.Code_GATEWAY_SUCCESS.String(),
		//}, nil
		default:
			return &cross_chain.CrossChainEventResponse{
				Code:    common.Code_INVALID_PARAMETER,
				Message: "unsupported operate",
			}, nil
		}
	default:
		return &cross_chain.CrossChainEventResponse{
			Code:    common.Code_INVALID_PARAMETER,
			Message: utils.UnsupportVersion(req.Version),
		}, nil
	}
}

// TxVerify rpc交易验证，不是非要在当前服务中实现,本项目不支持rpc验证，不需要实现
//
//	@receiver h
//	@param ctx
//	@param req
//	@return *cross_chain.TxVerifyResponse
//	@return error
func (h *Handler) TxVerify(ctx context.Context,
	req *cross_chain.TxVerifyRequest) (*cross_chain.TxVerifyResponse, error) {
	h.printRequest(ctx, "TxVerify", fmt.Sprintf("%+v", req))
	switch req.Version {
	case common.Version_V1_0_0:
		res := chain_client.ChainClientV1.TxProve(req.TxProve)
		return &cross_chain.TxVerifyResponse{
			TxVerifyResult: res,
			Code:           common.Code_GATEWAY_SUCCESS,
			Message:        common.Code_GATEWAY_SUCCESS.String(),
		}, nil
	default:
		return &cross_chain.TxVerifyResponse{
			Code:    common.Code_INVALID_PARAMETER,
			Message: utils.UnsupportVersion(req.Version),
		}, nil
	}
}

// IsCrossChainSuccess 判断跨链结果
//
//	@receiver h
//	@param ctx
//	@param req
//	@return *cross_chain.IsCrossChainSuccessResponse
//	@return error
func (h *Handler) IsCrossChainSuccess(
	ctx context.Context,
	req *cross_chain.IsCrossChainSuccessRequest) (*cross_chain.IsCrossChainSuccessResponse, error) {
	h.printRequest(ctx, "IsCrossChainSuccess", fmt.Sprintf("%+v", req))
	// 根据业务做一些处理，这里一律让他失败
	return &cross_chain.IsCrossChainSuccessResponse{
		CrossChainResult: false,
		Code:             common.Code_GATEWAY_SUCCESS,
		Message:          common.Code_GATEWAY_SUCCESS.String(),
	}, nil
}

// PingPong 心跳
//
//	@receiver h
//	@param ctx
//	@param req
//	@return *cross_chain.PingPongResponse
//	@return error
func (h *Handler) PingPong(ctx context.Context, req *emptypb.Empty) (*cross_chain.PingPongResponse, error) {
	//h.printRequest(ctx, "PingPong", fmt.Sprintf("%+v", req))

	return &cross_chain.PingPongResponse{
		ChainOk: chain_client.ChainClientV1.CheckChain(),
	}, nil
}

// printRequest 打印请求信息
//
//	@receiver h
//	@param ctx
//	@param method
//	@param request
func (h *Handler) printRequest(ctx context.Context, method, request string) {
	pr, ok := peer.FromContext(ctx)
	var addr string
	if !ok || pr.Addr == net.Addr(nil) {
		h.log.Errorf("getClientAddr FromContext failed")
		addr = "unknown"
	} else {
		addr = pr.Addr.String()
	}

	h.log.Infof("[%s]: |%s|%s", method, addr, request)
}

// getCrossChainTryReturn 创建crosschaintry返回值
//
//	@param code
//	@param crossChainId
//	@param crossChainName
//	@param crossChainFlag
//	@param msg
//	@param txContent
//	@param tryResult
//	@return *cross_chain.CrossChainTryResponse
//	@return error
func getCrossChainTryReturn(
	code common.Code, crossChainId,
	crossChainName, crossChainFlag,
	msg string, txContent *common.TxContent,
	tryResult []string) (*cross_chain.CrossChainTryResponse, error) {
	return &cross_chain.CrossChainTryResponse{
		CrossChainId:   crossChainId,
		CrossChainName: crossChainName,
		CrossChainFlag: crossChainFlag,
		TxContent:      txContent,
		TryResult:      tryResult,
		Code:           code,
		Message:        msg,
	}, nil
}

// fillTryResult 填充跨链查询内容
//
//	@param param
//	@param tryResult
//	@param crossType
//	@return string
//	@return error
func fillTryResult(param string, tryResult []string, crossType common.CrossType) (string, error) {
	if param == "" {
		return nilParam, nil
	}
	if crossType == common.CrossType_INVOKE {
		return param, nil
	}
	tryResultCount := strings.Count(param, common.TryResult_TRY_RESULT.String())
	if len(tryResult) == 0 || tryResultCount == 0 {
		return param, nil
	}
	if len(tryResult) != tryResultCount {
		return "", fmt.Errorf("\"%s\" count != len(TryResult), please update event config",
			common.TryResult_TRY_RESULT.String())
	}
	param = strings.Replace(param, common.TryResult_TRY_RESULT.String(), "%s", -1)
	paramData := make([]interface{}, len(tryResult))
	for j, v := range tryResult {
		paramData[j] = v
	}
	param = fmt.Sprintf(param, paramData...)
	return param, nil
}

// fillTbisResult 填充tbis执行结果
//  @param kvJsonStr
//  @param chainRid
//  @param proveStatus
//  @param contractStatus
//  @param contractResult
//  @return string
//  @return error
//func fillTbisResult(param, chainRid string,
//	proveStatus, contractStatus int, contractResult string) (string, error) {
//	res := tbis_event.GetCommitParam(chainRid, proveStatus, contractStatus, contractResult)
//	if param == "" {
//		return fmt.Sprintf("[\"%s\"]", res), nil
//	}
//	paramArr := make([]interface{}, 0)
//	err := json.Unmarshal([]byte(param), &paramArr)
//	if err != nil {
//		return "", fmt.Errorf("unmarshal param error: %s", err.Error())
//	}
//	paramArr[0] = res
//	resStr, _ := json.Marshal(paramArr)
//	return string(resStr), nil
//}
