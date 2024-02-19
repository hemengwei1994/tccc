/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/

package chain_client

import (
	"github.com/FISCO-BCOS/go-sdk/client"

	"github.com/FISCO-BCOS/go-sdk/conf"
)

var abis = []string{
	"{\"name\":\"req\",\"type\":\"string\"}",
	"{\"name\":\"param\",\"type\":\"string\"}",
}

// createSDK 创建bcos的sdk
//
//	@param bcosConfig
//	@param log
//	@return *client.Client
//	@return error
func createSDK(sdkConfigPath string) (*client.Client, error) {
	configs, err := conf.ParseConfigFile(sdkConfigPath)
	if err != nil {
		return nil, err
	}
	invokeConfig := &configs[0]
	cli, err := client.Dial(invokeConfig)
	return cli, err
}
