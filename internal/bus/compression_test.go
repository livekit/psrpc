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

import (
	"bytes"
	"crypto/rand"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
)

func testCompressor(quality, threshold int) *compressor {
	return newCompressor(CompressionOpts{Quality: quality, Threshold: threshold})
}

func TestNewCompressorDisabled(t *testing.T) {
	require.Nil(t, newCompressor(CompressionOpts{}))
	require.Nil(t, newCompressor(CompressionOpts{Threshold: 1}))
	require.NotNil(t, testCompressor(1, 1))
}

func TestCompressRoundTrip(t *testing.T) {
	src := []byte(strings.Repeat("psrpc compresses well ", 200))

	for _, quality := range []int{1, 5, 9} {
		c := testCompressor(quality, 1)
		buf := c.compress(src)
		require.NotNil(t, buf, "quality %d should compress a repetitive payload", quality)
		require.Less(t, buf.Len(), len(src))

		out, err := decompress(buf.Bytes(), internal.Compression_COMPRESSION_GZIP, 0)
		require.NoError(t, err)
		require.Equal(t, src, out)
		c.release(buf)
	}
}

func TestCompressSkipsBelowThreshold(t *testing.T) {
	src := []byte(strings.Repeat("a", 100))
	require.Nil(t, testCompressor(6, 1024).compress(src))
	require.NotNil(t, testCompressor(6, 100).compress(src))
}

func TestCompressDeclinesIncompressible(t *testing.T) {
	src := make([]byte, 4096)
	_, err := rand.Read(src)
	require.NoError(t, err)

	require.Nil(t, testCompressor(9, 1).compress(src))
}

func TestDecompressMaxSize(t *testing.T) {
	src := []byte(strings.Repeat("b", 8192))
	c := testCompressor(6, 1)
	buf := c.compress(src)
	require.NotNil(t, buf)
	defer c.release(buf)

	_, err := decompress(buf.Bytes(), internal.Compression_COMPRESSION_GZIP, 1024)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")

	out, err := decompress(buf.Bytes(), internal.Compression_COMPRESSION_GZIP, 8192)
	require.NoError(t, err)
	require.Equal(t, src, out)
}

func TestDecompressDispatch(t *testing.T) {
	src := []byte("uncompressed")
	out, err := decompress(src, internal.Compression_COMPRESSION_NONE, 0)
	require.NoError(t, err)
	require.Equal(t, src, out)

	_, err = decompress(src, internal.Compression(99), 0)
	require.ErrorContains(t, err, "unrecognized message compression")
}

func TestDecompressRejectsGarbage(t *testing.T) {
	_, err := decompress([]byte("not gzip at all"), internal.Compression_COMPRESSION_GZIP, 0)
	require.Error(t, err)
}

// A missed Reset on a pooled writer or reader shows up as cross-talk between
// concurrent messages.
func TestCompressorPoolConcurrency(t *testing.T) {
	c := testCompressor(6, 1)

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			src := bytes.Repeat([]byte{byte(i)}, 512+i)
			for range 50 {
				buf := c.compress(src)
				require.NotNil(t, buf)
				out, err := decompress(buf.Bytes(), internal.Compression_COMPRESSION_GZIP, 0)
				c.release(buf)
				require.NoError(t, err)
				require.Equal(t, src, out)
			}
		}(i)
	}
	wg.Wait()
}

func BenchmarkSerialize(b *testing.B) {
	msg := &internal.Request{
		RequestId:  "reid",
		ClientId:   "clid",
		RawRequest: []byte(strings.Repeat("psrpc payload ", 400)),
	}

	for _, c := range []struct {
		name string
		c    *compressor
	}{
		{"plain", nil},
		{"gzip", testCompressor(6, 1024)},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				buf, err := serialize(msg, "channel", c.c)
				if err != nil {
					b.Fatal(err)
				}
				if _, err = deserialize(buf, 0); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestNewCompressorClampsQuality(t *testing.T) {
	c := testCompressor(99, 1)
	require.NotNil(t, c)

	src := []byte(strings.Repeat("clamped ", 200))
	buf := c.compress(src)
	require.NotNil(t, buf)
	defer c.release(buf)

	out, err := decompress(buf.Bytes(), internal.Compression_COMPRESSION_GZIP, 0)
	require.NoError(t, err)
	require.Equal(t, src, out)
}
