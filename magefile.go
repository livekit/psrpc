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

// regenerate protobuf
func Proto() error {
	fmt.Println("generating protobuf")
	target := "internal"
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}

	protoc, err := mageutil.GetToolPath("protoc")
	if err != nil {
		return err
	}
	protocGoPath, err := mageutil.GetToolPath("protoc-gen-go")
	if err != nil {
		return err
	}

	cmd := exec.Command(protoc,
		"--go_out", target,
		"--go-grpc_out", target,
		"--go_opt=paths=source_relative",
		"--go-grpc_opt=paths=source_relative",
		"--plugin=go="+protocGoPath,
		"-I=.",
		"internal.proto",
	)
	mageutil.ConnectStd(cmd)
	return cmd.Run()
}

func Test() error {
	return mageutil.Run(context.Background(), "go test -v ./...")
}
