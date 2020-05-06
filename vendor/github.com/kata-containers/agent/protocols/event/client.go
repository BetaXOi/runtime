// Copyright 2019 ZTE Inc.
//
// SPDX-License-Identifier: Apache-2.0
//
// gRPC agent event client wrapper

package event

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/mdlayher/vsock"
	opentracing "github.com/opentracing/opentracing-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcStatus "google.golang.org/grpc/status"

	agentgrpc "github.com/kata-containers/agent/protocols/grpc"
)

const (
	UnixSocketScheme  = "unix"
	VSockSocketScheme = "vsock"
	HybridVSockScheme = "hvsock"
)

var defaultDialTimeout = 3 * time.Second

var hybridVSockPort uint32

// EventClient is an gRPC client connection wrapper for agentgrpc.EventServiceClient
type EventClient struct {
	agentgrpc.EventServiceClient
	conn *grpc.ClientConn
}

type dialer func(string, time.Duration) (net.Conn, error)

// NewEventClient creates a new gRPC client and handles both unix and vsock addresses.
//
// Supported sock address formats are:
//   - unix://<unix socket path>
//   - vsock://<cid>:<port>
//   - <unix socket path>
//   - hvsock://<path>:<port>. Firecracker implements the virtio-vsock device
//     model, and mediates communication between AF_UNIX sockets (on the host end)
//     and AF_VSOCK sockets (on the guest end).
func NewEventClient(ctx context.Context, sock string) (*EventClient, error) {
	grpcAddr, parsedAddr, err := parse(sock)
	if err != nil {
		return nil, err
	}
	dialOpts := []grpc.DialOption{grpc.WithInsecure(), grpc.WithBlock()}
	dialOpts = append(dialOpts, grpc.WithDialer(eventDialer(parsedAddr)))

	var tracer opentracing.Tracer

	span := opentracing.SpanFromContext(ctx)

	// If the context contains a trace span, trace all client comms
	if span != nil {
		tracer = span.Tracer()

		dialOpts = append(dialOpts,
			grpc.WithUnaryInterceptor(otgrpc.OpenTracingClientInterceptor(tracer)))
		dialOpts = append(dialOpts,
			grpc.WithStreamInterceptor(otgrpc.OpenTracingStreamClientInterceptor(tracer)))
	}

	ctx, cancel := context.WithTimeout(ctx, defaultDialTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, grpcAddr, dialOpts...)
	if err != nil {
		return nil, err
	}

	return &EventClient{
		EventServiceClient: agentgrpc.NewEventServiceClient(conn),
		conn:                conn,
	}, nil
}

// Close an existing connection to the gRPC server.
func (c *EventClient) Close() error {
	return c.conn.Close()
}

// vsock scheme is self-defined to be kept from being parsed by grpc.
// Any format starting with "scheme://" will be parsed by grpc and we lose
// all address information because vsock scheme is not supported by grpc.
// Therefore we use the format vsock:<cid>:<port> for vsock address.
//
// See https://github.com/grpc/grpc/blob/master/doc/naming.md
//
// In the long term, we should patch grpc to support vsock scheme and also
// upstream the timed vsock dialer.
func parse(sock string) (string, *url.URL, error) {
	addr, err := url.Parse(sock)
	if err != nil {
		return "", nil, err
	}

	var grpcAddr string
	// validate more
	switch addr.Scheme {
	case VSockSocketScheme:
		if addr.Hostname() == "" || addr.Port() == "" || addr.Path != "" {
			return "", nil, grpcStatus.Errorf(codes.InvalidArgument, "Invalid vsock scheme: %s", sock)
		}
		if _, err := strconv.ParseUint(addr.Hostname(), 10, 32); err != nil {
			return "", nil, grpcStatus.Errorf(codes.InvalidArgument, "Invalid vsock cid: %s", sock)
		}
		if _, err := strconv.ParseUint(addr.Port(), 10, 32); err != nil {
			return "", nil, grpcStatus.Errorf(codes.InvalidArgument, "Invalid vsock port: %s", sock)
		}
		grpcAddr = VSockSocketScheme + ":" + addr.Host
	case UnixSocketScheme:
		fallthrough
	case "":
		if (addr.Host == "" && addr.Path == "") || addr.Port() != "" {
			return "", nil, grpcStatus.Errorf(codes.InvalidArgument, "Invalid unix scheme: %s", sock)
		}
		if addr.Host == "" {
			grpcAddr = UnixSocketScheme + ":///" + addr.Path
		} else {
			grpcAddr = UnixSocketScheme + ":///" + addr.Host + "/" + addr.Path
		}
	case HybridVSockScheme:
		if addr.Path == "" {
			return "", nil, grpcStatus.Errorf(codes.InvalidArgument, "Invalid hybrid vsock scheme: %s", sock)
		}
		hvsocket := strings.Split(addr.Path, ":")
		if len(hvsocket) != 2 {
			return "", nil, grpcStatus.Errorf(codes.InvalidArgument, "Invalid hybrid vsock scheme: %s", sock)
		}
		// Save port since agent dialer not sent the port to the hybridVSock dialer
		var port uint64
		if port, err = strconv.ParseUint(hvsocket[1], 10, 32); err != nil {
			return "", nil, grpcStatus.Errorf(codes.InvalidArgument, "Invalid hybrid vsock port %s: %v", sock, err)
		}
		hybridVSockPort = uint32(port)
		grpcAddr = HybridVSockScheme + ":" + hvsocket[0]
	default:
		return "", nil, grpcStatus.Errorf(codes.InvalidArgument, "Invalid scheme: %s", sock)
	}

	return grpcAddr, addr, nil
}

func eventDialer(addr *url.URL) dialer {
	var d dialer
	switch addr.Scheme {
	case VSockSocketScheme:
		d = vsockDialer
	case HybridVSockScheme:
		d = HybridVSockDialer
	case UnixSocketScheme:
		fallthrough
	default:
		d = unixDialer
	}

	return d
}

func unixDialer(sock string, timeout time.Duration) (net.Conn, error) {
	if strings.HasPrefix(sock, "unix:") {
		sock = strings.Trim(sock, "unix:")
	}

	dialFunc := func() (net.Conn, error) {
		return net.DialTimeout("unix", sock, timeout)
	}

	timeoutErr := grpcStatus.Errorf(codes.DeadlineExceeded, "timed out connecting to unix socket %s", sock)
	return commonDialer(timeout, dialFunc, timeoutErr)
}

func parseGrpcVsockAddr(sock string) (uint32, uint32, error) {
	sp := strings.Split(sock, ":")
	if len(sp) != 3 {
		return 0, 0, grpcStatus.Errorf(codes.InvalidArgument, "Invalid vsock address: %s", sock)
	}
	if sp[0] != VSockSocketScheme {
		return 0, 0, grpcStatus.Errorf(codes.InvalidArgument, "Invalid vsock URL scheme: %s", sp[0])
	}

	cid, err := strconv.ParseUint(sp[1], 10, 32)
	if err != nil {
		return 0, 0, grpcStatus.Errorf(codes.InvalidArgument, "Invalid vsock cid: %s", sp[1])
	}
	port, err := strconv.ParseUint(sp[2], 10, 32)
	if err != nil {
		return 0, 0, grpcStatus.Errorf(codes.InvalidArgument, "Invalid vsock port: %s", sp[2])
	}

	return uint32(cid), uint32(port), nil
}

func parseGrpcHybridVSockAddr(sock string) (string, uint32, error) {
	sp := strings.Split(sock, ":")
	// scheme and host are required
	if len(sp) < 2 {
		return "", 0, grpcStatus.Errorf(codes.InvalidArgument, "Invalid hybrid vsock address: %s", sock)
	}
	if sp[0] != HybridVSockScheme {
		return "", 0, grpcStatus.Errorf(codes.InvalidArgument, "Invalid hybrid vsock URL scheme: %s", sock)
	}

	port := uint32(0)
	// the third is the port
	if len(sp) == 3 {
		p, err := strconv.ParseUint(sp[2], 10, 32)
		if err == nil {
			port = uint32(p)
		}
	}

	return sp[1], port, nil
}

// This would bypass the grpc dialer backoff strategy and handle dial timeout
// internally. Because we do not have a large number of concurrent dialers,
// it is not reasonable to have such aggressive backoffs which would kill kata
// containers boot up speed. For more information, see
// https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md
func commonDialer(timeout time.Duration, dialFunc func() (net.Conn, error), timeoutErrMsg error) (net.Conn, error) {
	t := time.NewTimer(timeout)
	cancel := make(chan bool)
	ch := make(chan net.Conn)
	go func() {
		for {
			select {
			case <-cancel:
				// canceled or channel closed
				return
			default:
			}

			conn, err := dialFunc()
			if err == nil {
				// Send conn back iff timer is not fired
				// Otherwise there might be no one left reading it
				if t.Stop() {
					ch <- conn
				} else {
					conn.Close()
				}
				return
			}
		}
	}()

	var conn net.Conn
	var ok bool
	select {
	case conn, ok = <-ch:
		if !ok {
			return nil, timeoutErrMsg
		}
	case <-t.C:
		cancel <- true
		return nil, timeoutErrMsg
	}

	return conn, nil
}

func vsockDialer(sock string, timeout time.Duration) (net.Conn, error) {
	cid, port, err := parseGrpcVsockAddr(sock)
	if err != nil {
		return nil, err
	}

	dialFunc := func() (net.Conn, error) {
		return vsock.Dial(cid, port)
	}

	timeoutErr := grpcStatus.Errorf(codes.DeadlineExceeded, "timed out connecting to vsock %d:%d", cid, port)

	return commonDialer(timeout, dialFunc, timeoutErr)
}

// HybridVSockDialer dials to a hybrid virtio socket
func HybridVSockDialer(sock string, timeout time.Duration) (net.Conn, error) {
	udsPath, port, err := parseGrpcHybridVSockAddr(sock)
	if err != nil {
		return nil, err
	}

	dialFunc := func() (net.Conn, error) {
		conn, err := net.DialTimeout("unix", udsPath, timeout)
		if err != nil {
			return nil, err
		}

		if port == 0 {
			// use the port read at parse()
			port = hybridVSockPort
		}

		// Once the connection is opened, the following command MUST BE sent,
		// the hypervisor needs to know the port number where the agent is listening in order to
		// create the connection
		if _, err = conn.Write([]byte(fmt.Sprintf("CONNECT %d\n", port))); err != nil {
			conn.Close()
			return nil, err
		}

		// Read EOT (End of transmission) byte
		eot := make([]byte, 32)
		if _, err = conn.Read(eot); err != nil {
			// Just close the connection, gRPC will dial again
			// without errors
			conn.Close()
		}

		return conn, nil
	}

	timeoutErr := grpcStatus.Errorf(codes.DeadlineExceeded, "timed out connecting to hybrid vsocket %s", sock)
	return commonDialer(timeout, dialFunc, timeoutErr)
}
