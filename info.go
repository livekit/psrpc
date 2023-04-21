package psrpc

type RPCInfo struct {
	Service string
	Method  string
	Topic   []string
	Multi   bool
}
