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
	"fmt"
	"io"
	"sync"

	"github.com/klauspost/compress/gzip"

	"github.com/livekit/psrpc/internal"
)

// Readers carry no level, so one pool serves every compressor.
var gzipReaders = sync.Pool{
	New: func() any { return new(gzip.Reader) },
}

type compressor struct {
	compression internal.Compression
	threshold   int
	writers     sync.Pool
	buffers     sync.Pool
}

func newCompressor(o CompressionOpts) *compressor {
	if o.Quality <= 0 {
		return nil
	}
	// Clamped, so NewWriterLevel below cannot fail.
	level := min(o.Quality, gzip.BestCompression)

	c := &compressor{
		compression: internal.Compression_COMPRESSION_GZIP,
		threshold:   o.Threshold,
	}
	c.buffers.New = func() any { return new(bytes.Buffer) }
	// Level is fixed at construction and survives Reset, so the pool is per-compressor.
	c.writers.New = func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, level)
		return w
	}
	return c
}

// The returned buffer is pooled: its bytes are valid only until release.
func (c *compressor) compress(src []byte) *bytes.Buffer {
	if len(src) < c.threshold {
		return nil
	}

	w := c.writers.Get().(*gzip.Writer)
	defer c.writers.Put(w)

	buf := c.buffers.Get().(*bytes.Buffer)
	buf.Reset()
	w.Reset(buf)

	if _, err := w.Write(src); err != nil {
		c.release(buf)
		return nil
	}
	if err := w.Close(); err != nil {
		c.release(buf)
		return nil
	}

	// gzip expands incompressible data.
	if buf.Len() >= len(src) {
		c.release(buf)
		return nil
	}
	return buf
}

func (c *compressor) release(buf *bytes.Buffer) {
	buf.Reset()
	c.buffers.Put(buf)
}

func decompress(src []byte, compression internal.Compression, maxSize int) ([]byte, error) {
	switch compression {
	case internal.Compression_COMPRESSION_NONE:
		return src, nil
	case internal.Compression_COMPRESSION_GZIP:
		return gunzip(src, maxSize)
	default:
		return nil, fmt.Errorf("psrpc: unrecognized message compression %d", compression)
	}
}

func gunzip(src []byte, maxSize int) ([]byte, error) {
	r := gzipReaders.Get().(*gzip.Reader)
	defer gzipReaders.Put(r)

	if err := r.Reset(bytes.NewReader(src)); err != nil {
		return nil, err
	}

	var lr io.Reader = r
	if maxSize > 0 {
		lr = io.LimitReader(r, int64(maxSize)+1)
	}

	out, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if maxSize > 0 && len(out) > maxSize {
		return nil, fmt.Errorf("psrpc: decompressed message exceeds %d bytes", maxSize)
	}
	return out, nil
}
