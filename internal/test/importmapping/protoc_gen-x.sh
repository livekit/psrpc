#!/usr/bin/env bash
# Copyright 2023 LiveKit, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


set -euo pipefail

# Pull into variable since both protoc-gen-go and protoc-gen-psrpc need the map.
Y_IMPORT_MAPPING="y/y.proto=github.com/livekit/psrpc/internal/test/importmapping/y"

PROTOC_GEN_GO_PARAMS="M${Y_IMPORT_MAPPING}" \
PROTOC_GEN_PSRPC_PARAMS="go_import_mapping@${Y_IMPORT_MAPPING}" \
../../protoc_gen.sh x/x.proto
