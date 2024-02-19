/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"fmt"

	"chainmaker.org/chainmaker/tcip-bcos/v2/main/cmd"
	"github.com/spf13/cobra"
)

// ./tcip-bcos start -c tcip-bcos.yml
func main() {
	mainCmd := &cobra.Command{Use: "tcip-bcos"}
	mainCmd.AddCommand(cmd.StartCMD())
	mainCmd.AddCommand(cmd.VersionCMD())

	err := mainCmd.Execute()
	if err != nil {
		fmt.Println(err)
	}
}
