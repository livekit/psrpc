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
// source: snake_case_names.proto

// Test that protoc-gen-psrpc follows the same behavior as protoc-gen-go
// for converting RPCs and message names from snake case to camel case.

package snake_case_names

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

type MakeHatArgsV1 struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *MakeHatArgsV1) Reset() {
	*x = MakeHatArgsV1{}
	mi := &file_snake_case_names_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *MakeHatArgsV1) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MakeHatArgsV1) ProtoMessage() {}

func (x *MakeHatArgsV1) ProtoReflect() protoreflect.Message {
	mi := &file_snake_case_names_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MakeHatArgsV1.ProtoReflect.Descriptor instead.
func (*MakeHatArgsV1) Descriptor() ([]byte, []int) {
	return file_snake_case_names_proto_rawDescGZIP(), []int{0}
}

type MakeHatArgsV1_HatV1 struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Size          int32                  `protobuf:"varint,1,opt,name=size,proto3" json:"size,omitempty"`
	Color         string                 `protobuf:"bytes,2,opt,name=color,proto3" json:"color,omitempty"`
	Name          string                 `protobuf:"bytes,3,opt,name=name,proto3" json:"name,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *MakeHatArgsV1_HatV1) Reset() {
	*x = MakeHatArgsV1_HatV1{}
	mi := &file_snake_case_names_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *MakeHatArgsV1_HatV1) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MakeHatArgsV1_HatV1) ProtoMessage() {}

func (x *MakeHatArgsV1_HatV1) ProtoReflect() protoreflect.Message {
	mi := &file_snake_case_names_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MakeHatArgsV1_HatV1.ProtoReflect.Descriptor instead.
func (*MakeHatArgsV1_HatV1) Descriptor() ([]byte, []int) {
	return file_snake_case_names_proto_rawDescGZIP(), []int{0, 0}
}

func (x *MakeHatArgsV1_HatV1) GetSize() int32 {
	if x != nil {
		return x.Size
	}
	return 0
}

func (x *MakeHatArgsV1_HatV1) GetColor() string {
	if x != nil {
		return x.Color
	}
	return ""
}

func (x *MakeHatArgsV1_HatV1) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

type MakeHatArgsV1_SizeV1 struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Inches        int32                  `protobuf:"varint,1,opt,name=inches,proto3" json:"inches,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *MakeHatArgsV1_SizeV1) Reset() {
	*x = MakeHatArgsV1_SizeV1{}
	mi := &file_snake_case_names_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *MakeHatArgsV1_SizeV1) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MakeHatArgsV1_SizeV1) ProtoMessage() {}

func (x *MakeHatArgsV1_SizeV1) ProtoReflect() protoreflect.Message {
	mi := &file_snake_case_names_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MakeHatArgsV1_SizeV1.ProtoReflect.Descriptor instead.
func (*MakeHatArgsV1_SizeV1) Descriptor() ([]byte, []int) {
	return file_snake_case_names_proto_rawDescGZIP(), []int{0, 1}
}

func (x *MakeHatArgsV1_SizeV1) GetInches() int32 {
	if x != nil {
		return x.Inches
	}
	return 0
}

var File_snake_case_names_proto protoreflect.FileDescriptor

var file_snake_case_names_proto_rawDesc = string([]byte{
	0x0a, 0x16, 0x73, 0x6e, 0x61, 0x6b, 0x65, 0x5f, 0x63, 0x61, 0x73, 0x65, 0x5f, 0x6e, 0x61, 0x6d,
	0x65, 0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x24, 0x70, 0x73, 0x72, 0x70, 0x63, 0x2e,
	0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x74, 0x65, 0x73, 0x74, 0x2e, 0x73, 0x6e,
	0x61, 0x6b, 0x65, 0x5f, 0x63, 0x61, 0x73, 0x65, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x73, 0x22, 0x7b,
	0x0a, 0x0e, 0x4d, 0x61, 0x6b, 0x65, 0x48, 0x61, 0x74, 0x41, 0x72, 0x67, 0x73, 0x5f, 0x76, 0x31,
	0x1a, 0x46, 0x0a, 0x06, 0x48, 0x61, 0x74, 0x5f, 0x76, 0x31, 0x12, 0x12, 0x0a, 0x04, 0x73, 0x69,
	0x7a, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x05, 0x52, 0x04, 0x73, 0x69, 0x7a, 0x65, 0x12, 0x14,
	0x0a, 0x05, 0x63, 0x6f, 0x6c, 0x6f, 0x72, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x63,
	0x6f, 0x6c, 0x6f, 0x72, 0x12, 0x12, 0x0a, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x03, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x1a, 0x21, 0x0a, 0x07, 0x53, 0x69, 0x7a, 0x65,
	0x5f, 0x76, 0x31, 0x12, 0x16, 0x0a, 0x06, 0x69, 0x6e, 0x63, 0x68, 0x65, 0x73, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x05, 0x52, 0x06, 0x69, 0x6e, 0x63, 0x68, 0x65, 0x73, 0x32, 0x9a, 0x01, 0x0a, 0x0e,
	0x48, 0x61, 0x62, 0x65, 0x72, 0x64, 0x61, 0x73, 0x68, 0x65, 0x72, 0x5f, 0x76, 0x31, 0x12, 0x87,
	0x01, 0x0a, 0x0a, 0x4d, 0x61, 0x6b, 0x65, 0x48, 0x61, 0x74, 0x5f, 0x76, 0x31, 0x12, 0x3c, 0x2e,
	0x70, 0x73, 0x72, 0x70, 0x63, 0x2e, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x74,
	0x65, 0x73, 0x74, 0x2e, 0x73, 0x6e, 0x61, 0x6b, 0x65, 0x5f, 0x63, 0x61, 0x73, 0x65, 0x5f, 0x6e,
	0x61, 0x6d, 0x65, 0x73, 0x2e, 0x4d, 0x61, 0x6b, 0x65, 0x48, 0x61, 0x74, 0x41, 0x72, 0x67, 0x73,
	0x5f, 0x76, 0x31, 0x2e, 0x53, 0x69, 0x7a, 0x65, 0x5f, 0x76, 0x31, 0x1a, 0x3b, 0x2e, 0x70, 0x73,
	0x72, 0x70, 0x63, 0x2e, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2e, 0x74, 0x65, 0x73,
	0x74, 0x2e, 0x73, 0x6e, 0x61, 0x6b, 0x65, 0x5f, 0x63, 0x61, 0x73, 0x65, 0x5f, 0x6e, 0x61, 0x6d,
	0x65, 0x73, 0x2e, 0x4d, 0x61, 0x6b, 0x65, 0x48, 0x61, 0x74, 0x41, 0x72, 0x67, 0x73, 0x5f, 0x76,
	0x31, 0x2e, 0x48, 0x61, 0x74, 0x5f, 0x76, 0x31, 0x42, 0x14, 0x5a, 0x12, 0x2f, 0x3b, 0x73, 0x6e,
	0x61, 0x6b, 0x65, 0x5f, 0x63, 0x61, 0x73, 0x65, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x73, 0x62, 0x06,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
})

var (
	file_snake_case_names_proto_rawDescOnce sync.Once
	file_snake_case_names_proto_rawDescData []byte
)

func file_snake_case_names_proto_rawDescGZIP() []byte {
	file_snake_case_names_proto_rawDescOnce.Do(func() {
		file_snake_case_names_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_snake_case_names_proto_rawDesc), len(file_snake_case_names_proto_rawDesc)))
	})
	return file_snake_case_names_proto_rawDescData
}

var file_snake_case_names_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_snake_case_names_proto_goTypes = []any{
	(*MakeHatArgsV1)(nil),        // 0: psrpc.internal.test.snake_case_names.MakeHatArgs_v1
	(*MakeHatArgsV1_HatV1)(nil),  // 1: psrpc.internal.test.snake_case_names.MakeHatArgs_v1.Hat_v1
	(*MakeHatArgsV1_SizeV1)(nil), // 2: psrpc.internal.test.snake_case_names.MakeHatArgs_v1.Size_v1
}
var file_snake_case_names_proto_depIdxs = []int32{
	2, // 0: psrpc.internal.test.snake_case_names.Haberdasher_v1.MakeHat_v1:input_type -> psrpc.internal.test.snake_case_names.MakeHatArgs_v1.Size_v1
	1, // 1: psrpc.internal.test.snake_case_names.Haberdasher_v1.MakeHat_v1:output_type -> psrpc.internal.test.snake_case_names.MakeHatArgs_v1.Hat_v1
	1, // [1:2] is the sub-list for method output_type
	0, // [0:1] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_snake_case_names_proto_init() }
func file_snake_case_names_proto_init() {
	if File_snake_case_names_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_snake_case_names_proto_rawDesc), len(file_snake_case_names_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_snake_case_names_proto_goTypes,
		DependencyIndexes: file_snake_case_names_proto_depIdxs,
		MessageInfos:      file_snake_case_names_proto_msgTypes,
	}.Build()
	File_snake_case_names_proto = out.File
	file_snake_case_names_proto_goTypes = nil
	file_snake_case_names_proto_depIdxs = nil
}
