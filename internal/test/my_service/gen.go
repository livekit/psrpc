package my_service

//go:generate protoc --go_out=paths=source_relative:. --psrpc_out=paths=source_relative:. -I ../../../protoc-gen-psrpc/options -I=. my_service.proto
