/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/

package grpcrequest

import (
	"chainmaker.org/chainmaker/common/v2/ca"
	"context"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"io/ioutil"
	"time"

	"chainmaker.org/chainmaker/tcip-bcos/v2/module/conf"

	"chainmaker.org/chainmaker/tcip-go/v2/common/relay_chain"

	"chainmaker.org/chainmaker/tcip-go/v2/api"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// GrpcRequest grpc请求结构体
type GrpcRequest struct {
	// 后续可以实现一个connection pool
	//conn map[string]api.RpcCrossChainClient
	log *zap.SugaredLogger
}

// NewGrpcRequest 初始化grpc请求
//
//	@param log
//	@return *GrpcRequest
func NewGrpcRequest(log *zap.SugaredLogger) *GrpcRequest {
	return &GrpcRequest{
		log: log,
	}
}

// BeginCrossChain 调用跨链接口
//
//	@receiver g
//	@param req
//	@return *relay_chain.BeginCrossChainResponse
//	@return error
func (g *GrpcRequest) BeginCrossChain(
	req *relay_chain.BeginCrossChainRequest) (*relay_chain.BeginCrossChainResponse, error) {
	timeout := conf.Config.BaseConfig.DefaultTimeout
	client, conn, err := g.getConnection()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	md := metadata.Pairs("x-token", conf.Config.Relay.AccessCode)
	metadataCtx := metadata.NewOutgoingContext(ctx, md)
	response, err := client.BeginCrossChain(metadataCtx, req)
	defer conn.Close()
	return response, err
}

// SyncBlockHeader 同步区块头
//
//	@receiver g
//	@param req
//	@return *relay_chain.SyncBlockHeaderResponse
//	@return error
func (g *GrpcRequest) SyncBlockHeader(
	req *relay_chain.SyncBlockHeaderRequest) (*relay_chain.SyncBlockHeaderResponse, error) {
	timeout := conf.Config.BaseConfig.DefaultTimeout
	client, conn, err := g.getConnection()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	md := metadata.Pairs("x-token", conf.Config.Relay.AccessCode)
	metadataCtx := metadata.NewOutgoingContext(ctx, md)
	response, err := client.SyncBlockHeader(metadataCtx, req)
	_ = conn.Close()
	return response, err
}

// 后续可以实现一个connection pool
//
//	@receiver g
//	@return api.RpcRelayChainClient
//	@return *grpc.ClientConn
//	@return error
func (g *GrpcRequest) getConnection() (api.RpcRelayChainClient, *grpc.ClientConn, error) {
	var kacp = keepalive.ClientParameters{
		Time:                10 * time.Second,
		Timeout:             time.Second,
		PermitWithoutStream: true,
	}
	var tlsClient ca.CAClient
	var (
		err error
	)
	caCert, err := ioutil.ReadFile(conf.Config.Relay.Tlsca)
	if err != nil {
		return nil, nil, err
	}
	clientCert, err := ioutil.ReadFile(conf.Config.Relay.ClientCert)
	if err != nil {
		return nil, nil, err
	}
	clientKey, err := ioutil.ReadFile(conf.Config.Relay.ClientKey)
	if err != nil {
		return nil, nil, err
	}
	tlsClient = ca.CAClient{
		ServerName: conf.Config.Relay.ServerName,
		CaCerts:    []string{string(caCert)},
		CertBytes:  clientCert,
		KeyBytes:   clientKey,
		Logger:     g.log,
	}

	c, err := tlsClient.GetCredentialsByCA()
	if err != nil {
		return nil, nil, err
	}
	conn, err := grpc.Dial(
		conf.Config.Relay.Address,
		grpc.WithTransportCredentials(*c),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(conf.Config.RpcConfig.MaxRecvMsgSize*1024*1024),
			grpc.MaxCallSendMsgSize(conf.Config.RpcConfig.MaxSendMsgSize*1024*1024),
		),
		grpc.WithKeepaliveParams(kacp),
	)
	if err != nil {
		return nil, nil, err
	}
	return api.NewRpcRelayChainClient(conn), conn, nil

}
