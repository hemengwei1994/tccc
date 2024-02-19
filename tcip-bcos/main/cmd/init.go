/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/

package cmd

import (
	"fmt"
	"os"

	"chainmaker.org/chainmaker/tcip-bcos/v2/module/conf"
	"github.com/spf13/cobra"
)

const (
	flagNameOfConfigFilepath          = "conf-file"
	flagNameShortHandOfConfigFilepath = "c"
)

func initLocalConfig(cmd *cobra.Command) {
	if err := conf.InitLocalConfig(cmd); err != nil {
		fmt.Println(err)
		os.Exit(0)
	}
}
