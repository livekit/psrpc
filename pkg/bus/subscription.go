// Copyright 2023 LiveKit, Inc.
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
	"sync"

	"google.golang.org/protobuf/proto"
)

type Subscription[MessageType proto.Message] interface {
	Channel() <-chan MessageType
	Close() error
}

type subscription[MessageType proto.Message] struct {
	sub        Reader
	unanswered func(subject string) MessageType
	c          chan MessageType
	done       chan struct{}
	complete   chan struct{}
	closeOnce  sync.Once
	closeErr   error
}

func newSubscription[MessageType proto.Message](sub Reader, size int, maxSize int) Subscription[MessageType] {
	return startSubscription[MessageType](sub, size, maxSize, nil)
}

func startSubscription[MessageType proto.Message](
	sub Reader,
	size int,
	maxSize int,
	unanswered func(subject string) MessageType,
) Subscription[MessageType] {
	s := &subscription[MessageType]{
		sub:        sub,
		unanswered: unanswered,
		c:          make(chan MessageType, size),
		done:       make(chan struct{}),
		complete:   make(chan struct{}),
	}
	go s.forward(maxSize)
	return s
}

func (s *subscription[MessageType]) forward(maxSize int) {
	defer close(s.complete)
	defer close(s.c)

	read := func() ([]byte, string, bool) {
		b, ok := s.sub.Read()
		return b, "", ok
	}
	if ir, ok := s.sub.(InboxReader); ok && s.unanswered != nil {
		read = ir.ReadInbox
	}

	for {
		b, subject, ok := read()
		if !ok {
			return
		}

		var msg MessageType
		if len(b) == 0 && subject != "" {
			msg = s.unanswered(subject)
		} else {
			p, err := Deserialize(b, maxSize)
			if err != nil {
				continue
			}
			if msg, ok = p.(MessageType); !ok {
				continue
			}
		}

		select {
		case s.c <- msg:
		case <-s.done:
			return
		}
	}
}

func (s *subscription[MessageType]) Channel() <-chan MessageType {
	return s.c
}

func (s *subscription[MessageType]) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.closeErr = s.sub.Close()
		<-s.complete
	})
	return s.closeErr
}
