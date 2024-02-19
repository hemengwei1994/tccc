/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/

package rpcserver

import (
	"chainmaker.org/chainmaker/common/v2/ca"
	"context"
	"crypto/x509"
	"fmt"
	"google.golang.org/grpc/keepalive"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	"github.com/tmc/grpc-websocket-proxy/wsproxy"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	cmtls "chainmaker.org/chainmaker/common/v2/crypto/tls"
	cmx509 "chainmaker.org/chainmaker/common/v2/crypto/x509"
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/handler"
	"chainmaker.org/chainmaker/tcip-bcos/v2/module/logger"
	tcipApi "chainmaker.org/chainmaker/tcip-go/v2/api"

	"chainmaker.org/chainmaker/tcip-bcos/v2/module/conf"

	"go.uber.org/zap"

	"github.com/cloudflare/cfssl/log"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"google.golang.org/grpc"
)

var (
	rpcLog *zap.SugaredLogger
)

// RPCServer rpc服务结构体
type RPCServer struct {
	grpcServer *grpc.Server
	config     *conf.RpcConfig
	log        *zap.SugaredLogger
	ctx        context.Context
	cancel     context.CancelFunc
	isShutdown bool
	mixServer  *http.Server
}

// NewRpcServer 新建rpc服务
//
//	@return *RPCServer
//	@return error
func NewRpcServer() (*RPCServer, error) {

	grpcServer, err := newGrpc()
	if err != nil {
		return nil, fmt.Errorf("new grpc server failed, %s", err.Error())
	}

	mixServer, err := newMixServer(grpcServer)
	if err != nil {
		return nil, fmt.Errorf("new http grpc server failed, %s", err.Error())
	}

	rpcLog = logger.GetLogger(logger.ModuleRpcServer)
	return &RPCServer{
		grpcServer: grpcServer,
		mixServer:  mixServer,
		log:        rpcLog,
	}, nil
}

// Start - start RPCServer
//
//	@receiver s
//	@return error
func (s *RPCServer) Start() error {
	var (
		err       error
		tlsConfig *cmtls.Config
		caCerts   []string
	)

	s.ctx, s.cancel = context.WithCancel(context.Background())

	s.isShutdown = false

	if err = s.RegisterHandler(); err != nil {
		return fmt.Errorf("register handler failed, %s", err.Error())
	}

	caCert, err := ioutil.ReadFile(conf.Config.RpcConfig.TLSConfig.CaFile)
	if err != nil {
		log.Errorf("read ca file failed, %s", err.Error())
		return err
	}

	caCerts = []string{string(caCert)}

	tlsConfig, err = ca.GetTLSConfig(conf.Config.RpcConfig.TLSConfig.CertFile,
		conf.Config.RpcConfig.TLSConfig.KeyFile, []string{}, caCerts,
		"", "")

	if err != nil {
		log.Errorf("GetTLSConfig, failed, %s", err.Error())
		return err
	}

	endPoint := fmt.Sprintf("%s:%d", "0.0.0.0",
		conf.Config.RpcConfig.Port)
	conn, err := net.Listen("tcp", endPoint)
	if err != nil {
		return fmt.Errorf("TCP listen failed, %s", err.Error())
	}

	go func() {
		err = s.mixServer.Serve(ca.NewTLSListener(conn, tlsConfig))
		if err == http.ErrServerClosed {
			s.log.Info("RPCServer http closed")
		} else {
			s.log.Errorf("RPCServer http serve failed, %s", err.Error())
		}
	}()

	s.log.Infof("gRPC server listen on %s", endPoint)

	return nil
}

// RegisterHandler - register apiservice handler to rpcserver
//
//	@receiver s
//	@return error
func (s *RPCServer) RegisterHandler() error {
	apiHandler := handler.NewHandler()
	tcipApi.RegisterRpcCrossChainServer(s.grpcServer, apiHandler)
	return nil
}

// Stop - stop RPCServer
//
//	@receiver s
func (s *RPCServer) Stop() {
	s.isShutdown = true
	s.cancel()
	s.grpcServer.GracefulStop()
	s.log.Info("RPCServer is stopped!")
}

// newGrpc - new GRPC object
func newGrpc() (*grpc.Server, error) {
	var opts []grpc.ServerOption
	opts = []grpc.ServerOption{
		grpc_middleware.WithUnaryServerChain(
			RecoveryInterceptor,
			LoggingInterceptor,
			BlackListInterceptor(),
		),
	}

	caCert, err := ioutil.ReadFile(conf.Config.RpcConfig.TLSConfig.CaFile)
	if err != nil {
		log.Errorf("read ca file failed, %s", err.Error())
		return nil, err
	}

	caCerts := []string{string(caCert)}

	tlsRPCServer := ca.CAServer{
		CaCerts:  caCerts,
		CertFile: conf.Config.RpcConfig.TLSConfig.CertFile,
		KeyFile:  conf.Config.RpcConfig.TLSConfig.KeyFile,
		Logger:   rpcLog,
	}

	customVerify := ca.CustomVerify{
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			return nil
		},
		GMVerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*cmx509.Certificate) error {
			return nil
		},
	}

	c, err := tlsRPCServer.GetCredentialsByCA(false, customVerify)
	if err != nil {
		log.Errorf("new gRPC failed, GetTLSCredentialsByCA err: %v", err)
		return nil, err
	}

	opts = append(opts, grpc.Creds(*c))

	opts = append(opts, grpc.MaxSendMsgSize(conf.Config.RpcConfig.MaxSendMsgSize*1024*1024))
	opts = append(opts, grpc.MaxRecvMsgSize(conf.Config.RpcConfig.MaxRecvMsgSize*1024*1024))

	// keep alive
	var kaep = keepalive.EnforcementPolicy{
		MinTime:             2 * time.Second, // If a client pings more than once every 2 seconds, terminate the connection
		PermitWithoutStream: true,            // Allow pings even when there are no active streams
	}
	var kasp = keepalive.ServerParameters{
		Time:    5 * time.Second, // Ping the client if it is idle for 5 seconds to ensure the connection is still active
		Timeout: 1 * time.Second, // Wait 1 second for the ping ack before assuming the connection is dead
	}
	opts = append(opts, grpc.KeepaliveEnforcementPolicy(kaep), grpc.KeepaliveParams(kasp))

	server := grpc.NewServer(opts...)

	return server, nil
}

func newMixServer(grpcServer *grpc.Server) (*http.Server, error) {

	var (
		mux        *http.ServeMux
		httpServer *http.Server
	)

	if conf.Config.RpcConfig.RestfulConfig.Enable {
		mux = http.NewServeMux()
		gwmux, err := newGateway()
		if err != nil {
			log.Error(err)
			return nil, err
		}

		mux.Handle("/", gwmux)
	}

	handler := GrpcHandlerFunc(grpcServer, mux)

	if conf.Config.RpcConfig.RestfulConfig.Enable {
		httpServer = &http.Server{
			Handler: wsproxy.WebsocketProxy(handler, wsproxy.WithMaxRespBodyBufferSize(
				conf.Config.RpcConfig.RestfulConfig.MaxRespBodySize*1024*1024))}
	} else {
		httpServer = &http.Server{Handler: handler}
	}

	return httpServer, nil
}

func newGateway() (http.Handler, error) {
	ctx := context.Background()

	dopts := []grpc.DialOption{}
	caCert, err := ioutil.ReadFile(conf.Config.RpcConfig.TLSConfig.CaFile)
	if err != nil {
		log.Errorf("read ca file failed, %s", err.Error())
		return nil, err
	}

	caCerts := []string{string(caCert)}

	tlsClient := ca.CAClient{
		CaCerts:  caCerts,
		CertFile: conf.Config.RpcConfig.TLSConfig.CertFile,
		KeyFile:  conf.Config.RpcConfig.TLSConfig.KeyFile,
		Logger:   rpcLog,
	}

	c, err := tlsClient.GetCredentialsByCA()
	if err != nil {
		log.Errorf("new gateway failed, GetTLSCredentialsByCA err: %v", err)
		return nil, err
	}

	dopts = append(dopts, grpc.WithTransportCredentials(*c))

	// NOTE: the mix http server certificate is valid for 127.0.0.1, so we must use 127.0.0.1
	endPoint := fmt.Sprintf("%s:%d", "127.0.0.1", conf.Config.RpcConfig.Port)

	gwmux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard,
			&runtime.JSONPb{OrigName: true, EmitDefaults: false, EnumsAsInts: true},
		),
	)

	if err := tcipApi.RegisterRpcCrossChainHandlerFromEndpoint(ctx, gwmux, endPoint, dopts); err != nil {
		log.Errorf("new gateway failed, RegisterRpcNodeHandlerFromEndpoint err: %v", err)
		return nil, err
	}

	return gwmux, nil
}
