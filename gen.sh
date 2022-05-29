set -ex

protoc -I=./bpfd/proto --go_out=paths=source_relative:./clients/gobpfd ./bpfd/proto/bpfd.proto
protoc -I=./bpfd/proto --go-grpc_out=./clients/gobpfd --go-grpc_opt=paths=source_relative ./bpfd/proto/bpfd.proto
