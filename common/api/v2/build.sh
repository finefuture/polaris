#!/bin/bash

# 安装protoc和protoc-gen-go插件
#
# 注意：
# grpc包引入github.com/golang/protobuf/proto v1.2.0
# protoc-gen-go插件和引入proto包的版本必须保持一致
#
# github.com/golang/protobuf/
#   protoc-gen-go：在pb.go文件中插入proto.ProtoPackageIsVersionX
#   proto：在lib.go中定义ProtoPackageIsVersionX
#
# ProtoPackageIsVersion并非表示proto2/proto3

CURRENT_OS=$(uname -s)
CURRENT_ARCH=$(uname -m)
PROTOC=../protoc
PROTO_FILES="model_v2.proto routing_v2.proto request_v2.proto response_v2.proto grpcapi_v2.proto"

if [ "$CURRENT_ARCH" != "x86_64" ]; then
    echo "Current only support x86_64"
    exit 1
fi

if [ "$CURRENT_OS" == "Linux" ]; then
    ${PROTOC}/bin/protoc \
    --plugin=protoc-gen-go=${PROTOC}/bin/protoc-gen-go \
    --go_out=plugins=grpc:. \
    --proto_path=${PROTOC}/include \
    --proto_path=. \
    ${PROTO_FILES}

    ${PROTOC}/bin/protoc-go-inject-tag -input="*.pb.go"
else
    docker run --rm -it -v "$(dirname $(pwd))":/app --workdir /app/v2 debian:buster ./build.sh
fi