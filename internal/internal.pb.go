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

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.4
// 	protoc        v4.23.4
// source: internal.proto

package internal

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	anypb "google.golang.org/protobuf/types/known/anypb"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type Msg struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	TypeUrl       string                 `protobuf:"bytes,1,opt,name=type_url,json=typeUrl,proto3" json:"type_url,omitempty"`
	Value         []byte                 `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
	Channel       string                 `protobuf:"bytes,3,opt,name=channel,proto3" json:"channel,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Msg) Reset() {
	*x = Msg{}
	mi := &file_internal_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Msg) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Msg) ProtoMessage() {}

func (x *Msg) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Msg.ProtoReflect.Descriptor instead.
func (*Msg) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{0}
}

func (x *Msg) GetTypeUrl() string {
	if x != nil {
		return x.TypeUrl
	}
	return ""
}

func (x *Msg) GetValue() []byte {
	if x != nil {
		return x.Value
	}
	return nil
}

func (x *Msg) GetChannel() string {
	if x != nil {
		return x.Channel
	}
	return ""
}

type Channel struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Channel       string                 `protobuf:"bytes,3,opt,name=channel,proto3" json:"channel,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Channel) Reset() {
	*x = Channel{}
	mi := &file_internal_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Channel) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Channel) ProtoMessage() {}

func (x *Channel) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Channel.ProtoReflect.Descriptor instead.
func (*Channel) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{1}
}

func (x *Channel) GetChannel() string {
	if x != nil {
		return x.Channel
	}
	return ""
}

type Request struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	RequestId     string                 `protobuf:"bytes,1,opt,name=request_id,json=requestId,proto3" json:"request_id,omitempty"`
	ClientId      string                 `protobuf:"bytes,2,opt,name=client_id,json=clientId,proto3" json:"client_id,omitempty"`
	SentAt        int64                  `protobuf:"varint,3,opt,name=sent_at,json=sentAt,proto3" json:"sent_at,omitempty"`
	Expiry        int64                  `protobuf:"varint,4,opt,name=expiry,proto3" json:"expiry,omitempty"`
	Multi         bool                   `protobuf:"varint,5,opt,name=multi,proto3" json:"multi,omitempty"`
	Request       *anypb.Any             `protobuf:"bytes,6,opt,name=request,proto3" json:"request,omitempty"`
	Metadata      map[string]string      `protobuf:"bytes,7,rep,name=metadata,proto3" json:"metadata,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
	RawRequest    []byte                 `protobuf:"bytes,8,opt,name=raw_request,json=rawRequest,proto3" json:"raw_request,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Request) Reset() {
	*x = Request{}
	mi := &file_internal_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Request) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Request) ProtoMessage() {}

func (x *Request) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Request.ProtoReflect.Descriptor instead.
func (*Request) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{2}
}

func (x *Request) GetRequestId() string {
	if x != nil {
		return x.RequestId
	}
	return ""
}

func (x *Request) GetClientId() string {
	if x != nil {
		return x.ClientId
	}
	return ""
}

func (x *Request) GetSentAt() int64 {
	if x != nil {
		return x.SentAt
	}
	return 0
}

func (x *Request) GetExpiry() int64 {
	if x != nil {
		return x.Expiry
	}
	return 0
}

func (x *Request) GetMulti() bool {
	if x != nil {
		return x.Multi
	}
	return false
}

func (x *Request) GetRequest() *anypb.Any {
	if x != nil {
		return x.Request
	}
	return nil
}

func (x *Request) GetMetadata() map[string]string {
	if x != nil {
		return x.Metadata
	}
	return nil
}

func (x *Request) GetRawRequest() []byte {
	if x != nil {
		return x.RawRequest
	}
	return nil
}

type Response struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	RequestId     string                 `protobuf:"bytes,1,opt,name=request_id,json=requestId,proto3" json:"request_id,omitempty"`
	ServerId      string                 `protobuf:"bytes,2,opt,name=server_id,json=serverId,proto3" json:"server_id,omitempty"`
	SentAt        int64                  `protobuf:"varint,3,opt,name=sent_at,json=sentAt,proto3" json:"sent_at,omitempty"`
	Response      *anypb.Any             `protobuf:"bytes,4,opt,name=response,proto3" json:"response,omitempty"`
	Error         string                 `protobuf:"bytes,5,opt,name=error,proto3" json:"error,omitempty"`
	Code          string                 `protobuf:"bytes,6,opt,name=code,proto3" json:"code,omitempty"`
	RawResponse   []byte                 `protobuf:"bytes,7,opt,name=raw_response,json=rawResponse,proto3" json:"raw_response,omitempty"`
	ErrorDetails  []*anypb.Any           `protobuf:"bytes,8,rep,name=error_details,json=errorDetails,proto3" json:"error_details,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Response) Reset() {
	*x = Response{}
	mi := &file_internal_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Response) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Response) ProtoMessage() {}

func (x *Response) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Response.ProtoReflect.Descriptor instead.
func (*Response) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{3}
}

func (x *Response) GetRequestId() string {
	if x != nil {
		return x.RequestId
	}
	return ""
}

func (x *Response) GetServerId() string {
	if x != nil {
		return x.ServerId
	}
	return ""
}

func (x *Response) GetSentAt() int64 {
	if x != nil {
		return x.SentAt
	}
	return 0
}

func (x *Response) GetResponse() *anypb.Any {
	if x != nil {
		return x.Response
	}
	return nil
}

func (x *Response) GetError() string {
	if x != nil {
		return x.Error
	}
	return ""
}

func (x *Response) GetCode() string {
	if x != nil {
		return x.Code
	}
	return ""
}

func (x *Response) GetRawResponse() []byte {
	if x != nil {
		return x.RawResponse
	}
	return nil
}

func (x *Response) GetErrorDetails() []*anypb.Any {
	if x != nil {
		return x.ErrorDetails
	}
	return nil
}

type ClaimRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	RequestId     string                 `protobuf:"bytes,1,opt,name=request_id,json=requestId,proto3" json:"request_id,omitempty"`
	ServerId      string                 `protobuf:"bytes,2,opt,name=server_id,json=serverId,proto3" json:"server_id,omitempty"`
	Affinity      float32                `protobuf:"fixed32,3,opt,name=affinity,proto3" json:"affinity,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ClaimRequest) Reset() {
	*x = ClaimRequest{}
	mi := &file_internal_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ClaimRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ClaimRequest) ProtoMessage() {}

func (x *ClaimRequest) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ClaimRequest.ProtoReflect.Descriptor instead.
func (*ClaimRequest) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{4}
}

func (x *ClaimRequest) GetRequestId() string {
	if x != nil {
		return x.RequestId
	}
	return ""
}

func (x *ClaimRequest) GetServerId() string {
	if x != nil {
		return x.ServerId
	}
	return ""
}

func (x *ClaimRequest) GetAffinity() float32 {
	if x != nil {
		return x.Affinity
	}
	return 0
}

type ClaimResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	RequestId     string                 `protobuf:"bytes,1,opt,name=request_id,json=requestId,proto3" json:"request_id,omitempty"`
	ServerId      string                 `protobuf:"bytes,2,opt,name=server_id,json=serverId,proto3" json:"server_id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ClaimResponse) Reset() {
	*x = ClaimResponse{}
	mi := &file_internal_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ClaimResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ClaimResponse) ProtoMessage() {}

func (x *ClaimResponse) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ClaimResponse.ProtoReflect.Descriptor instead.
func (*ClaimResponse) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{5}
}

func (x *ClaimResponse) GetRequestId() string {
	if x != nil {
		return x.RequestId
	}
	return ""
}

func (x *ClaimResponse) GetServerId() string {
	if x != nil {
		return x.ServerId
	}
	return ""
}

type Stream struct {
	state     protoimpl.MessageState `protogen:"open.v1"`
	StreamId  string                 `protobuf:"bytes,1,opt,name=stream_id,json=streamId,proto3" json:"stream_id,omitempty"`
	RequestId string                 `protobuf:"bytes,2,opt,name=request_id,json=requestId,proto3" json:"request_id,omitempty"`
	SentAt    int64                  `protobuf:"varint,3,opt,name=sent_at,json=sentAt,proto3" json:"sent_at,omitempty"`
	Expiry    int64                  `protobuf:"varint,4,opt,name=expiry,proto3" json:"expiry,omitempty"`
	// Types that are valid to be assigned to Body:
	//
	//	*Stream_Open
	//	*Stream_Message
	//	*Stream_Ack
	//	*Stream_Close
	Body          isStream_Body `protobuf_oneof:"body"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Stream) Reset() {
	*x = Stream{}
	mi := &file_internal_proto_msgTypes[6]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Stream) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Stream) ProtoMessage() {}

func (x *Stream) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[6]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Stream.ProtoReflect.Descriptor instead.
func (*Stream) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{6}
}

func (x *Stream) GetStreamId() string {
	if x != nil {
		return x.StreamId
	}
	return ""
}

func (x *Stream) GetRequestId() string {
	if x != nil {
		return x.RequestId
	}
	return ""
}

func (x *Stream) GetSentAt() int64 {
	if x != nil {
		return x.SentAt
	}
	return 0
}

func (x *Stream) GetExpiry() int64 {
	if x != nil {
		return x.Expiry
	}
	return 0
}

func (x *Stream) GetBody() isStream_Body {
	if x != nil {
		return x.Body
	}
	return nil
}

func (x *Stream) GetOpen() *StreamOpen {
	if x != nil {
		if x, ok := x.Body.(*Stream_Open); ok {
			return x.Open
		}
	}
	return nil
}

func (x *Stream) GetMessage() *StreamMessage {
	if x != nil {
		if x, ok := x.Body.(*Stream_Message); ok {
			return x.Message
		}
	}
	return nil
}

func (x *Stream) GetAck() *StreamAck {
	if x != nil {
		if x, ok := x.Body.(*Stream_Ack); ok {
			return x.Ack
		}
	}
	return nil
}

func (x *Stream) GetClose() *StreamClose {
	if x != nil {
		if x, ok := x.Body.(*Stream_Close); ok {
			return x.Close
		}
	}
	return nil
}

type isStream_Body interface {
	isStream_Body()
}

type Stream_Open struct {
	Open *StreamOpen `protobuf:"bytes,6,opt,name=open,proto3,oneof"`
}

type Stream_Message struct {
	Message *StreamMessage `protobuf:"bytes,7,opt,name=message,proto3,oneof"`
}

type Stream_Ack struct {
	Ack *StreamAck `protobuf:"bytes,8,opt,name=ack,proto3,oneof"`
}

type Stream_Close struct {
	Close *StreamClose `protobuf:"bytes,9,opt,name=close,proto3,oneof"`
}

func (*Stream_Open) isStream_Body() {}

func (*Stream_Message) isStream_Body() {}

func (*Stream_Ack) isStream_Body() {}

func (*Stream_Close) isStream_Body() {}

type StreamOpen struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	NodeId        string                 `protobuf:"bytes,1,opt,name=node_id,json=nodeId,proto3" json:"node_id,omitempty"`
	Metadata      map[string]string      `protobuf:"bytes,7,rep,name=metadata,proto3" json:"metadata,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *StreamOpen) Reset() {
	*x = StreamOpen{}
	mi := &file_internal_proto_msgTypes[7]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *StreamOpen) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StreamOpen) ProtoMessage() {}

func (x *StreamOpen) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[7]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use StreamOpen.ProtoReflect.Descriptor instead.
func (*StreamOpen) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{7}
}

func (x *StreamOpen) GetNodeId() string {
	if x != nil {
		return x.NodeId
	}
	return ""
}

func (x *StreamOpen) GetMetadata() map[string]string {
	if x != nil {
		return x.Metadata
	}
	return nil
}

type StreamMessage struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Message       *anypb.Any             `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
	RawMessage    []byte                 `protobuf:"bytes,2,opt,name=raw_message,json=rawMessage,proto3" json:"raw_message,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *StreamMessage) Reset() {
	*x = StreamMessage{}
	mi := &file_internal_proto_msgTypes[8]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *StreamMessage) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StreamMessage) ProtoMessage() {}

func (x *StreamMessage) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[8]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use StreamMessage.ProtoReflect.Descriptor instead.
func (*StreamMessage) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{8}
}

func (x *StreamMessage) GetMessage() *anypb.Any {
	if x != nil {
		return x.Message
	}
	return nil
}

func (x *StreamMessage) GetRawMessage() []byte {
	if x != nil {
		return x.RawMessage
	}
	return nil
}

type StreamAck struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *StreamAck) Reset() {
	*x = StreamAck{}
	mi := &file_internal_proto_msgTypes[9]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *StreamAck) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StreamAck) ProtoMessage() {}

func (x *StreamAck) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[9]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use StreamAck.ProtoReflect.Descriptor instead.
func (*StreamAck) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{9}
}

type StreamClose struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Error         string                 `protobuf:"bytes,1,opt,name=error,proto3" json:"error,omitempty"`
	Code          string                 `protobuf:"bytes,2,opt,name=code,proto3" json:"code,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *StreamClose) Reset() {
	*x = StreamClose{}
	mi := &file_internal_proto_msgTypes[10]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *StreamClose) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StreamClose) ProtoMessage() {}

func (x *StreamClose) ProtoReflect() protoreflect.Message {
	mi := &file_internal_proto_msgTypes[10]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use StreamClose.ProtoReflect.Descriptor instead.
func (*StreamClose) Descriptor() ([]byte, []int) {
	return file_internal_proto_rawDescGZIP(), []int{10}
}

func (x *StreamClose) GetError() string {
	if x != nil {
		return x.Error
	}
	return ""
}

func (x *StreamClose) GetCode() string {
	if x != nil {
		return x.Code
	}
	return ""
}

var File_internal_proto protoreflect.FileDescriptor

var file_internal_proto_rawDesc = string([]byte{
	0x0a, 0x0e, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x12, 0x08, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x1a, 0x19, 0x67, 0x6f, 0x6f, 0x67,
	0x6c, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2f, 0x61, 0x6e, 0x79, 0x2e,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0x50, 0x0a, 0x03, 0x4d, 0x73, 0x67, 0x12, 0x19, 0x0a, 0x08,
	0x74, 0x79, 0x70, 0x65, 0x5f, 0x75, 0x72, 0x6c, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07,
	0x74, 0x79, 0x70, 0x65, 0x55, 0x72, 0x6c, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65,
	0x18, 0x02, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x12, 0x18, 0x0a,
	0x07, 0x63, 0x68, 0x61, 0x6e, 0x6e, 0x65, 0x6c, 0x18, 0x03, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07,
	0x63, 0x68, 0x61, 0x6e, 0x6e, 0x65, 0x6c, 0x22, 0x23, 0x0a, 0x07, 0x43, 0x68, 0x61, 0x6e, 0x6e,
	0x65, 0x6c, 0x12, 0x18, 0x0a, 0x07, 0x63, 0x68, 0x61, 0x6e, 0x6e, 0x65, 0x6c, 0x18, 0x03, 0x20,
	0x01, 0x28, 0x09, 0x52, 0x07, 0x63, 0x68, 0x61, 0x6e, 0x6e, 0x65, 0x6c, 0x22, 0xd7, 0x02, 0x0a,
	0x07, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x1d, 0x0a, 0x0a, 0x72, 0x65, 0x71, 0x75,
	0x65, 0x73, 0x74, 0x5f, 0x69, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x09, 0x72, 0x65,
	0x71, 0x75, 0x65, 0x73, 0x74, 0x49, 0x64, 0x12, 0x1b, 0x0a, 0x09, 0x63, 0x6c, 0x69, 0x65, 0x6e,
	0x74, 0x5f, 0x69, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x63, 0x6c, 0x69, 0x65,
	0x6e, 0x74, 0x49, 0x64, 0x12, 0x17, 0x0a, 0x07, 0x73, 0x65, 0x6e, 0x74, 0x5f, 0x61, 0x74, 0x18,
	0x03, 0x20, 0x01, 0x28, 0x03, 0x52, 0x06, 0x73, 0x65, 0x6e, 0x74, 0x41, 0x74, 0x12, 0x16, 0x0a,
	0x06, 0x65, 0x78, 0x70, 0x69, 0x72, 0x79, 0x18, 0x04, 0x20, 0x01, 0x28, 0x03, 0x52, 0x06, 0x65,
	0x78, 0x70, 0x69, 0x72, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x6d, 0x75, 0x6c, 0x74, 0x69, 0x18, 0x05,
	0x20, 0x01, 0x28, 0x08, 0x52, 0x05, 0x6d, 0x75, 0x6c, 0x74, 0x69, 0x12, 0x2e, 0x0a, 0x07, 0x72,
	0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x18, 0x06, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e, 0x67,
	0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x41,
	0x6e, 0x79, 0x52, 0x07, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x3b, 0x0a, 0x08, 0x6d,
	0x65, 0x74, 0x61, 0x64, 0x61, 0x74, 0x61, 0x18, 0x07, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x1f, 0x2e,
	0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74,
	0x2e, 0x4d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74, 0x61, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x52, 0x08,
	0x6d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74, 0x61, 0x12, 0x1f, 0x0a, 0x0b, 0x72, 0x61, 0x77, 0x5f,
	0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x18, 0x08, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x0a, 0x72,
	0x61, 0x77, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x3b, 0x0a, 0x0d, 0x4d, 0x65, 0x74,
	0x61, 0x64, 0x61, 0x74, 0x61, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65,
	0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x14, 0x0a, 0x05,
	0x76, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x76, 0x61, 0x6c,
	0x75, 0x65, 0x3a, 0x02, 0x38, 0x01, 0x22, 0x99, 0x02, 0x0a, 0x08, 0x52, 0x65, 0x73, 0x70, 0x6f,
	0x6e, 0x73, 0x65, 0x12, 0x1d, 0x0a, 0x0a, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x5f, 0x69,
	0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x09, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74,
	0x49, 0x64, 0x12, 0x1b, 0x0a, 0x09, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x5f, 0x69, 0x64, 0x18,
	0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x49, 0x64, 0x12,
	0x17, 0x0a, 0x07, 0x73, 0x65, 0x6e, 0x74, 0x5f, 0x61, 0x74, 0x18, 0x03, 0x20, 0x01, 0x28, 0x03,
	0x52, 0x06, 0x73, 0x65, 0x6e, 0x74, 0x41, 0x74, 0x12, 0x30, 0x0a, 0x08, 0x72, 0x65, 0x73, 0x70,
	0x6f, 0x6e, 0x73, 0x65, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e, 0x67, 0x6f, 0x6f,
	0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x41, 0x6e, 0x79,
	0x52, 0x08, 0x72, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x14, 0x0a, 0x05, 0x65, 0x72,
	0x72, 0x6f, 0x72, 0x18, 0x05, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x65, 0x72, 0x72, 0x6f, 0x72,
	0x12, 0x12, 0x0a, 0x04, 0x63, 0x6f, 0x64, 0x65, 0x18, 0x06, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04,
	0x63, 0x6f, 0x64, 0x65, 0x12, 0x21, 0x0a, 0x0c, 0x72, 0x61, 0x77, 0x5f, 0x72, 0x65, 0x73, 0x70,
	0x6f, 0x6e, 0x73, 0x65, 0x18, 0x07, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x0b, 0x72, 0x61, 0x77, 0x52,
	0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x39, 0x0a, 0x0d, 0x65, 0x72, 0x72, 0x6f, 0x72,
	0x5f, 0x64, 0x65, 0x74, 0x61, 0x69, 0x6c, 0x73, 0x18, 0x08, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x14,
	0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66,
	0x2e, 0x41, 0x6e, 0x79, 0x52, 0x0c, 0x65, 0x72, 0x72, 0x6f, 0x72, 0x44, 0x65, 0x74, 0x61, 0x69,
	0x6c, 0x73, 0x22, 0x66, 0x0a, 0x0c, 0x43, 0x6c, 0x61, 0x69, 0x6d, 0x52, 0x65, 0x71, 0x75, 0x65,
	0x73, 0x74, 0x12, 0x1d, 0x0a, 0x0a, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x5f, 0x69, 0x64,
	0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x09, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x49,
	0x64, 0x12, 0x1b, 0x0a, 0x09, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x5f, 0x69, 0x64, 0x18, 0x02,
	0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x49, 0x64, 0x12, 0x1a,
	0x0a, 0x08, 0x61, 0x66, 0x66, 0x69, 0x6e, 0x69, 0x74, 0x79, 0x18, 0x03, 0x20, 0x01, 0x28, 0x02,
	0x52, 0x08, 0x61, 0x66, 0x66, 0x69, 0x6e, 0x69, 0x74, 0x79, 0x22, 0x4b, 0x0a, 0x0d, 0x43, 0x6c,
	0x61, 0x69, 0x6d, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x1d, 0x0a, 0x0a, 0x72,
	0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x5f, 0x69, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x09, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x49, 0x64, 0x12, 0x1b, 0x0a, 0x09, 0x73, 0x65,
	0x72, 0x76, 0x65, 0x72, 0x5f, 0x69, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x73,
	0x65, 0x72, 0x76, 0x65, 0x72, 0x49, 0x64, 0x22, 0xb6, 0x02, 0x0a, 0x06, 0x53, 0x74, 0x72, 0x65,
	0x61, 0x6d, 0x12, 0x1b, 0x0a, 0x09, 0x73, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x5f, 0x69, 0x64, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x73, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x49, 0x64, 0x12,
	0x1d, 0x0a, 0x0a, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x5f, 0x69, 0x64, 0x18, 0x02, 0x20,
	0x01, 0x28, 0x09, 0x52, 0x09, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x49, 0x64, 0x12, 0x17,
	0x0a, 0x07, 0x73, 0x65, 0x6e, 0x74, 0x5f, 0x61, 0x74, 0x18, 0x03, 0x20, 0x01, 0x28, 0x03, 0x52,
	0x06, 0x73, 0x65, 0x6e, 0x74, 0x41, 0x74, 0x12, 0x16, 0x0a, 0x06, 0x65, 0x78, 0x70, 0x69, 0x72,
	0x79, 0x18, 0x04, 0x20, 0x01, 0x28, 0x03, 0x52, 0x06, 0x65, 0x78, 0x70, 0x69, 0x72, 0x79, 0x12,
	0x2a, 0x0a, 0x04, 0x6f, 0x70, 0x65, 0x6e, 0x18, 0x06, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e,
	0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x53, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x4f,
	0x70, 0x65, 0x6e, 0x48, 0x00, 0x52, 0x04, 0x6f, 0x70, 0x65, 0x6e, 0x12, 0x33, 0x0a, 0x07, 0x6d,
	0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x18, 0x07, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x17, 0x2e, 0x69,
	0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x53, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x4d, 0x65,
	0x73, 0x73, 0x61, 0x67, 0x65, 0x48, 0x00, 0x52, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65,
	0x12, 0x27, 0x0a, 0x03, 0x61, 0x63, 0x6b, 0x18, 0x08, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x13, 0x2e,
	0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x53, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x41,
	0x63, 0x6b, 0x48, 0x00, 0x52, 0x03, 0x61, 0x63, 0x6b, 0x12, 0x2d, 0x0a, 0x05, 0x63, 0x6c, 0x6f,
	0x73, 0x65, 0x18, 0x09, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x15, 0x2e, 0x69, 0x6e, 0x74, 0x65, 0x72,
	0x6e, 0x61, 0x6c, 0x2e, 0x53, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x43, 0x6c, 0x6f, 0x73, 0x65, 0x48,
	0x00, 0x52, 0x05, 0x63, 0x6c, 0x6f, 0x73, 0x65, 0x42, 0x06, 0x0a, 0x04, 0x62, 0x6f, 0x64, 0x79,
	0x22, 0xa2, 0x01, 0x0a, 0x0a, 0x53, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x4f, 0x70, 0x65, 0x6e, 0x12,
	0x17, 0x0a, 0x07, 0x6e, 0x6f, 0x64, 0x65, 0x5f, 0x69, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x06, 0x6e, 0x6f, 0x64, 0x65, 0x49, 0x64, 0x12, 0x3e, 0x0a, 0x08, 0x6d, 0x65, 0x74, 0x61,
	0x64, 0x61, 0x74, 0x61, 0x18, 0x07, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x22, 0x2e, 0x69, 0x6e, 0x74,
	0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x53, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x4f, 0x70, 0x65, 0x6e,
	0x2e, 0x4d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74, 0x61, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x52, 0x08,
	0x6d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74, 0x61, 0x1a, 0x3b, 0x0a, 0x0d, 0x4d, 0x65, 0x74, 0x61,
	0x64, 0x61, 0x74, 0x61, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79,
	0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x76,
	0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75,
	0x65, 0x3a, 0x02, 0x38, 0x01, 0x22, 0x60, 0x0a, 0x0d, 0x53, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x4d,
	0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x12, 0x2e, 0x0a, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67,
	0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x41, 0x6e, 0x79, 0x52, 0x07, 0x6d,
	0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x12, 0x1f, 0x0a, 0x0b, 0x72, 0x61, 0x77, 0x5f, 0x6d, 0x65,
	0x73, 0x73, 0x61, 0x67, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x0a, 0x72, 0x61, 0x77,
	0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x22, 0x0b, 0x0a, 0x09, 0x53, 0x74, 0x72, 0x65, 0x61,
	0x6d, 0x41, 0x63, 0x6b, 0x22, 0x37, 0x0a, 0x0b, 0x53, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x43, 0x6c,
	0x6f, 0x73, 0x65, 0x12, 0x14, 0x0a, 0x05, 0x65, 0x72, 0x72, 0x6f, 0x72, 0x18, 0x01, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x05, 0x65, 0x72, 0x72, 0x6f, 0x72, 0x12, 0x12, 0x0a, 0x04, 0x63, 0x6f, 0x64,
	0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x63, 0x6f, 0x64, 0x65, 0x42, 0x23, 0x5a,
	0x21, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x6c, 0x69, 0x76, 0x65,
	0x6b, 0x69, 0x74, 0x2f, 0x70, 0x73, 0x72, 0x70, 0x63, 0x2f, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e,
	0x61, 0x6c, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
})

var (
	file_internal_proto_rawDescOnce sync.Once
	file_internal_proto_rawDescData []byte
)

func file_internal_proto_rawDescGZIP() []byte {
	file_internal_proto_rawDescOnce.Do(func() {
		file_internal_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_internal_proto_rawDesc), len(file_internal_proto_rawDesc)))
	})
	return file_internal_proto_rawDescData
}

var file_internal_proto_msgTypes = make([]protoimpl.MessageInfo, 13)
var file_internal_proto_goTypes = []any{
	(*Msg)(nil),           // 0: internal.Msg
	(*Channel)(nil),       // 1: internal.Channel
	(*Request)(nil),       // 2: internal.Request
	(*Response)(nil),      // 3: internal.Response
	(*ClaimRequest)(nil),  // 4: internal.ClaimRequest
	(*ClaimResponse)(nil), // 5: internal.ClaimResponse
	(*Stream)(nil),        // 6: internal.Stream
	(*StreamOpen)(nil),    // 7: internal.StreamOpen
	(*StreamMessage)(nil), // 8: internal.StreamMessage
	(*StreamAck)(nil),     // 9: internal.StreamAck
	(*StreamClose)(nil),   // 10: internal.StreamClose
	nil,                   // 11: internal.Request.MetadataEntry
	nil,                   // 12: internal.StreamOpen.MetadataEntry
	(*anypb.Any)(nil),     // 13: google.protobuf.Any
}
var file_internal_proto_depIdxs = []int32{
	13, // 0: internal.Request.request:type_name -> google.protobuf.Any
	11, // 1: internal.Request.metadata:type_name -> internal.Request.MetadataEntry
	13, // 2: internal.Response.response:type_name -> google.protobuf.Any
	13, // 3: internal.Response.error_details:type_name -> google.protobuf.Any
	7,  // 4: internal.Stream.open:type_name -> internal.StreamOpen
	8,  // 5: internal.Stream.message:type_name -> internal.StreamMessage
	9,  // 6: internal.Stream.ack:type_name -> internal.StreamAck
	10, // 7: internal.Stream.close:type_name -> internal.StreamClose
	12, // 8: internal.StreamOpen.metadata:type_name -> internal.StreamOpen.MetadataEntry
	13, // 9: internal.StreamMessage.message:type_name -> google.protobuf.Any
	10, // [10:10] is the sub-list for method output_type
	10, // [10:10] is the sub-list for method input_type
	10, // [10:10] is the sub-list for extension type_name
	10, // [10:10] is the sub-list for extension extendee
	0,  // [0:10] is the sub-list for field type_name
}

func init() { file_internal_proto_init() }
func file_internal_proto_init() {
	if File_internal_proto != nil {
		return
	}
	file_internal_proto_msgTypes[6].OneofWrappers = []any{
		(*Stream_Open)(nil),
		(*Stream_Message)(nil),
		(*Stream_Ack)(nil),
		(*Stream_Close)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_internal_proto_rawDesc), len(file_internal_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   13,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_internal_proto_goTypes,
		DependencyIndexes: file_internal_proto_depIdxs,
		MessageInfos:      file_internal_proto_msgTypes,
	}.Build()
	File_internal_proto = out.File
	file_internal_proto_goTypes = nil
	file_internal_proto_depIdxs = nil
}
