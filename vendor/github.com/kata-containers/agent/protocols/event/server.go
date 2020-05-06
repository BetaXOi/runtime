// Copyright 2019 ZTE Inc.
//
// SPDX-License-Identifier: Apache-2.0
//
// gRPC agent event server wrapper

package event

import (
	"context"
	"strconv"
	"strings"

	"github.com/mdlayher/vsock"
	"google.golang.org/grpc"
	pbTypes "github.com/gogo/protobuf/types"
	pb "github.com/kata-containers/agent/protocols/grpc"
)

type EventServer struct {
	rpc   *grpc.Server
	Port  uint32
	Event chan string
}

func (s *EventServer) Ready(ctx context.Context, empty *pbTypes.Empty) (*pbTypes.Empty, error) {
	s.Event <- "Ready"

	return &pbTypes.Empty{}, nil
}

func StartEventServiceServer() (*EventServer, error) {
	lis, err := vsock.Listen(0)
	if err != nil {
		return nil, err
	}

	vals := strings.Split(lis.Addr().String(), ":")
	len := len(vals)
	port, err := strconv.Atoi(vals[1])
	if len != 2 || port == 0 {
		lis.Close()
		return nil, err
	}

	server := &EventServer{
		rpc:   grpc.NewServer(),
		Port:  uint32(port),
		Event: make(chan string),
	}

	pb.RegisterEventServiceServer(server.rpc, server)

	go func() {
		if err = server.rpc.Serve(lis); err != nil {
			return
		}
	}()

	return server, nil
}