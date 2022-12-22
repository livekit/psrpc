//go:build mage
// +build mage

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/livekit/mageutil"
)

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
	protocGoPath, err := mageutil.GetToolPath("protoc-gen-go")
	if err != nil {
		return err
	}

	cmd := exec.Command(protoc,
		"--go_out", "internal",
		"--go_opt=paths=source_relative",
		"--plugin=go="+protocGoPath,
		"-I=./internal",
		"internal.proto",
	)
	mageutil.ConnectStd(cmd)
	if err = cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command(protoc,
		"--go_out", "protoc-gen-psrpc/options",
		"--go_opt=paths=source_relative",
		"--plugin=go="+protocGoPath,
		"-I=./protoc-gen-psrpc/options",
		"options.proto",
	)
	mageutil.ConnectStd(cmd)
	if err = cmd.Run(); err != nil {
		return err
	}

	return nil
}

func Generate() error {
	ctx := context.Background()

	err := mageutil.Run(ctx, "go install ./protoc-gen-psrpc")
	if err != nil {
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
	return mageutil.Run(context.Background(), "go test -v .")
}

func TestAll() error {
	if err := Generate(); err != nil {
		return err
	}
	return mageutil.Run(context.Background(), "go test ./...")
}
