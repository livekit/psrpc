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

package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"go/parser"
	"go/printer"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/exp/slices"
	"golang.org/x/mod/semver"
	"google.golang.org/protobuf/proto"
	descriptor "google.golang.org/protobuf/types/descriptorpb"
	plugin "google.golang.org/protobuf/types/pluginpb"

	"github.com/livekit/psrpc/protoc-gen-psrpc/internal/gen"
	"github.com/livekit/psrpc/protoc-gen-psrpc/internal/gen/stringutils"
	"github.com/livekit/psrpc/protoc-gen-psrpc/internal/gen/typemap"
	"github.com/livekit/psrpc/protoc-gen-psrpc/options"
	"github.com/livekit/psrpc/version"
)

type psrpc struct {
	filesHandled int

	reg *typemap.Registry

	// Map to record whether we've built each package
	pkgs          map[string]string
	pkgNamesInUse map[string]bool

	importPrefix string            // String to prefix to imported package file names.
	importMap    map[string]string // Mapping from .proto file name to import path.

	// Package output:
	sourceRelativePaths bool // instruction on where to write output files
	modulePrefix        string

	// Package naming:
	genPkgName          string // Name of the package that we're generating
	fileToGoPackageName map[*descriptor.FileDescriptorProto]string

	// List of files that were inputs to the generator. We need to hold this in
	// the struct, so we can write a header for the file that lists its inputs.
	genFiles []*descriptor.FileDescriptorProto

	// Output buffer that holds the bytes we want to write out for a single file.
	// Gets reset after working on a file.
	output *bytes.Buffer
}

func newGenerator() *psrpc {
	t := &psrpc{
		pkgs:                make(map[string]string),
		pkgNamesInUse:       make(map[string]bool),
		importMap:           make(map[string]string),
		fileToGoPackageName: make(map[*descriptor.FileDescriptorProto]string),
		output:              bytes.NewBuffer(nil),
	}

	return t
}

func (t *psrpc) Generate(in *plugin.CodeGeneratorRequest) *plugin.CodeGeneratorResponse {
	params, err := parseCommandLineParams(in.GetParameter())
	if err != nil {
		gen.Fail("could not parse parameters passed to --psrpc_out", err.Error())
	}
	t.importPrefix = params.importPrefix
	t.importMap = params.importMap
	t.sourceRelativePaths = params.paths == "source_relative"
	t.modulePrefix = params.module

	t.genFiles = gen.FilesToGenerate(in)

	// Collect information on types.
	t.reg = typemap.New(in.ProtoFile)

	// Register names of packages that we import.
	t.registerPackageName("client")
	t.registerPackageName("context")
	t.registerPackageName("info")
	t.registerPackageName("psrpc")
	t.registerPackageName("rand")
	t.registerPackageName("server")
	t.registerPackageName("version")

	// Time to figure out package names of objects defined in protobuf. First,
	// we'll figure out the name for the package we're generating.
	genPkgName, err := deduceGenPkgName(t.genFiles)
	if err != nil {
		gen.Fail(err.Error())
	}
	t.genPkgName = genPkgName

	// We also need to figure out the full import path of the package we're
	// generating. It's possible to import proto definitions from different .proto
	// files which will be generated into the same Go package, which we need to
	// detect (and can only detect if files use fully-specified go_package
	// options).
	genPkgImportPath, _, _ := goPackageOption(t.genFiles[0])

	// Next, we need to pick names for all the files that are dependencies.
	for _, f := range in.ProtoFile {
		if *f.Name == "options.proto" && *f.Package == "psrpc" {
			continue
		}

		// Is this is a file we are generating? If yes, it gets the shared package name.
		if fileDescSliceContains(t.genFiles, f) {
			t.fileToGoPackageName[f] = t.genPkgName
			continue
		}

		// Is this is an imported .proto file which has the same fully-specified
		// go_package as the targeted file for generation? If yes, it gets the
		// shared package name too.
		if genPkgImportPath != "" {
			importPath, _, _ := goPackageOption(f)
			if importPath == genPkgImportPath {
				t.fileToGoPackageName[f] = t.genPkgName
				continue
			}
		}

		// This is a dependency from a different go_package. Use its package name.
		name := f.GetPackage()
		if name == "" {
			name = stringutils.BaseName(f.GetName())
		}
		name = stringutils.CleanIdentifier(name)
		alias := t.registerPackageName(name)
		t.fileToGoPackageName[f] = alias
	}

	// Showtime! Generate the response.
	resp := new(plugin.CodeGeneratorResponse)
	resp.SupportedFeatures = proto.Uint64(uint64(plugin.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL))
	for _, f := range t.genFiles {
		respFile := t.generate(f)
		if respFile != nil {
			resp.File = append(resp.File, respFile)
		}
	}
	return resp
}

func (t *psrpc) registerPackageName(name string) (alias string) {
	alias = name
	i := 1
	for t.pkgNamesInUse[alias] {
		alias = name + strconv.Itoa(i)
		i++
	}
	t.pkgNamesInUse[alias] = true
	t.pkgs[name] = alias
	return alias
}

// deduceGenPkgName figures out the go package name to use for generated code.
// Will try to use the explicit go_package setting in a file (if set, must be
// consistent in all files). If no files have go_package set, then use the
// protobuf package name (must be consistent in all files)
func deduceGenPkgName(genFiles []*descriptor.FileDescriptorProto) (string, error) {
	var genPkgName string
	for _, f := range genFiles {
		name, explicit := goPackageName(f)
		if explicit {
			name = stringutils.CleanIdentifier(name)
			if genPkgName != "" && genPkgName != name {
				// Make sure they're all set consistently.
				return "", errors.Errorf("files have conflicting go_package settings, must be the same: %q and %q", genPkgName, name)
			}
			genPkgName = name
		}
	}
	if genPkgName != "" {
		return genPkgName, nil
	}

	// If there is no explicit setting, then check the implicit package name
	// (derived from the protobuf package name) of the files and make sure it's
	// consistent.
	for _, f := range genFiles {
		name, _ := goPackageName(f)
		name = stringutils.CleanIdentifier(name)
		if genPkgName != "" && genPkgName != name {
			return "", errors.Errorf("files have conflicting package names, must be the same or overridden with go_package: %q and %q", genPkgName, name)
		}
		genPkgName = name
	}

	// All the files have the same name, so we're good.
	return genPkgName, nil
}

func (t *psrpc) generate(file *descriptor.FileDescriptorProto) *plugin.CodeGeneratorResponse_File {
	resp := new(plugin.CodeGeneratorResponse_File)
	if len(file.Service) == 0 {
		return nil
	}

	t.generateFileHeader(file)
	t.generateImports(file)

	t.P(`var _ = `, t.pkgs["version"], fmt.Sprintf(".PsrpcVersion_%s", strings.Replace(semver.MajorMinor(version.Version)[1:], ".", "_", 1)))

	// For each service, generate client stubs and server
	for _, service := range file.Service {
		t.generateService(file, service)
	}

	t.generateFileDescriptor(file)

	resp.Name = proto.String(t.goFileName(file))
	resp.Content = proto.String(t.formattedOutput())
	t.output.Reset()

	t.filesHandled++
	return resp
}

func (t *psrpc) generateFileHeader(file *descriptor.FileDescriptorProto) {
	t.P("// Code generated by protoc-gen-psrpc ", version.Version, ", DO NOT EDIT.")
	t.P("// source: ", file.GetName())
	t.P()

	comment, err := t.reg.FileComments(file)
	if err == nil && comment.Leading != "" {
		for _, line := range strings.Split(comment.Leading, "\n") {
			if line != "" {
				t.P("// " + strings.TrimPrefix(line, " "))
			}
		}
		t.P()
	}

	t.P(`package `, t.genPkgName)
	t.P()
}

func (t *psrpc) generateImports(file *descriptor.FileDescriptorProto) {
	if len(file.Service) == 0 {
		return
	}

	// stdlib imports
	t.P(`import (`)
	for _, service := range file.Service {
		if len(service.Method) > 0 {
			t.P(`  "context"`)
			t.P()
			break
		}
	}

	// dependency imports
	t.P(`  "github.com/livekit/psrpc"`)
	t.P(`  "github.com/livekit/psrpc/pkg/client"`)
	t.P(`  "github.com/livekit/psrpc/pkg/info"`)
	t.P(`  "github.com/livekit/psrpc/pkg/rand"`)
	t.P(`  "github.com/livekit/psrpc/pkg/server"`)
	t.P(`  "github.com/livekit/psrpc/version"`)
	t.P(`)`)

	// It's legal to import a message and use it as an input or output for a
	// method. Make sure to import the package of any such message. First, dedupe
	// them.
	deps := make(map[string]string) // Map of package name to quoted import path.
	ourImportPath := path.Dir(t.goFileName(file))
	for _, s := range file.Service {
		for _, m := range s.Method {
			defs := []*typemap.MessageDefinition{
				t.reg.MethodInputDefinition(m),
				t.reg.MethodOutputDefinition(m),
			}
			for _, def := range defs {
				importPath, _ := parseGoPackageOption(def.File.GetOptions().GetGoPackage())
				if importPath == "" { // no option go_package
					importPath := path.Dir(t.goFileName(def.File)) // use the dirname of the Go filename as import path
					if importPath == ourImportPath {
						continue
					}
				}

				if substitution, ok := t.importMap[def.File.GetName()]; ok {
					importPath = substitution
				}
				importPath = t.importPrefix + importPath

				pkg := t.goPackageName(def.File)
				if pkg != t.genPkgName {
					deps[pkg] = strconv.Quote(importPath)
				}
			}
		}
	}
	pkgs := make([]string, 0, len(deps))
	for pkg := range deps {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		t.P(`import `, pkg, ` `, deps[pkg])
	}
	if len(deps) > 0 {
		t.P()
	}
}

// W forwards to g.gen.P, which prints output.
func (t *psrpc) W(args ...string) {
	for _, v := range args {
		t.output.WriteString(v)
	}
}

// P forwards to g.gen.P, which prints output.
func (t *psrpc) P(args ...string) {
	for _, v := range args {
		t.output.WriteString(v)
	}
	t.output.WriteByte('\n')
}

// Big header comments to makes it easier to visually parse a generated file.
func (t *psrpc) sectionComment(sectionTitle string) {
	t.P()
	t.P(`// `, strings.Repeat("=", len(sectionTitle)))
	t.P(`// `, sectionTitle)
	t.P(`// `, strings.Repeat("=", len(sectionTitle)))
	t.P()
}

const (
	client     = "Client"
	serverImpl = "ServerImpl"
	server     = "Server"
)

func (t *psrpc) generateService(file *descriptor.FileDescriptorProto, service *descriptor.ServiceDescriptorProto) {
	servName := serviceNameCamelCased(service)

	t.sectionComment(servName + ` Client Interface`)
	t.generateInterface(file, service, client)

	t.sectionComment(servName + ` ServerImpl Interface`)
	t.generateInterface(file, service, serverImpl)

	t.sectionComment(servName + ` Server Interface`)
	t.generateInterface(file, service, server)

	t.sectionComment(servName + ` Client`)
	t.generateClient(service)

	t.sectionComment(servName + ` Server`)
	t.generateServer(service)

	t.sectionComment(servName + ` Unimplemented Server`)
	t.generateUnimplementedServer(service)
}

func (t *psrpc) generateInterface(file *descriptor.FileDescriptorProto, service *descriptor.ServiceDescriptorProto, iface string) {
	servName := serviceNameCamelCased(service)

	comments, err := t.reg.ServiceComments(file, service)
	if err == nil {
		t.printComments(comments)
	}

	var topics topicSlice
	if iface != serverImpl {
		topics = t.typedTopicsForService(service)
	}

	t.P(`type `, servName, iface, topics.FormatTypeParamConstraints(), ` interface {`)
	for _, method := range service.Method {
		opts := t.getOptions(method)
		if (iface == serverImpl && opts.Subscription) || (iface == server && !opts.Subscription && !opts.Topics) {
			continue
		}

		comments, err = t.reg.MethodComments(file, service, method)
		if err == nil {
			t.printComments(comments)
		}
		switch iface {
		case client:
			t.generateClientSignature(method, opts)
		case serverImpl:
			t.generateServerImplSignature(method, opts)
		case server:
			t.generateServerSignature(method, opts)
		}
	}
	if iface == server {
		for _, group := range t.topicGroupsForService(service) {
			t.P(`  RegisterAll`, group.typeName, `Topics(`, group.topics.FormatParams(), `) error`)
			t.P(`  DeregisterAll`, group.typeName, `Topics(`, group.topics.FormatParams(), `)`)
		}
		t.P()

		t.P(`  // Close and wait for pending RPCs to complete`)
		t.P(`  Shutdown()`)
		t.P()
		t.P(`  // Close immediately, without waiting for pending RPCs`)
		t.P(`  Kill()`)
	} else if iface == client {
		t.P(`  // Close immediately, without waiting for pending RPCs`)
		t.P(`  Close()`)
	}
	t.P(`}`)
}

func (t *psrpc) generateClientSignature(method *descriptor.MethodDescriptorProto, opts *options.Options) {
	methName := methodNameCamelCased(method)
	inputType := t.goTypeName(method.GetInputType())
	outputType := t.goTypeName(method.GetOutputType())

	if opts.Subscription {
		t.W(`  Subscribe`)
	} else {
		t.W(`  `)
	}
	t.W(methName, `(ctx `, t.pkgs["context"], `.Context`)
	if opts.Topics {
		t.W(`, `, t.topicsForMethod(method).FormatParams())
	}
	if opts.Subscription {
		t.P(`) (`, t.pkgs["psrpc"], `.Subscription[*`, outputType, `], error)`)
	} else if opts.Stream {
		t.P(`, opts ...`, t.pkgs["psrpc"], `.RequestOption) (`, t.pkgs["psrpc"], `.ClientStream[*`, inputType, `, *`, outputType, `], error)`)
	} else if opts.Type == options.Routing_MULTI {
		t.P(`, req *`, inputType, `, opts ...`, t.pkgs["psrpc"], `.RequestOption) (<-chan *`, t.pkgs["psrpc"], `.Response[*`, outputType, `], error)`)
	} else {
		t.P(`, req *`, inputType, `, opts ...`, t.pkgs["psrpc"], `.RequestOption) (*`, outputType, `, error)`)
	}
	t.P()
}

func (t *psrpc) generateClient(service *descriptor.ServiceDescriptorProto) {
	servName := serviceNameCamelCased(service)
	servTopics := t.typedTopicsForService(service)
	structName := unexported(servName) + "Client"
	newClientFunc := "New" + servName + "Client"

	t.P(`type `, structName, servTopics.FormatTypeParamConstraints(), ` struct {`)
	t.P(`  client *`, t.pkgs["client"], `.RPCClient`)
	t.P(`}`)
	t.P()

	t.P(`// `, newClientFunc, ` creates a psrpc client that implements the `, servName, `Client interface.`)
	t.P(`func `, newClientFunc, servTopics.FormatTypeParamConstraints(), `(bus `, t.pkgs["psrpc"], `.MessageBus, opts ...`, t.pkgs["psrpc"], `.ClientOption) (`, servName, `Client`, servTopics.FormatTypeParams(), `, error) {`)
	t.P(`  sd := &`, t.pkgs["info"], `.ServiceDefinition{`)
	t.P(`    Name: "`, servName, `",`)
	t.P(`    ID:   `, t.pkgs["rand"], `.NewClientID(),`)
	t.P(`  }`)
	t.P()

	for _, method := range service.Method {
		opts := t.getOptions(method)

		methName := methodNameCamelCased(method)
		t.P(`  sd.RegisterMethod("`, methName, `", `,
			fmt.Sprint(opts.Type == options.Routing_AFFINITY), `, `,
			fmt.Sprint(opts.Type == options.Routing_MULTI), `, `,
			fmt.Sprint(t.getRequireClaim(opts)), `, `,
			fmt.Sprint(opts.Type == options.Routing_QUEUE), `)`,
		)
	}

	clientConstructor := `NewRPCClient`
	for _, method := range service.Method {
		if t.getOptions(method).Stream {
			clientConstructor = `NewRPCClientWithStreams`
		}
	}
	t.P()
	t.P(`  rpcClient, err := `, t.pkgs["client"], `.`, clientConstructor, `(sd, bus, opts...)`)
	t.P(`  if err != nil {`)
	t.P(`    return nil, err`)
	t.P(`  }`)
	t.P()
	t.P(`  return &`, structName, servTopics.FormatTypeParams(), `{`)
	t.P(`    client: rpcClient,`)
	t.P(`  }, nil`)
	t.P(`}`)
	t.P()

	for _, method := range service.Method {
		methName := methodNameCamelCased(method)
		inputType := t.goTypeName(method.GetInputType())
		outputType := t.goTypeName(method.GetOutputType())
		opts := t.getOptions(method)
		topics := t.topicsForMethod(method)

		t.W(`func (c *`, structName, servTopics.FormatTypeParams())
		if opts.Subscription {
			t.W(`) Subscribe`)
		} else {
			t.W(`) `)
		}
		t.W(methName, `(ctx `, t.pkgs["context"], `.Context`)
		if opts.Topics {
			t.W(`, `, topics.FormatParams())
		}
		if opts.Subscription {
			t.P(`) (`, t.pkgs["psrpc"], `.Subscription[*`, outputType, `], error) {`)
		} else if opts.Stream {
			t.P(`, opts ...`, t.pkgs["psrpc"], `.RequestOption) (`, t.pkgs["psrpc"], `.ClientStream[*`, inputType, `, *`, outputType, `], error) {`)
		} else {
			t.W(`, req *`, inputType, `, opts ...`, t.pkgs["psrpc"], `.RequestOption`)
			if opts.Type == options.Routing_MULTI {
				t.P(`) (<-chan *`, t.pkgs["psrpc"], `.Response[*`, outputType, `], error) {`)
			} else {
				t.P(`) (*`, outputType, `, error) {`)
			}
		}

		t.W(`  return `, t.pkgs["client"])
		if opts.Subscription {
			if opts.Type == options.Routing_MULTI {
				t.W(`.Join[*`)
			} else {
				t.W(`.JoinQueue[*`)
			}
			t.P(outputType, `](ctx, c.client, "`, methName, `", `, topics.FormatCastToStringSlice(), `)`)
		} else if opts.Stream {
			t.P(`.OpenStream[*`, inputType, `, *`, outputType, `](ctx, c.client, "`, methName, `", `, topics.FormatCastToStringSlice(), `, opts...)`)
		} else {
			if opts.Type == options.Routing_MULTI {
				t.W(`.RequestMulti[*`)
			} else {
				t.W(`.RequestSingle[*`)
			}
			t.W(outputType, `](ctx, c.client, "`, methName, `", `, topics.FormatCastToStringSlice())
			t.P(`, req, opts...)`)
		}
		t.P(`}`)
		t.P()
	}

	t.P(`func (s *`, structName, servTopics.FormatTypeParams(), `) Close() {`)
	t.P(`  s.client.Close()`)
	t.P(`}`)
	t.P()
}

func (t *psrpc) generateServerImplSignature(method *descriptor.MethodDescriptorProto, opts *options.Options) {
	methName := methodNameCamelCased(method)
	inputType := t.goTypeName(method.GetInputType())
	outputType := t.goTypeName(method.GetOutputType())

	if opts.Stream {
		t.P(`  `, methName, `(`, t.pkgs["psrpc"], `.ServerStream[*`, outputType, `, *`, inputType, `]) error`)
		if opts.Type == options.Routing_AFFINITY {
			t.P(`  `, methName, `Affinity(context.Context) float32`)
		}
	} else {
		t.P(`  `, methName, `(`, t.pkgs["context"], `.Context, *`, inputType, `) (*`, outputType, `, error)`)
		if opts.Type == options.Routing_AFFINITY {
			t.P(`  `, methName, `Affinity(context.Context, *`, inputType, `) float32`)
		}
	}
	t.P()
}

func (t *psrpc) generateServerSignature(method *descriptor.MethodDescriptorProto, opts *options.Options) {
	methName := methodNameCamelCased(method)
	outputType := t.goTypeName(method.GetOutputType())
	topics := t.topicsForMethod(method)

	if opts.Subscription {
		t.W(`  Publish`, methName, `(ctx `, t.pkgs["context"], `.Context`)
		if opts.Topics {
			t.W(`, `, topics.FormatParams())
		}
		t.P(`, msg *`, outputType, `) error`)
		t.P()
	} else {
		t.P(`  Register`, methName, `Topic(`, topics.FormatParams(), `) error`)
		t.P(`  Deregister`, methName, `Topic(`, topics.FormatParams(), `)`)
	}
}

func (t *psrpc) generateServer(service *descriptor.ServiceDescriptorProto) {
	servName := serviceNameCamelCased(service)
	servTopics := t.typedTopicsForService(service)

	// Server implementation.
	servStruct := serviceStruct(service)
	t.P(`type `, servStruct, servTopics.FormatTypeParamConstraints(), ` struct {`)
	t.P(`  svc `, servName, `ServerImpl`)
	t.P(`  rpc *`, t.pkgs["server"], `.RPCServer`)
	t.P(`}`)
	t.P()

	// Constructor for server implementation
	t.P(`// New`, servName, `Server builds a RPCServer that will route requests`)
	t.P(`// to the corresponding method in the provided svc implementation.`)
	t.P(`func New`, servName, `Server`, servTopics.FormatTypeParamConstraints(), `(svc `, servName, `ServerImpl, bus `, t.pkgs["psrpc"], `.MessageBus, opts ...`, t.pkgs["psrpc"], `.ServerOption) (`, servName, `Server`, servTopics.FormatTypeParams(), `, error) {`)
	t.P(`  sd := &`, t.pkgs["info"], `.ServiceDefinition{`)
	t.P(`    Name: "`, servName, `",`)
	t.P(`    ID:   `, t.pkgs["rand"], `.NewServerID(),`)
	t.P(`  }`)
	t.P()
	t.P(`  s := `, t.pkgs["server"], `.NewRPCServer(sd, bus, opts...)`)
	t.P()

	errVar := false
	for _, method := range service.Method {
		opts := t.getOptions(method)

		methName := methodNameCamelCased(method)
		t.P(`  sd.RegisterMethod("`, methName, `", `,
			fmt.Sprint(opts.Type == options.Routing_AFFINITY), `, `,
			fmt.Sprint(opts.Type == options.Routing_MULTI), `, `,
			fmt.Sprint(t.getRequireClaim(opts)), `, `,
			fmt.Sprint(opts.Type == options.Routing_QUEUE), `)`,
		)

		if opts.Subscription || opts.Topics {
			continue
		}

		if !errVar {
			t.P(`  var err error`)
			errVar = true
		}

		registerFuncName := "RegisterHandler"
		if opts.Stream {
			registerFuncName = "RegisterStreamHandler"
		}
		t.W(`  err = `, t.pkgs["server"], `.`, registerFuncName, `(s, "`, methName, `", nil, svc.`, methName)
		if t.getOptions(method).Type == options.Routing_AFFINITY {
			t.W(`, svc.`, methName, `Affinity`)
		} else {
			t.W(`, nil`)
		}
		t.P(`)`)
		t.P(`  if err != nil {`)
		t.P(`    s.Close(false)`)
		t.P(`    return nil, err`)
		t.P(`  }`)
		t.P()
	}

	t.P(`  return &`, servStruct, servTopics.FormatTypeParams(), `{`)
	t.P(`    svc: svc,`)
	t.P(`    rpc: s,`)
	t.P(`  }, nil`)
	t.P(`}`)
	t.P()

	for _, method := range service.Method {
		opts := t.getOptions(method)
		if !opts.Subscription && !opts.Topics {
			continue
		}

		methName := methodNameCamelCased(method)
		outputType := t.goTypeName(method.GetOutputType())
		topics := t.topicsForMethod(method)

		if opts.Subscription {
			t.W(`func (s *`, servStruct, servTopics.FormatTypeParams(), `) Publish`, methName, `(ctx `, t.pkgs["context"], `.Context`)
			if opts.Topics {
				t.W(`, `, topics.FormatParams())
			}
			t.P(`, msg *`, outputType, `) error {`)
			t.P(`  return s.rpc.Publish(ctx, "`, methName, `", `, topics.FormatCastToStringSlice(), `, msg)`)
			t.P(`}`)
			t.P()
		} else {
			registerFuncName := "RegisterHandler"
			if opts.Stream {
				registerFuncName = "RegisterStreamHandler"
			}
			t.P(`func (s *`, servStruct, servTopics.FormatTypeParams(), `) Register`, methName, `Topic(`, topics.FormatParams(), `) error {`)
			t.W(`  return `, t.pkgs["server"], `.`, registerFuncName, `(s.rpc, "`, methName, `", `, topics.FormatCastToStringSlice(), `, s.svc.`, methName)
			if t.getOptions(method).Type == options.Routing_AFFINITY {
				t.W(`, s.svc.`, methName, `Affinity`)
			} else {
				t.W(`, nil`)
			}
			t.P(`)`)
			t.P(`}`)
			t.P()
			t.P(`func (s *`, servStruct, servTopics.FormatTypeParams(), `) Deregister`, methName, `Topic(`, topics.FormatParams(), `) {`)
			t.P(`  s.rpc.DeregisterHandler("`, methName, `", `, topics.FormatCastToStringSlice(), `)`)
			t.P(`}`)
			t.P()
		}
	}

	for _, group := range t.topicGroupsForService(service) {
		t.P(`func (s *`, servStruct, servTopics.FormatTypeParams(), `) all`, group.typeName, `TopicRegisterers() `, t.pkgs["server"], `.RegistererSlice {`)
		t.P(`  return `, t.pkgs["server"], `.RegistererSlice{`)
		for _, methName := range group.methNames {
			t.P(`    `, t.pkgs["server"], `.NewRegisterer(s.Register`, methName, `Topic, s.Deregister`, methName, `Topic),`)
		}
		t.P(`  }`)
		t.P(`}`)
		t.P()
		t.P(`func (s *`, servStruct, servTopics.FormatTypeParams(), `) RegisterAll`, group.typeName, `Topics(`, group.topics.FormatParams(), `) error {`)
		t.P(`  return s.all`, group.typeName, `TopicRegisterers().Register(`, strings.Join(group.topics.VarNames(), ", "), `)`)
		t.P(`}`)
		t.P()
		t.P(`func (s *`, servStruct, servTopics.FormatTypeParams(), `) DeregisterAll`, group.typeName, `Topics(`, group.topics.FormatParams(), `) {`)
		t.P(`  s.all`, group.typeName, `TopicRegisterers().Deregister(`, strings.Join(group.topics.VarNames(), ", "), `)`)
		t.P(`}`)
		t.P()
	}

	t.P(`func (s *`, servStruct, servTopics.FormatTypeParams(), `) Shutdown() {`)
	t.P(`  s.rpc.Close(false)`)
	t.P(`}`)
	t.P()
	t.P(`func (s *`, servStruct, servTopics.FormatTypeParams(), `) Kill() {`)
	t.P(`  s.rpc.Close(true)`)
	t.P(`}`)
	t.P()
}

func (t *psrpc) generateUnimplementedServer(service *descriptor.ServiceDescriptorProto) {
	servName := fmt.Sprintf("Unimplemented%sServer", serviceNameCamelCased(service))
	t.P(`type `, servName, ` struct{}`)
	for _, method := range service.Method {
		opts := t.getOptions(method)
		methName := methodNameCamelCased(method)
		inputType := t.goTypeName(method.GetInputType())
		outputType := t.goTypeName(method.GetOutputType())

		if opts.Stream {
			t.P(`func (`, servName, `) `, methName, `(`, t.pkgs["psrpc"], `.ServerStream[*`, outputType, `, *`, inputType, `]) error { return psrpc.ErrUnimplemented }`)
			if opts.Type == options.Routing_AFFINITY {
				t.P(`func (`, servName, `) `, methName, `Affinity(context.Context) float32 { return -1 }`)
			}
		} else {
			t.P(`func (`, servName, `) `, methName, `(`, t.pkgs["context"], `.Context, *`, inputType, `) (*`, outputType, `, error) { return nil, psrpc.ErrUnimplemented }`)
			if opts.Type == options.Routing_AFFINITY {
				t.P(`func (`, servName, `) `, methName, `Affinity(context.Context, *`, inputType, `) float32 { return -1 }`)
			}
		}
		t.P()
	}
}

func (t *psrpc) getOptions(method *descriptor.MethodDescriptorProto) *options.Options {
	if method.Options == nil || !proto.HasExtension(method.Options, options.E_Options) {
		return &options.Options{}
	}

	opts := proto.GetExtension(method.Options, options.E_Options).(*options.Options)
	switch opts.Routing.(type) {
	case *options.Options_AffinityFunc:
		opts.Type = options.Routing_AFFINITY
	case *options.Options_Multi:
		opts.Type = options.Routing_MULTI
	case *options.Options_Queue:
		opts.Type = options.Routing_QUEUE
	}

	return opts
}

func (t *psrpc) getRequireClaim(opts *options.Options) bool {
	return opts.Type != options.Routing_MULTI && (opts.TopicParams == nil || !opts.TopicParams.SingleServer)
}

type topic struct {
	varName  string
	typeName string
	typed    bool
}

func (t topic) FormatCastToString() string {
	if !t.typed {
		return t.varName
	}
	return fmt.Sprintf("string(%s)", t.varName)
}

type topicSlice []topic

func (s topicSlice) TypeNames() (names []string) {
	for _, t := range s {
		names = append(names, t.typeName)
	}
	return
}

func (s topicSlice) VarNames() (names []string) {
	for _, t := range s {
		names = append(names, t.varName)
	}
	return
}

func (s topicSlice) FormatTypeParamConstraints() string {
	if len(s) == 0 || !s[0].typed {
		return ""
	}
	return fmt.Sprintf("[%s ~string]", strings.Join(s.TypeNames(), ", "))
}

func (s topicSlice) FormatTypeParams() string {
	if len(s) == 0 || !s[0].typed {
		return ""
	}
	return fmt.Sprintf("[%s]", strings.Join(s.TypeNames(), ", "))
}

func (s topicSlice) FormatParams() string {
	p := make([]string, 0, len(s))
	for _, t := range s {
		p = append(p, fmt.Sprintf("%s %s", t.varName, t.typeName))
	}
	return strings.Join(p, ", ")
}

func (s topicSlice) FormatTypeNames() string {
	return strings.Join(s.TypeNames(), ", ")
}

func (s topicSlice) FormatCastToStringSlice() string {
	vars := make([]string, 0, len(s))
	for _, t := range s {
		vars = append(vars, t.FormatCastToString())
	}
	if len(vars) == 0 {
		return "nil"
	}
	return fmt.Sprintf("[]string{%s}", strings.Join(vars, ", "))
}

func (t *psrpc) topicsForMethod(method *descriptor.MethodDescriptorProto) topicSlice {
	opt := t.getOptions(method)
	if !opt.Topics {
		return nil
	}
	if opt.TopicParams == nil || len(opt.TopicParams.Names) == 0 {
		return []topic{{"topic", "string", false}}
	}
	ts := make([]topic, 0, len(opt.TopicParams.Names))
	for _, p := range opt.TopicParams.Names {
		t := topic{
			varName:  stringutils.LowerCamelCase(p),
			typeName: "string",
			typed:    opt.TopicParams.Typed,
		}
		if t.typed {
			t.typeName = fmt.Sprintf("%sTopicType", stringutils.CamelCase(p))
		}
		ts = append(ts, t)
	}
	return ts
}

func (t *psrpc) typedTopicsForService(service *descriptor.ServiceDescriptorProto) topicSlice {
	var ts []topic
	found := map[string]bool{}
	for _, m := range service.Method {
		for _, t := range t.topicsForMethod(m) {
			if t.typed && !found[t.typeName] {
				found[t.typeName] = true
				ts = append(ts, t)
			}
		}
	}
	return ts
}

type topicGroup struct {
	methNames []string
	typeName  string
	topics    topicSlice
}

type topicGroupSlice []topicGroup

func (t *psrpc) topicGroupsForService(service *descriptor.ServiceDescriptorProto) topicGroupSlice {
	var gs []topicGroup
	found := map[string]int{}
	for _, m := range service.Method {
		opt := t.getOptions(m)
		if opt.TopicParams == nil || opt.TopicParams.Group == "" || opt.Subscription {
			continue
		}

		t := t.topicsForMethod(m)
		g := topicGroup{
			methNames: []string{methodNameCamelCased(m)},
			typeName:  stringutils.CamelCase(opt.TopicParams.Group),
			topics:    t,
		}

		if i, ok := found[opt.TopicParams.Group]; ok {
			if !slices.Equal(gs[i].topics, g.topics) {
				gen.Fail(fmt.Sprintf(`all members of topic group "%s" must have the same parameter names and typed values. mismatch found in "%s"`, opt.TopicParams.Group, *m.Name))
			}
			gs[i].methNames = append(gs[i].methNames, methodNameCamelCased(m))
		} else {
			found[opt.TopicParams.Group] = len(gs)
			gs = append(gs, g)
		}
	}
	return gs
}

// serviceMetadataVarName is the variable name used in generated code to refer
// to the compressed bytes of this descriptor. It is not exported, so it is only
// valid inside the generated package.
//
// protoc-gen-go writes its own version of this file, but so does
// protoc-gen-gogo - with a different name! PSRPC aims to be compatible with
// both; the simplest way forward is to write the file descriptor again as
// another variable that we control.
func (t *psrpc) serviceMetadataVarName() string {
	return fmt.Sprintf("psrpcFileDescriptor%d", t.filesHandled)
}

func (t *psrpc) generateFileDescriptor(file *descriptor.FileDescriptorProto) {
	// Copied straight off of protoc-gen-go, which trims out comments.
	pb := proto.Clone(file).(*descriptor.FileDescriptorProto)
	pb.SourceCodeInfo = nil

	b, err := proto.Marshal(pb)
	if err != nil {
		gen.Fail(err.Error())
	}

	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = w.Write(b)
	_ = w.Close()
	b = buf.Bytes()

	v := t.serviceMetadataVarName()
	t.P()
	t.P("var ", v, " = []byte{")
	t.P("	// ", fmt.Sprintf("%d", len(b)), " bytes of a gzipped FileDescriptorProto")
	for len(b) > 0 {
		n := 16
		if n > len(b) {
			n = len(b)
		}

		s := ""
		for _, c := range b[:n] {
			s += fmt.Sprintf("0x%02x,", c)
		}
		t.P(`	`, s)

		b = b[n:]
	}
	t.P("}")
}

func (t *psrpc) printComments(comments typemap.DefinitionComments) bool {
	text := strings.TrimSuffix(comments.Leading, "\n")
	if len(strings.TrimSpace(text)) == 0 {
		return false
	}
	split := strings.Split(text, "\n")
	for _, line := range split {
		t.P("// ", strings.TrimPrefix(line, " "))
	}
	return len(split) > 0
}

// Given a protobuf name for a Message, return the Go name we will use for that
// type, including its package prefix.
func (t *psrpc) goTypeName(protoName string) string {
	def := t.reg.MessageDefinition(protoName)
	if def == nil {
		gen.Fail("could not find message for", protoName)
	}

	var prefix string
	if pkg := t.goPackageName(def.File); pkg != t.genPkgName {
		prefix = pkg + "."
	}

	var name string
	for _, parent := range def.Lineage() {
		name += stringutils.CamelCase(parent.Descriptor.GetName()) + "_"
	}
	name += stringutils.CamelCase(def.Descriptor.GetName())
	return prefix + name
}

func (t *psrpc) goPackageName(file *descriptor.FileDescriptorProto) string {
	return t.fileToGoPackageName[file]
}

func (t *psrpc) formattedOutput() string {
	// Reformat generated code.
	fset := token.NewFileSet()
	raw := t.output.Bytes()
	ast, err := parser.ParseFile(fset, "", raw, parser.ParseComments)
	if err != nil {
		// Print out the bad code with line numbers.
		// This should never happen in practice, but it can while changing generated code,
		// so consider this a debugging aid.
		var src bytes.Buffer
		s := bufio.NewScanner(bytes.NewReader(raw))
		for line := 1; s.Scan(); line++ {
			_, _ = fmt.Fprintf(&src, "%5d\t%s\n", line, s.Bytes())
		}
		gen.Fail("bad Go source code was generated:", err.Error(), "\n"+src.String())
	}

	out := bytes.NewBuffer(nil)
	err = (&printer.Config{Mode: printer.TabIndent | printer.UseSpaces, Tabwidth: 8}).Fprint(out, fset, ast)
	if err != nil {
		gen.Fail("generated Go source code could not be reformatted:", err.Error())
	}

	return out.String()
}

func unexported(s string) string { return strings.ToLower(s[:1]) + s[1:] }

func serviceNameCamelCased(service *descriptor.ServiceDescriptorProto) string {
	return stringutils.CamelCase(service.GetName())
}

func serviceStruct(service *descriptor.ServiceDescriptorProto) string {
	return unexported(serviceNameCamelCased(service)) + "Server"
}

func methodNameCamelCased(method *descriptor.MethodDescriptorProto) string {
	return stringutils.CamelCase(method.GetName())
}

func fileDescSliceContains(slice []*descriptor.FileDescriptorProto, f *descriptor.FileDescriptorProto) bool {
	for _, sf := range slice {
		if f == sf {
			return true
		}
	}
	return false
}
