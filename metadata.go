package psrpc

import (
	"context"
	"time"

	"golang.org/x/exp/maps"
)

type Metadata map[string]string

type Header struct {
	RemoteID string
	SentAt   time.Time
	Metadata Metadata
}

type ctxMD struct {
	md    Metadata
	added [][]string
}

type headerKey struct{}
type metadataKey struct{}

func NewContextWithIncomingHeader(ctx context.Context, head *Header) context.Context {
	return context.WithValue(ctx, headerKey{}, head)
}

func IncomingHeader(ctx context.Context) *Header {
	head, ok := ctx.Value(headerKey{}).(*Header)
	if !ok {
		return nil
	}
	return &Header{
		RemoteID: head.RemoteID,
		SentAt:   head.SentAt,
		Metadata: maps.Clone(head.Metadata),
	}
}

func NewContextWithOutgoingMetadata(ctx context.Context, md Metadata) context.Context {
	return context.WithValue(ctx, metadataKey{}, ctxMD{md: md})
}

func AppendMetadataToOutgoingContext(ctx context.Context, kv ...string) context.Context {
	md, ok := ctx.Value(metadataKey{}).(ctxMD)
	if !ok || md.md == nil {
		md = ctxMD{md: Metadata{}}
	}
	added := make([][]string, len(md.added)+1)
	copy(added, md.added)
	added[len(added)-1] = make([]string, len(kv))
	copy(added[len(added)-1], kv)
	return context.WithValue(ctx, metadataKey{}, ctxMD{md.md, added})
}

func OutgoingContextMetadata(ctx context.Context) Metadata {
	md, ok := ctx.Value(metadataKey{}).(ctxMD)
	if !ok {
		return nil
	}
	clone := maps.Clone(md.md)
	for _, a := range md.added {
		for i := 1; i < len(a); i += 2 {
			clone[a[i-1]] = a[i]
		}
	}
	return clone
}
