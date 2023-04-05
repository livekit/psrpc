package middleware

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
)

type MetricRole int

const (
	_ MetricRole = iota
	ClientRole
	ServerRole
)

func (r MetricRole) String() string {
	switch r {
	case ClientRole:
		return "client"
	case ServerRole:
		return "server"
	default:
		return "invalid"
	}
}

type MetricsObserver interface {
	OnUnaryRequest(role MetricRole, info psrpc.RPCInfo, duration time.Duration, err error)
	OnMultiRequest(role MetricRole, info psrpc.RPCInfo, duration time.Duration, responseCount int, errorCount int)
	OnStreamSend(role MetricRole, info psrpc.RPCInfo, duration time.Duration, err error)
	OnStreamRecv(role MetricRole, info psrpc.RPCInfo, err error)
	OnStreamOpen(role MetricRole, info psrpc.RPCInfo)
	OnStreamClose(role MetricRole, info psrpc.RPCInfo)
}

func WithClientMetrics(observer MetricsObserver) psrpc.ClientOption {
	return psrpc.WithClientOptions(
		psrpc.WithClientRPCInterceptors(newRPCMetricsInterceptorFactory(observer)),
		psrpc.WithClientMultiRPCInterceptors(newMultiRPCMetricsInterceptorFactory(observer)),
		psrpc.WithClientStreamInterceptors(newStreamMetricsInterceptorFactory(observer, ClientRole)),
	)
}

func WithServerMetrics(observer MetricsObserver) psrpc.ServerOption {
	return psrpc.WithServerOptions(
		psrpc.WithServerInterceptors(newRPCMetricsInterceptor(observer)),
		psrpc.WithServerStreamInterceptors(newStreamMetricsInterceptorFactory(observer, ServerRole)),
	)
}

func newRPCMetricsInterceptorFactory(observer MetricsObserver) psrpc.RPCInterceptorFactory {
	return func(info psrpc.RPCInfo, next psrpc.RPCInterceptor) psrpc.RPCInterceptor {
		return func(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) (res proto.Message, err error) {
			start := time.Now()
			defer func() { observer.OnUnaryRequest(ClientRole, info, time.Since(start), err) }()
			return next(ctx, req, opts...)
		}
	}
}

func newRPCMetricsInterceptor(observer MetricsObserver) psrpc.ServerInterceptor {
	return func(ctx context.Context, req proto.Message, info psrpc.RPCInfo, handler psrpc.Handler) (resp proto.Message, err error) {
		start := time.Now()
		defer func() {
			if info.Multi {
				var responseCount, errorCount int
				if err == nil {
					responseCount++
				} else {
					errorCount++
				}
				observer.OnMultiRequest(ServerRole, info, time.Since(start), responseCount, errorCount)
			} else {
				observer.OnUnaryRequest(ServerRole, info, time.Since(start), err)
			}
		}()
		return handler(ctx, req)
	}
}

func newStreamMetricsInterceptorFactory(observer MetricsObserver, role MetricRole) psrpc.StreamInterceptorFactory {
	return func(info psrpc.RPCInfo, next psrpc.StreamInterceptor) psrpc.StreamInterceptor {
		observer.OnStreamOpen(role, info)
		return &streamMetricsInterceptor{
			StreamInterceptor: next,
			observer:          observer,
			role:              role,
			info:              info,
		}
	}
}

type streamMetricsInterceptor struct {
	psrpc.StreamInterceptor
	observer MetricsObserver
	role     MetricRole
	info     psrpc.RPCInfo
}

func (s *streamMetricsInterceptor) Recv(msg proto.Message) (err error) {
	s.observer.OnStreamRecv(s.role, s.info, err)
	return s.StreamInterceptor.Recv(msg)
}

func (s *streamMetricsInterceptor) Send(msg proto.Message, opts ...psrpc.StreamOption) (err error) {
	start := time.Now()
	defer func() { s.observer.OnStreamSend(s.role, s.info, time.Since(start), err) }()
	return s.StreamInterceptor.Send(msg, opts...)
}

func (s *streamMetricsInterceptor) Close(cause error) error {
	s.observer.OnStreamClose(s.role, s.info)
	return s.StreamInterceptor.Close(cause)
}

func newMultiRPCMetricsInterceptorFactory(observer MetricsObserver) psrpc.MultiRPCInterceptorFactory {
	return func(info psrpc.RPCInfo, next psrpc.MultiRPCInterceptor) psrpc.MultiRPCInterceptor {
		return &multiRPCMetricsInterceptor{
			MultiRPCInterceptor: next,
			observer:            observer,
			start:               time.Now(),
			info:                info,
		}
	}
}

type multiRPCMetricsInterceptor struct {
	psrpc.MultiRPCInterceptor
	observer      MetricsObserver
	start         time.Time
	info          psrpc.RPCInfo
	responseCount int
	errorCount    int
}

func (r *multiRPCMetricsInterceptor) Send(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) error {
	r.start = time.Now()
	return r.MultiRPCInterceptor.Send(ctx, req, opts...)
}

func (r *multiRPCMetricsInterceptor) Recv(msg proto.Message, err error) {
	if err == nil {
		r.responseCount++
	} else {
		r.errorCount++
	}
	r.MultiRPCInterceptor.Recv(msg, err)
}

func (r *multiRPCMetricsInterceptor) Close() {
	r.observer.OnMultiRequest(ClientRole, r.info, time.Since(r.start), r.responseCount, r.errorCount)
	r.MultiRPCInterceptor.Close()
}
