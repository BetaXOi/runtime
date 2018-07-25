// Copyright (c) 2017 HyperHQ Inc.
// Copyright (c) 2018 Huawei Corporation.
//
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	vc "github.com/kata-containers/runtime/virtcontainers"
	"github.com/urfave/cli"
)

type networkState struct {
	Interface   string `json:"interface"`
	MAC         string `json:"MAC"`
	Type        string `json:"type"`
	HotPlugable bool   `json:"hotplugable"`
}

type formatNetworkState interface {
	Write(state []networkState, file *os.File) error
}
type formatNetworkJSON struct{}
type formatNetworkList struct{}
type formatNetworkTabular struct{}

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
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "format, f",
			Value: "table",
			Usage: `select one of: ` + formatOptions,
		},
		cli.BoolFlag{
			Name:  "quiet, q",
			Usage: "display only network interface",
		},
	},
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

		s, err := getNetworks(sandboxID)

		file := defaultOutputFile
		var fs formatNetworkState = formatNetworkList{}

		if context.Bool("quiet") {
			fs = formatNetworkList{}
		} else {
			switch context.String("format") {
			case "table":
				fs = formatNetworkTabular{}
			case "json":
				fs = formatNetworkJSON{}
			default:
				return fmt.Errorf("invalid format option")
			}
		}

		return fs.Write(s, file)
	},
}

var netAttachCommand = cli.Command{
	Name:      "attach",
	Usage:     "attach network",
	ArgsUsage: `attach <container> <network>`,
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "default, d",
			Usage: "specify the network as the default route",
		},
	},
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
		asDefault := context.Bool("default")
		return vci.AttachNetwork(sandboxID, network, asDefault)
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

func getNetworks(sandboxID string) ([]networkState, error) {
	var s []networkState

	endpointList, err := vci.ListNetwork(sandboxID)
	if err != nil {
		return nil, err
	}

	// TODO: need more detail info(tap, ip, route...)
	for _, endpoint := range endpointList {
		s = append(s, networkState{
			Interface:   endpoint.Name(),
			MAC:         endpoint.HardwareAddr(),
			Type:        string(endpoint.Type()),
			HotPlugable: false,
		})
	}

	return s, nil
}

func (f formatNetworkList) Write(state []networkState, file *os.File) error {
	for _, item := range state {
		_, err := fmt.Fprintln(file, item.Interface)
		if err != nil {
			return err
		}
	}

	return nil
}

func (f formatNetworkTabular) Write(state []networkState, file *os.File) error {
	// values used by runc
	flags := uint(0)
	minWidth := 12
	tabWidth := 1
	padding := 3

	w := tabwriter.NewWriter(file, minWidth, tabWidth, padding, ' ', flags)

	fmt.Fprint(w, "INTERFACE\tMAC\tTYPE\tHOTPLUGABLE\n")

	for _, item := range state {
		fmt.Fprintf(w, "%s\t%s\t%s\t%t\n",
			item.Interface,
			item.MAC,
			item.Type,
			item.HotPlugable)
	}

	return w.Flush()
}

func (f formatNetworkJSON) Write(state []networkState, file *os.File) error {
	return json.NewEncoder(file).Encode(state)
}
