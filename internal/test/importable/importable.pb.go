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
// source: importable.proto

// Test to make sure that importing other packages doesnt break

package importable

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
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
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Msg) Reset() {
	*x = Msg{}
	mi := &file_importable_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Msg) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Msg) ProtoMessage() {}

func (x *Msg) ProtoReflect() protoreflect.Message {
	mi := &file_importable_proto_msgTypes[0]
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
	return file_importable_proto_rawDescGZIP(), []int{0}
}

var File_importable_proto protoreflect.FileDescriptor

var file_importable_proto_rawDesc = string([]byte{
	0x0a, 0x10, 0x69, 0x6d, 0x70, 0x6f, 0x72, 0x74, 0x61, 0x62, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x12, 0x1e, 0x70, 0x73, 0x72, 0x70, 0x63, 0x2e, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e,
	0x61, 0x6c, 0x2e, 0x74, 0x65, 0x73, 0x74, 0x2e, 0x69, 0x6d, 0x70, 0x6f, 0x72, 0x74, 0x61, 0x62,
	0x6c, 0x65, 0x22, 0x05, 0x0a, 0x03, 0x4d, 0x73, 0x67, 0x32, 0x57, 0x0a, 0x03, 0x53, 0x76, 0x63,
	0x12, 0x50, 0x0a, 0x04, 0x53, 0x65, 0x6e, 0x64, 0x12, 0x23, 0x2e, 0x70, 0x73, 0x72, 0x70, 0x63,
	0x2e, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x74, 0x65, 0x73, 0x74, 0x2e, 0x69,
	0x6d, 0x70, 0x6f, 0x72, 0x74, 0x61, 0x62, 0x6c, 0x65, 0x2e, 0x4d, 0x73, 0x67, 0x1a, 0x23, 0x2e,
	0x70, 0x73, 0x72, 0x70, 0x63, 0x2e, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x74,
	0x65, 0x73, 0x74, 0x2e, 0x69, 0x6d, 0x70, 0x6f, 0x72, 0x74, 0x61, 0x62, 0x6c, 0x65, 0x2e, 0x4d,
	0x73, 0x67, 0x42, 0x33, 0x5a, 0x31, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d,
	0x2f, 0x6c, 0x69, 0x76, 0x65, 0x6b, 0x69, 0x74, 0x2f, 0x70, 0x73, 0x72, 0x70, 0x63, 0x2f, 0x69,
	0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2f, 0x74, 0x65, 0x73, 0x74, 0x2f, 0x69, 0x6d, 0x70,
	0x6f, 0x72, 0x74, 0x61, 0x62, 0x6c, 0x65, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
})

var (
	file_importable_proto_rawDescOnce sync.Once
	file_importable_proto_rawDescData []byte
)

func file_importable_proto_rawDescGZIP() []byte {
	file_importable_proto_rawDescOnce.Do(func() {
		file_importable_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_importable_proto_rawDesc), len(file_importable_proto_rawDesc)))
	})
	return file_importable_proto_rawDescData
}

var file_importable_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_importable_proto_goTypes = []any{
	(*Msg)(nil), // 0: psrpc.internal.test.importable.Msg
}
var file_importable_proto_depIdxs = []int32{
	0, // 0: psrpc.internal.test.importable.Svc.Send:input_type -> psrpc.internal.test.importable.Msg
	0, // 1: psrpc.internal.test.importable.Svc.Send:output_type -> psrpc.internal.test.importable.Msg
	1, // [1:2] is the sub-list for method output_type
	0, // [0:1] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_importable_proto_init() }
func file_importable_proto_init() {
	if File_importable_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_importable_proto_rawDesc), len(file_importable_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_importable_proto_goTypes,
		DependencyIndexes: file_importable_proto_depIdxs,
		MessageInfos:      file_importable_proto_msgTypes,
	}.Build()
	File_importable_proto = out.File
	file_importable_proto_goTypes = nil
	file_importable_proto_depIdxs = nil
}
