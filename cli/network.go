// Copyright (c) 2017 HyperHQ Inc.
// Copyright (c) 2018 Huawei Corporation.
//
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	"fmt"

	vc "github.com/kata-containers/runtime/virtcontainers"
	"github.com/urfave/cli"
)

var networkCLICommand = cli.Command{
	Name:  "network",
	Usage: "manage network of container",
	Subcommands: []cli.Command{
		netListCommand,
		netAttachCommand,
		netDetachCommand,
	},
	Action: func(context *cli.Context) error {
		return cli.ShowSubcommandHelp(context)
	},
}

var netListCommand = cli.Command{
	Name:      "ls",
	Aliases:   []string{"list"},
	Usage:     "list networks conneted to the container",
	ArgsUsage: `ls <container>`,
	Flags:     []cli.Flag{},
	Action: func(context *cli.Context) error {
		if context.Args().Present() == false {
			return fmt.Errorf("missing container ID, should provide one")
		}
		containerID := context.Args().First()
		status, sandboxID, err := getExistingContainerInfo(containerID)
		if err != nil {
			return err
		}

		containerID = status.ID
		// container MUST be running
		if status.State.State != vc.StateRunning {
			return fmt.Errorf("container %s is not running", containerID)
		}

		_, err = vci.ListNetwork(sandboxID)
		if err != nil {
			return err
		}

		return nil
	},
}

var netAttachCommand = cli.Command{
	Name:      "attach",
	Usage:     "attach network",
	ArgsUsage: `attach <container> <network>`,
	Flags:     []cli.Flag{},
	Action: func(context *cli.Context) error {
		if context.Args().Present() == false {
			return fmt.Errorf("missing container ID, should provide one")
		}
		containerID := context.Args().First()
		status, sandboxID, err := getExistingContainerInfo(containerID)
		if err != nil {
			return err
		}

		containerID = status.ID
		// container MUST be running
		if status.State.State != vc.StateRunning {
			return fmt.Errorf("container %s is not running", containerID)
		}

		network := context.Args().Get(1)
		return vci.AttachNetwork(sandboxID, network)
	},
}

var netDetachCommand = cli.Command{
	Name:      "detach",
	Usage:     "detach network",
	ArgsUsage: `detach <container> <network>`,
	Flags:     []cli.Flag{},
	Action: func(context *cli.Context) error {
		if context.Args().Present() == false {
			return fmt.Errorf("missing container ID, should provide one")
		}
		containerID := context.Args().First()
		status, sandboxID, err := getExistingContainerInfo(containerID)
		if err != nil {
			return err
		}

		containerID = status.ID
		// container MUST be running
		if status.State.State != vc.StateRunning {
			return fmt.Errorf("container %s is not running", containerID)
		}

		network := context.Args().Get(1)
		return vci.DetachNetwork(sandboxID, network)
	},
}
