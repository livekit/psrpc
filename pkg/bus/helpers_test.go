package bus

// These helpers are only exposed during tests.

func GetBusOpts(opts ...BusOption) BusOpts {
	return getBusOpts(opts...)
}
