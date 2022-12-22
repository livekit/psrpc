package psrpc

import (
	"fmt"

	"github.com/lithammer/shortuuid/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func newRequestID() string {
	return "REQ_" + shortuuid.New()[:12]
}

func getRPCChannel(serviceName, rpc, topic string) string {
	if topic != "" {
		return fmt.Sprintf("%s|%s|%s|REQ", serviceName, rpc, topic)
	} else {
		return fmt.Sprintf("%s|%s|REQ", serviceName, rpc)
	}
}

func getHandlerKey(rpc, topic string) string {
	if topic != "" {
		return fmt.Sprintf("%s|%s", rpc, topic)
	} else {
		return rpc
	}
}

func getResponseChannel(serviceName, clientID string) string {
	return fmt.Sprintf("%s|%s|RES", serviceName, clientID)
}

func getClaimRequestChannel(serviceName, clientID string) string {
	return fmt.Sprintf("%s|%s|CLAIM", serviceName, clientID)
}

func getClaimResponseChannel(serviceName, rpc, topic string) string {
	if topic != "" {
		return fmt.Sprintf("%s|%s|%s|RCLAIM", serviceName, rpc, topic)
	} else {
		return fmt.Sprintf("%s|%s|RCLAIM", serviceName, rpc)
	}
}

func serialize(msg proto.Message) ([]byte, error) {
	a, err := anypb.New(msg)
	if err != nil {
		return nil, err
	}

	b, err := proto.Marshal(a)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func deserialize(b []byte) (proto.Message, error) {
	a := &anypb.Any{}
	err := proto.Unmarshal(b, a)
	if err != nil {
		return nil, err
	}

	return a.UnmarshalNew()
}
