// Copyright 2025 LiveKit, Inc.
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

package bus

type BusOption func(*BusOpts)

type BusOpts struct {
	Compression CompressionOpts
}

// CompressionOpts applies to publishing only. Decompression is driven by a flag
// on the wire, so a reader needs no configuration.
type CompressionOpts struct {
	// Zero disables compression; otherwise the gzip level.
	Quality int
	// Payload bytes below which compression is skipped.
	Threshold int
	// Caps an inbound payload after decompression, bounding amplification from a
	// hostile publisher. Zero is unlimited.
	MaxDecompressedSize int
}

func WithBusCompression(c CompressionOpts) BusOption {
	return func(o *BusOpts) {
		o.Compression = c
	}
}

func getBusOpts(opts ...BusOption) BusOpts {
	o := BusOpts{}
	for _, opt := range opts {
		opt(&o)
	}
	// Seeded after the options run: setting only Quality leaves Threshold zero.
	if o.Compression.Threshold <= 0 {
		o.Compression.Threshold = DefaultCompressionThreshold
	}
	return o
}
