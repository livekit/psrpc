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

//go:build mage
// +build mage

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/livekit/mageutil"
)

// goToolPath builds a protoc plugin from the tool directives in go.mod and returns its
// path, so generation uses the pinned version rather than whatever happens to be on PATH.
func goToolPath(name string) (string, error) {
	out, err := exec.Command("go", "tool", "-n", name).Output()
	if err != nil {
		return "", fmt.Errorf("resolving tool %s: %w", name, err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("resolving tool %s: no path returned", name)
	}
	return path, nil
}

var Default = Test

func Install() error {
	return mageutil.Run(context.Background(), "go install ./protoc-gen-psrpc")
}

func Proto() error {
	fmt.Println("generating protobuf")

	protoc, err := mageutil.GetToolPath("protoc")
	if err != nil {
		return err
	}
	protocGoPath, err := goToolPath("protoc-gen-go")
	if err != nil {
		return err
	}

	protos := []struct {
		importPath, outputPath, filename string
	}{
		{"./internal", "internal", "internal.proto"},
		{"./protoc-gen-psrpc/options", "protoc-gen-psrpc/options", "options.proto"},
		{"./testutils", "testutils", "testutils.proto"},
	}
	for _, p := range protos {
		cmd := exec.Command(protoc,
			"--go_out", p.outputPath,
			"--go_opt=paths=source_relative",
			"--plugin=protoc-gen-go="+protocGoPath,
			"-I="+p.importPath,
			p.filename,
		)
		mageutil.ConnectStd(cmd)
		if err = cmd.Run(); err != nil {
			return err
		}
	}

	return nil
}

func Generate() error {
	ctx := context.Background()

	// protoc-gen-psrpc is deliberately built from this tree rather than pinned: these
	// fixtures exist to exercise the plugin as it currently is.
	err := mageutil.Run(ctx, "go install ./protoc-gen-psrpc")
	if err != nil {
		return err
	}

	// The go:generate lines below invoke protoc directly, so they pick their plugins off
	// PATH. Put the pinned protoc-gen-go in front, so the fixtures are generated with the
	// version in go.mod rather than whatever a contributor happens to have installed.
	protocGoPath, err := goToolPath("protoc-gen-go")
	if err != nil {
		return err
	}
	newPath := filepath.Dir(protocGoPath) + string(os.PathListSeparator) + os.Getenv("PATH")
	if err := os.Setenv("PATH", newPath); err != nil {
		return err
	}

	base := "./internal/test"
	dirs, err := os.ReadDir(base)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if dir.IsDir() {
			err = mageutil.RunDir(ctx, fmt.Sprintf("%s/%s", base, dir.Name()), "go generate .")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func Test() error {
	return mageutil.Run(context.Background(), "go test -count=1 -v . ./internal/test")
}

func TestAll() error {
	if err := Generate(); err != nil {
		return err
	}
	return mageutil.Run(context.Background(), "go test -count=1 ./...")
}
