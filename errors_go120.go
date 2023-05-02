//go:build go1.20

package psrpc

func (e psrpcError) Unwrap() []error {
	return []error{e.error, e.code}
}
