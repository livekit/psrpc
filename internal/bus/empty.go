package bus

type EmptySubscription[MessageType any] struct{}

func (s EmptySubscription[MessageType]) Channel() <-chan MessageType {
	return nil
}

func (s EmptySubscription[MessageType]) Close() error {
	return nil
}
