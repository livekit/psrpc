//go:build !go1.20

package psrpc

func (e psrpcError) Unwrap() error {
	return e.error
}
