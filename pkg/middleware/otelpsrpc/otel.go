package otelpsrpc

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/pkg/metadata"
	"github.com/livekit/psrpc/version"
)

type Config struct {
	TracerProvider    trace.TracerProvider
	TextMapPropagator propagation.TextMapPropagator
}

func (c *Config) defaults() {
	if c.TracerProvider == nil {
		c.TracerProvider = otel.GetTracerProvider()
	}
	if c.TextMapPropagator == nil {
		c.TextMapPropagator = propagation.TraceContext{}
	}
}

func (c *Config) getTracer() trace.Tracer {
	return c.TracerProvider.Tracer(
		"github.com/livekit/psrpc",
		trace.WithInstrumentationVersion(version.Version),
	)
}

func ClientOptions(c Config) []psrpc.ClientOption {
	c.defaults()
	tracer := c.getTracer()
	return []psrpc.ClientOption{
		psrpc.WithClientRPCInterceptors(func(info psrpc.RPCInfo, next psrpc.ClientRPCHandler) psrpc.ClientRPCHandler {
			return func(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) (proto.Message, error) {
				ctx, span := tracer.Start(ctx, "Sent."+info.Service+"."+info.Method,
					trace.WithSpanKind(trace.SpanKindClient),
				)
				defer span.End()

				m := make(map[string]string)
				c.TextMapPropagator.Inject(ctx, propagation.MapCarrier(m))

				kvs := make([]string, 0, 2*len(m))
				for k, v := range m {
					kvs = append(kvs, k, v)
				}
				ctx = metadata.AppendMetadataToOutgoingContext(ctx, kvs...)

				span.AddEvent("Outbound message")
				resp, err := next(ctx, req)
				span.AddEvent("Inbound message")
				setSpanError(span, err)

				return resp, err
			}
		}),
	}
}

func ServerOptions(c Config) []psrpc.ServerOption {
	c.defaults()
	tracer := c.getTracer()
	return []psrpc.ServerOption{
		psrpc.WithServerRPCInterceptors(func(ctx context.Context, req proto.Message, info psrpc.RPCInfo, handler psrpc.ServerRPCHandler) (proto.Message, error) {
			var m map[string]string
			if h := metadata.IncomingHeader(ctx); h != nil {
				m = h.Metadata
			}
			ctx = c.TextMapPropagator.Extract(ctx, propagation.MapCarrier(m))

			ctx, span := tracer.Start(ctx, "Recv."+info.Service+"."+info.Method,
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()

			span.AddEvent("Outbound message")
			resp, err := handler(ctx, req)
			span.AddEvent("Inbound message")
			setSpanError(span, err)

			return resp, err
		}),
	}
}

func setSpanError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	if e := psrpc.Error(nil); errors.As(err, &e) {
		span.SetAttributes(StatusCodeKey.String(string(e.Code())))
	}
	if st, ok := status.FromError(err); ok {
		span.SetAttributes(semconv.RPCGRPCStatusCodeKey.Int(int(st.Code())))
	}
}

const (
	StatusCodeKey = attribute.Key("rpc.psrpc.status_code")
)
