// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bus

import (
	"context"

	"google.golang.org/protobuf/proto"
)

const (
	DefaultChannelSize          = 100
	DefaultCompressionThreshold = 1024
)

// One destination in three forms. A transport routes on whichever it supports
// and ignores the rest.
type Channel struct {
	// Flat '|'-delimited key, for brokers with no subject hierarchy.
	Legacy string
	// Hierarchical subject, e.g. SRV.<service>.<topic>.
	Server string
	// Demultiplexes several logical channels sharing one Server subject. Carried
	// in the payload, not alongside it.
	Local string
}

// Transport is the seam a message broker implements. Payloads are opaque:
// encoding, compression and the decompression cap belong to the MessageBus
// wrapping it, so an implementation must not interpret them.
type Transport interface {
	Publish(ctx context.Context, channel Channel, payload []byte) error
	Subscribe(ctx context.Context, channel Channel, channelSize int) (Reader, error)
	SubscribeQueue(ctx context.Context, channel Channel, channelSize int) (Reader, error)
}

// InboxTransport is a Transport whose broker takes wildcard subscriptions and
// reply subjects, and reports to a message's reply subject when nothing
// received the message.
type InboxTransport interface {
	Transport
	// Publishes with reply as the message's reply subject.
	PublishWithReply(ctx context.Context, channel, reply Channel, payload []byte) error
	// Subscribes to a wildcard channel. A message that nothing received, whose
	// reply subject the channel matches, is read as an empty payload.
	SubscribeInbox(ctx context.Context, channel Channel, channelSize int) (InboxReader, error)
}

type Reader interface {
	// Reports false once the reader is closed.
	Read() ([]byte, bool)
	Close() error
}

// InboxReader also reports the subject each payload was delivered on.
type InboxReader interface {
	Reader
	ReadInbox() ([]byte, string, bool)
}

type MessageBus interface {
	Publish(ctx context.Context, channel Channel, msg proto.Message) error
	Subscribe(ctx context.Context, channel Channel, channelSize int) (Reader, error)
	SubscribeQueue(ctx context.Context, channel Channel, channelSize int) (Reader, error)
	// Caps an inbound payload after decompression. Zero is unlimited.
	MaxDecompressedSize() int
}

// InboxBus is a MessageBus over an InboxTransport. New returns one whenever its
// transport is an InboxTransport, so a type assertion finds it.
type InboxBus interface {
	MessageBus
	PublishWithReply(ctx context.Context, channel, reply Channel, msg proto.Message) error
	SubscribeInbox(ctx context.Context, channel Channel, channelSize int) (InboxReader, error)
}

func New(t Transport, opts ...BusOption) MessageBus {
	o := getBusOpts(opts...)
	b := &messageBus{
		Transport: t,
		c:         newCompressor(o.Compression),
		maxSize:   o.Compression.MaxDecompressedSize,
	}
	if it, ok := t.(InboxTransport); ok {
		return &inboxBus{messageBus: b, it: it}
	}
	return b
}

type messageBus struct {
	Transport
	c       *compressor
	maxSize int
}

func (b *messageBus) Publish(ctx context.Context, channel Channel, msg proto.Message) error {
	p, err := serialize(msg, channel.Local, b.c)
	if err != nil {
		return err
	}
	return b.Transport.Publish(ctx, channel, p)
}

func (b *messageBus) MaxDecompressedSize() int {
	return b.maxSize
}

type inboxBus struct {
	*messageBus
	it InboxTransport
}

func (b *inboxBus) PublishWithReply(ctx context.Context, channel, reply Channel, msg proto.Message) error {
	p, err := serialize(msg, channel.Local, b.c)
	if err != nil {
		return err
	}
	return b.it.PublishWithReply(ctx, channel, reply, p)
}

func (b *inboxBus) SubscribeInbox(ctx context.Context, channel Channel, channelSize int) (InboxReader, error) {
	return b.it.SubscribeInbox(ctx, channel, channelSize)
}

func Subscribe[MessageType proto.Message](
	ctx context.Context,
	bus MessageBus,
	channel Channel,
	channelSize int,
) (Subscription[MessageType], error) {

	sub, err := bus.Subscribe(ctx, channel, channelSize)
	if err != nil {
		return nil, err
	}

	return newSubscription[MessageType](sub, channelSize, bus.MaxDecompressedSize()), nil
}

// SubscribeInbox is Subscribe for an InboxBus's wildcard channels. unanswered,
// if set, stands in a message for each report that nothing received a message
// replying to the subject it is given.
func SubscribeInbox[MessageType proto.Message](
	ctx context.Context,
	bus InboxBus,
	channel Channel,
	channelSize int,
	unanswered func(subject string) MessageType,
) (Subscription[MessageType], error) {

	sub, err := bus.SubscribeInbox(ctx, channel, channelSize)
	if err != nil {
		return nil, err
	}

	return startSubscription(sub, channelSize, bus.MaxDecompressedSize(), unanswered), nil
}

func SubscribeQueue[MessageType proto.Message](
	ctx context.Context,
	bus MessageBus,
	channel Channel,
	channelSize int,
) (Subscription[MessageType], error) {

	sub, err := bus.SubscribeQueue(ctx, channel, channelSize)
	if err != nil {
		return nil, err
	}

	return newSubscription[MessageType](sub, channelSize, bus.MaxDecompressedSize()), nil
}
