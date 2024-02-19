/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/

package chain_client

import (
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/conf"
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/db"
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/utils"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"

	"chainmaker.org/chainmaker/tcip-bcos/v2/module/request"

	"go.uber.org/zap"

	"chainmaker.org/chainmaker/tcip-bcos/v2/module/logger"
	tcipcommon "chainmaker.org/chainmaker/tcip-go/v2/common"
	bcosabi "github.com/FISCO-BCOS/go-sdk/abi"
	bcosbind "github.com/FISCO-BCOS/go-sdk/abi/bind"
	sdk "github.com/FISCO-BCOS/go-sdk/client"
	bcostypes "github.com/FISCO-BCOS/go-sdk/core/types"
	bcoscommon "github.com/ethereum/go-ethereum/common"
)

const (
	toBlock = "latest"
)

// ChainClientItfc 链客户端接口
type ChainClientItfc interface {
	// InvokeContract 调用合约
	InvokeContract(chainRid, contractName, method, abiStr string, args string,
		needTx bool) ([]string, *bcostypes.TransactionDetail, error)
	// GetTxProve 获取交易凭证
	GetTxProve(tx *bcostypes.TransactionDetail, chainRid string) string
	// TxProve 交易验证
	TxProve(txProve string) bool
	// CheckChain 验证了链的连通性
	CheckChain() bool
}

// ChainClient 链客户端结构体
type ChainClient struct {
	// 缓存链的客户端对象
	client map[string]*sdk.Client
	// 日志对象
	log *zap.SugaredLogger
}

// ChainClientV1 连交互模块对象
var ChainClientV1 ChainClientItfc

const (
	emptyJson = "{}"
)

// InitChainClient 初始化链客户端
//
//	@return error
func InitChainClient() error {
	log := logger.GetLogger(logger.ModuleChainmakerClient)
	log.Debug("[InitChainClient] init")
	bcosClient := &ChainClient{
		client: make(map[string]*sdk.Client),
		log:    logger.GetLogger(logger.ModuleChainClient),
	}
	for _, chainConfig := range conf.Config.ChainConfig {
		cc, err := createSDK(chainConfig.SdkConfigPath)
		if err != nil {
			log.Errorf("[InitChainClient] Create chain client error failed, err: %v", err)
			return err
		}
		log.Debugf("[InitChainClient] create chain [%s] client success", chainConfig.ChainRid)

		bcosClient.client[chainConfig.ChainRid] = cc
		if conf.Config.BaseConfig.TxVerifyType == conf.SpvTxVerify {
			if err1 := bcosClient.syncBlockHeaderBath(chainConfig.ChainRid); err1 != nil {
				panic(err1)
			}
			go bcosClient.listenBlockHeader(chainConfig.ChainRid)
		}
		err = bcosClient.listenEvent(chainConfig.ChainRid, chainConfig.CrossContractName)
		if err != nil {
			log.Errorf("[InitChainClient] listenEvent error, err: %v", err)
			return err
		}
	}
	ChainClientV1 = bcosClient
	return nil
}

// listenBlockHeader 监听区块头
//
//	@receiver c
//	@param chainRid
//	@param startBlock
func (c *ChainClient) listenBlockHeader(chainRid string) {
	interval := time.Duration(conf.Config.BlockHeaderSync.Interval) * time.Second

	timer := time.NewTimer(interval)

	for {
		select {
		case <-timer.C:
			err := c.syncBlockHeaderBath(chainRid)
			if err != nil {
				c.log.Errorf("[listenBlockHeader] %s", err.Error())
			}
			timer.Reset(interval)
		}
	}
}

func (c *ChainClient) syncBlockHeaderBath(chainRid string) error {
	client, err := c.getChainClient(chainRid)
	if err != nil {
		c.log.Errorf("[syncBlockHeaderBath] %s", err.Error())
		return err
	}
	startBlock := c.getLaseBlockHeaderHeight(chainRid)
	lastBlockHeight, err := client.GetBlockNumber(context.Background())
	if err != nil {
		c.log.Errorf("[syncBlockHeaderBath] %s, GetCurrentBlockHeight error", err.Error())
		return err
	}
	c.log.Infof("[syncBlockHeaderBath] startBlock %d, lastBlockHeight %d", startBlock, lastBlockHeight)
	const errorFormat = "[syncBlockHeaderBath] %s, startBlock %d, lastBlockHeight %d"
	if startBlock != 0 {
		startBlock += 1
	}
	needSyncCount := lastBlockHeight - startBlock + 1
	blockHeaderBatch := make([]string, 0)
	if needSyncCount < conf.Config.BlockHeaderSync.BatchCount {
		for i := startBlock; i <= lastBlockHeight; i++ {
			block, err := client.GetBlockByNumber(context.Background(), i, false)
			if err != nil {
				c.log.Errorf(errorFormat, err.Error(), startBlock, lastBlockHeight)
				return fmt.Errorf(errorFormat, err.Error(), startBlock, lastBlockHeight)
			}
			blockHeaderBatch = append(blockHeaderBatch, c.getLaseBlockHeaderByteBase64(block))
		}
		if len(blockHeaderBatch) == 0 {
			return nil
		}
		request.RequestV1.SyncBlockHeader(nil, blockHeaderBatch, chainRid, uint64(lastBlockHeight))
		return nil
	} else {
		reqCount := needSyncCount / conf.Config.BlockHeaderSync.BatchCount
		if needSyncCount%conf.Config.BlockHeaderSync.BatchCount != 0 {
			reqCount += 1
		}
		for i := int64(0); i < reqCount; i++ {
			successBlockHeight := uint64(0)
			blockHeaderBatch = make([]string, 0)
			for j := int64(0); j < conf.Config.BlockHeaderSync.BatchCount; j++ {
				blockHeight := startBlock + i*conf.Config.BlockHeaderSync.BatchCount + j
				if blockHeight > lastBlockHeight {
					break
				}
				block, err := client.GetBlockByNumber(context.Background(), blockHeight, false)
				if err != nil {
					c.log.Errorf(errorFormat, err.Error(), startBlock, lastBlockHeight)
					return fmt.Errorf(errorFormat, err.Error(), startBlock, lastBlockHeight)
				}
				blockHeaderBatch = append(blockHeaderBatch, c.getLaseBlockHeaderByteBase64(block))
				successBlockHeight = uint64(blockHeight)
			}
			if len(blockHeaderBatch) == 0 {
				continue
			}
			request.RequestV1.SyncBlockHeader(nil, blockHeaderBatch, chainRid, successBlockHeight)
		}
	}
	return nil
}

func (c *ChainClient) getLaseBlockHeaderByteBase64(blockHeader *bcostypes.Block) string {
	resByte, _ := json.Marshal(blockHeader)
	return base64.StdEncoding.EncodeToString(resByte)
}

// listenEvent 监听合约事件
//
//	@receiver c
//	@param dbEvent
//	@return error
func (c *ChainClient) listenEvent(chainRid, contractName string) error {
	startBlcok := c.getLaseCrossHeight(chainRid)
	client, err := c.getChainClient(chainRid)
	if err != nil {
		msg := fmt.Sprintf("[listenEvent] chain client error: %s\n", err.Error())
		c.log.Error(msg)
		return errors.New(msg)
	}
	topics := []string{
		bcoscommon.BytesToHash(
			crypto.Keccak256(
				[]byte(
					fmt.Sprintf("%s(string,string)", tcipcommon.EventName_CROSS_CHAIN_TRIGGER.String()),
				),
			),
		).Hex(),
	}
	eventLogParams := bcostypes.EventLogParams{
		FromBlock: fmt.Sprintf("%d", startBlcok),
		ToBlock:   toBlock,
		GroupID:   fmt.Sprintf("%d", client.GetGroupID()),
		Topics:    topics,
		Addresses: []string{contractName},
	}
	err = client.SubscribeEventLogs(eventLogParams, func(status int, logs []bcostypes.Log) {
		logRes, err2 := json.MarshalIndent(logs, "", "  ")
		if err2 != nil {
			c.log.Warnf("[listenEvent] logs marshalIndent error: %v", err2)
		}
		c.log.Debugf("[listenEvent] received: %s\n", logRes)
		args := make(bcosabi.Arguments, 0)
		for _, abi := range abis {
			arg := &bcosabi.Argument{}
			err2 = arg.UnmarshalJSON([]byte(abi))
			if err2 != nil {
				msg := fmt.Sprintf("[listenEvent] UnmarshalJSON abi [%s] error: %s", abi, err2.Error())
				c.log.Errorf(msg)
				return
			}

			args = append(args, *arg)
		}
		eve := &bcosabi.Event{
			Name:      tcipcommon.EventName_CROSS_CHAIN_TRIGGER.String(),
			RawName:   tcipcommon.EventName_CROSS_CHAIN_TRIGGER.String(),
			Anonymous: false,
			SMCrypto:  false,
			Inputs:    args,
		}
		text, err2 := eve.Inputs.UnpackValues(logs[0].Data)
		if err2 != nil {
			msg := fmt.Sprintf("[listenEvent] UnpackValues event data [%s] error: %s",
				tcipcommon.EventName_CROSS_CHAIN_TRIGGER.String(), err2.Error())
			c.log.Errorf(msg)
			return
		}
		data := fmt.Sprintf("%s", text)
		// 前后各去掉一个字符
		data = data[1:]
		data = data[:len(data)-1]

		if len(data) == 0 {
			msg := fmt.Sprintf("[listenEvent] nil data [%s], %s",
				tcipcommon.EventName_CROSS_CHAIN_TRIGGER.String(), fmt.Sprintf("%s", text))
			c.log.Warn(msg)
			return
		}
		var ctx1 context.Context
		tx, err2 := client.GetTransactionByHash(ctx1, logs[0].TxHash)
		if err2 != nil {
			msg := fmt.Sprintf("[listenEvent] get tx error [%s]", logs[0].TxHash)
			c.log.Warn(msg)
			return
		}
		txProve := c.GetTxProve(tx, chainRid)
		var txByte []byte
		if txByte, err2 = json.Marshal(tx); err2 != nil {
			msg := fmt.Sprintf("[listenEvent] Marshal tx error [%s]", tx.Hash)
			c.log.Warn(msg)
			return
		}
		eventData := make([]string, 0)
		// 不管topic了
		//for _, v := range logs[0].Topics {
		//	eventData = append(eventData, v.Hex())
		//}
		eventData = append(eventData, strings.Split(data, " ")...)
		eventInfo := &utils.EventInfo{
			Topic:        tcipcommon.EventName_CROSS_CHAIN_TRIGGER.String(),
			ChainRid:     chainRid,
			ContractName: contractName,
			TxProve:      txProve,
			Data:         eventData,
			Tx:           txByte,
			TxId:         logs[0].TxHash.Hex(),
			BlockHeight:  int64(logs[0].BlockNumber),
		}
		c.log.Infof("[listenEvent] eventInfo: %v\n", eventInfo.ToString())

		go request.RequestV1.BeginCrossChain(eventInfo)
	})
	if err != nil {
		c.log.Errorf("[listenEvent] listen ChainRid %s error: %s", chainRid, err.Error())
		return fmt.Errorf("[listenEvent] listen ChainRid %s error: %s", chainRid, err.Error())
	}
	c.log.Infof("[listenEvent] listen ChainRid %s success: eventName %s address %s",
		chainRid, tcipcommon.EventName_CROSS_CHAIN_TRIGGER.String(), contractName)
	return nil
}

// InvokeContract 调用合约
//
//	@receiver c
//	@param chainRid 链资源id
//	@param contractName 合约名称
//	@param method 调用方法
//	@param abiStr abi
//	@param args 参数
//	@param needTx 是否需要交易
//	@param paramType 参数类型
//	@return []string 返回参数
//	@return *bcostypes.TransactionDetail 交易
//	@return error 错误信息
func (c *ChainClient) InvokeContract(chainRid, contractName, method, abiStr string, args string,
	needTx bool) ([]string, *bcostypes.TransactionDetail, error) {
	argsArr, err := dealParam(args)
	if err != nil {
		msg := fmt.Sprintf("[InvokeContract] dealParam error: %s\n", err.Error())
		c.log.Error(msg)
		return nil, nil, errors.New(msg)
	}
	client, err := c.getChainClient(chainRid)
	if err != nil {
		msg := fmt.Sprintf("[InvokeContract] chain client error: %s\n", err.Error())
		c.log.Error(msg)
		return nil, nil, errors.New(msg)
	}

	address := bcoscommon.HexToAddress(contractName)
	parsed, err := bcosabi.JSON(strings.NewReader(abiStr))
	if err != nil {
		msg := fmt.Sprintf("[InvokeContract] abi [%s] read error: %s", abiStr, err.Error())
		c.log.Error(msg)
		return nil, nil, errors.New(msg)
	}

	_, receipt, err := bcosbind.NewBoundContract(address, parsed, client, client, client).
		Transact(client.GetTransactOpts(), method, argsArr...)

	if err != nil {
		msg := fmt.Sprintf("[InvokeContract] invoke contract [%s %s %s] error: %s\n, abi: %s, args: %v",
			chainRid, contractName, method, err.Error(), abiStr, args)
		c.log.Error(msg)
		return nil, nil, errors.New(msg)
	}

	c.log.Debugf("[InvokeContract] invoke contract [%s %s %s] resp: %v\n, abi: %s, args: %v",
		chainRid, contractName, method, receipt, abiStr, args)
	if receipt.Status != bcostypes.Success {
		msg := fmt.Sprintf("[InvokeContract] invoke contract [%s %s %s] error: %s\n, abi: %s, args: %v",
			chainRid, contractName, method, "status error", abiStr, args)
		c.log.Error(msg)
		return nil, nil, errors.New(msg)
	}

	resArr := make([]string, 0)
	if _, ok := parsed.Methods[method]; ok && len(parsed.Methods[method].Outputs) != 0 {
		b, err := hex.DecodeString(receipt.Output[2:])
		if err != nil {
			msg := fmt.Sprintf("[InvokeContract] Decode output [%s %s %s] error: %s\n, abi: %s, args: %v",
				chainRid, contractName, method, err.Error(), abiStr, args)
			c.log.Error(msg)
			return nil, nil, errors.New(msg)
		}
		methodApi := bcosabi.Method{
			Name:     method,
			RawName:  method,
			SMCrypto: false,
			Outputs:  parsed.Methods[method].Outputs,
		}
		text, err := methodApi.Outputs.UnpackValues(b)
		if err != nil {
			msg := fmt.Sprintf("[InvokeContract] UnpackValues output [%s %s %s] error: %s\n, abi: %s, args: %v",
				chainRid, contractName, method, err.Error(), abiStr, args)
			c.log.Error(msg)
			return nil, nil, errors.New(msg)
		}
		res := fmt.Sprintf("%s", text)
		res = res[1:]
		res = res[:len(res)-1]
		resArr = strings.Split(res, " ")
	}

	if needTx {
		var ctx context.Context
		tx, err := client.GetTransactionByHash(ctx, bcoscommon.HexToHash(receipt.TransactionHash))
		if err != nil {
			c.log.Debugf("[InvokeContract] get tx error [%s %s %s] error: %s\n, abi: %s, args: %v",
				chainRid, contractName, method, err.Error(), abiStr, args)
			msg := fmt.Sprintf("[InvokeContract] get tx error [%s]", err.Error())
			c.log.Warn(msg)
			return resArr, tx, nil
		}
		return resArr, tx, nil
	}

	// 如果是查询需要获取这里的结果，那么这里的值需要在跨链合约中进行处理，这里无法获取
	return resArr, nil, nil
}

// GetTxProve 获取交易证明
//
//	@receiver c
//	@param tx 交易
//	@param chainRid 链资源id
//	@return string 交易证明
func (c *ChainClient) GetTxProve(tx *bcostypes.TransactionDetail, chainRid string) string {
	if conf.Config.BaseConfig.TxVerifyType == conf.NotNeedTxVerify {
		return emptyJson
	}
	// 获取凭证的时候一定会需要用到交易验证，这时候去同步一下区块头
	err := c.syncBlockHeaderBath(chainRid)
	if err != nil {
		c.log.Errorf("[GetTxProve] %s", err.Error())
		return emptyJson
	}

	txProve := make(map[string][]byte)
	txProve["tx_id"] = []byte(tx.Hash)
	if txProve["tx_byte"], err = json.Marshal(tx); err != nil {
		return emptyJson
	}
	txProve["chain_rid"] = []byte(chainRid)
	res, err := json.Marshal(txProve)
	if err != nil {
		return ""
	}
	return string(res)
}

// CheckChain 检查链的连通性
//
//	@receiver c
//	@return bool
func (c *ChainClient) CheckChain() bool {
	for _, client := range c.client {
		if _, err := client.GetBlockNumber(context.Background()); err != nil {
			return false
		}
	}
	return true
}

// TxProve 交易认证
//
//	@receiver c
//	@param txProve
//	@return bool
func (c *ChainClient) TxProve(txProve string) bool {
	c.log.Debugf("txProve: %s\n", txProve)
	txProveMap := make(map[string][]byte)
	err := json.Unmarshal([]byte(txProve), &txProveMap)
	if err != nil {
		c.log.Errorf("[TxProve] Unmarshal error: %s", err.Error())
		return false
	}
	chainRid, ok := txProveMap["chain_rid"]
	if !ok {
		c.log.Errorf("[TxProve] chain_id not found: %s", err.Error())
		return false
	}
	txHash, ok := txProveMap["tx_hash"]
	if !ok {
		c.log.Errorf("[TxProve] tx_hash not found: %s", err.Error())
		return false
	}
	txByteString, ok := txProveMap["tx_byte"]
	if !ok {
		c.log.Errorf("[TxProve] tx_byte not found: %s", err.Error())
		return false
	}

	client, err := c.getChainClient(string(chainRid))
	if err != nil {
		c.log.Errorf("[TxProve] get client error %s", err.Error())
		return false
	}
	var ctx context.Context
	tx, err := client.GetTransactionByHash(ctx, bcoscommon.HexToHash(string(txHash)))
	if err != nil {
		c.log.Errorf("[TxProve] get tx error %s", err.Error())
		return false
	}
	txChainByte, err := json.Marshal(tx)
	if err != nil {
		c.log.Errorf("[TxProve] Marshal tx error %s", err.Error())
		return false
	}
	if string(txByteString) != "" && string(txByteString) == string(txChainByte) {
		return true
	}
	c.log.Errorf("[TxProve] Compare tx error\n%s\n%s\n", string(txByteString), string(txChainByte))
	return false
}

// getChainClient 获取链客户端
//
//	@receiver c
//	@param chainRid
//	@return *sdk.Client
//	@return error
func (c *ChainClient) getChainClient(chainRid string) (*sdk.Client, error) {
	if _, ok := c.client[chainRid]; !ok {
		msg := fmt.Sprintf("[getChainClient] no chain client: chainRid %s", chainRid)
		c.log.Warnf(msg)
		return nil, fmt.Errorf(msg)
	}
	return c.client[chainRid], nil
}

// getListenKey 拼接监听缓存的key
//
//	@param chainRid
//	@param eventName
//	@return string
func getListenKey(chainRid, contractName, eventName string) string {
	return fmt.Sprintf("%s#%s#%s", chainRid, contractName, eventName)
}

// dealParam 处理合约调用参数,规定一下，统一按照string处理
//
//	@param args
//	@param paramType
//	@return []interface{}
//	@return error
func dealParam(args string) ([]interface{}, error) {
	argsArr := make([]interface{}, 0)
	err := json.Unmarshal([]byte(args), &argsArr)
	if err != nil {
		return nil, fmt.Errorf("umarshal args error: %s", err.Error())
	}

	return argsArr, nil
}

func (c *ChainClient) getLaseBlockHeaderHeight(chainRid string) int64 {
	height, err := db.Db.Get([]byte(fmt.Sprintf("%s_last_block_header_height", chainRid)))
	if err != nil {
		c.log.Errorf("[getLaseBlockHeaderHeight] %s", err.Error())
		return 0
	}
	if len(height) == 0 {
		return 0
	}
	dbHeight, err := strconv.ParseInt(string(height), 10, 64)
	if err != nil {
		c.log.Errorf("[getLaseBlockHeaderHeight] %s", err.Error())
		return 0
	}
	return dbHeight
}

func (c *ChainClient) getLaseCrossHeight(chainRid string) int64 {
	height, err := db.Db.Get([]byte(fmt.Sprintf("%s_last_cross_height", chainRid)))
	if err != nil {
		c.log.Errorf("[getLaseCrossHeight] %s", err.Error())
		return 0
	}
	if len(height) == 0 {
		return 0
	}
	dbHeight, err := strconv.ParseInt(string(height), 10, 64)
	if err != nil {
		c.log.Errorf("[getLaseCrossHeight] %s", err.Error())
		return 0
	}
	return dbHeight
}
