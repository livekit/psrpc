package psrpc

import (
	"fmt"
	"strings"

	"github.com/lithammer/shortuuid/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func newRequestID() string {
	return "REQ_" + shortuuid.New()[:12]
}

func newStreamID() string {
	return "STR_" + shortuuid.New()[:12]
}

func getRPCChannel(serviceName, rpc string, topic []string) string {
	if len(topic) > 0 {
		return fmt.Sprintf("%s|%s|%s|REQ", serviceName, rpc, strings.Join(topic, "|"))
	} else {
		return fmt.Sprintf("%s|%s|REQ", serviceName, rpc)
	}
}

func getHandlerKey(rpc string, topic []string) string {
	if len(topic) > 0 {
		return fmt.Sprintf("%s|%s", rpc, strings.Join(topic, "|"))
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

func getClaimResponseChannel(serviceName, rpc string, topic []string) string {
	if len(topic) > 0 {
		return fmt.Sprintf("%s|%s|%s|RCLAIM", serviceName, rpc, strings.Join(topic, "|"))
	} else {
		return fmt.Sprintf("%s|%s|RCLAIM", serviceName, rpc)
	}
}

func getStreamChannel(serviceName, nodeID string) string {
	return fmt.Sprintf("%s|%s|STR", serviceName, nodeID)
}

func getStreamServerChannel(serviceName, rpc string, topic []string) string {
	if len(topic) > 0 {
		return fmt.Sprintf("%s|%s|%s|STR", serviceName, rpc, strings.Join(topic, "|"))
	} else {
		return fmt.Sprintf("%s|%s|STR", serviceName, rpc)
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
