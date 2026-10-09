// Command amqpbench verifies a RabbitMQ/AMQP instance against the requirements
// of the psrpc AMQP bus (feasibility items A and C).
//
// Usage: amqpbench [-url URL] <command> [flags]
// AMQP_URL env var is used when -url is not given.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

var rnd = rand.New(rand.NewSource(time.Now().UnixNano()))

func main() {
	url := flag.String("url", os.Getenv("AMQP_URL"), "AMQP URL (default $AMQP_URL)")
	flag.Usage = usage
	flag.Parse()
	if *url == "" {
		fmt.Fprintln(os.Stderr, "error: no -url or $AMQP_URL given")
		usage()
		os.Exit(2)
	}
	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	var err error
	switch args[0] {
	case "info":
		err = cmdInfo(*url)
	case "size":
		err = cmdSize(*url, args[1:])
	case "queues":
		err = cmdQueues(*url, args[1:])
	case "conns":
		err = cmdConns(*url, args[1:])
	case "churn":
		err = cmdChurn(*url, args[1:])
	case "quorum":
		err = cmdQuorum(*url)
	case "latency":
		err = cmdLatency(*url, args[1:])
	case "throughput":
		err = cmdThroughput(*url, args[1:])
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: amqpbench [-url URL] <command> [flags]

  info                          broker version, cluster name            (A)
  size    [-bytes N]            publish+consume an N-byte message       (A4)
  queues  [-n N] [-c W]         declare N queues concurrently           (A1)
  conns   [-n N]                open N connections                      (A3)
  churn   [-rate R] [-d DUR] [-c W]
                                sustained subscribe cycles at R/s       (A2)
  quorum                        quorum queue support                    (A5)
  latency [-n N]                one-way publish->deliver latency        (C)
  throughput [-c C] [-qps Q] [-d DUR]
                                sustained rate with competing consumers (C)
`)
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.Usage = usage
	return fs
}

func dial(url string) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, ch, nil
}

func percentiles(ds []time.Duration) (p50, p95, p99, max time.Duration) {
	if len(ds) == 0 {
		return
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	pick := func(q float64) time.Duration { return s[int(float64(len(s)-1)*q)] }
	return pick(0.50), pick(0.95), pick(0.99), s[len(s)-1]
}

func cmdInfo(url string) error {
	conn, err := amqp.Dial(url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	p := conn.Properties // server properties advertised at dial time
	fmt.Printf("broker.product:     %v\n", p["product"])
	fmt.Printf("broker.version:     %v\n", p["version"])
	fmt.Printf("broker.platform:    %v\n", p["platform"])
	fmt.Printf("broker.cluster:     %v\n", p["cluster_name"])
	if caps, ok := p["capabilities"].(amqp.Table); ok {
		fmt.Printf("broker.capabilities: per_consumer_qos=%v exchange_exchange_bindings=%v basic.nack=%v\n",
			caps["per_consumer_qos"], caps["exchange_exchange_bindings"], caps["basic.nack"])
	}
	return nil
}

func cmdSize(url string, args []string) error {
	fs := newFlagSet("size")
	bytesN := fs.Int("bytes", 1024*1024, "message body size in bytes")
	fanout := fs.Bool("fanout", false, "route through a fanout exchange, like the psrpc bus does")
	if err := fs.Parse(args); err != nil {
		return err
	}

	conn, ch, err := dial(url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return err
	}
	route := q.Name
	if *fanout {
		ex := fmt.Sprintf("amqpbench-size-ex-%d", rnd.Int63())
		if err = ch.ExchangeDeclare(ex, "fanout", false, true, false, false, nil); err != nil {
			return err
		}
		if err = ch.QueueBind(q.Name, ex, ex, false, nil); err != nil {
			return err
		}
		route = ex
	}
	body := make([]byte, *bytesN)
	for i := range body {
		body[i] = byte(rnd.Intn(256))
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ch.PublishWithContext(ctx, route, "", false, false, amqp.Publishing{Body: body}); err != nil {
		return fmt.Errorf("publish %d bytes: %w", *bytesN, err)
	}
	deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		return err
	}
	select {
	case msg, ok := <-deliveries:
		if !ok {
			return errors.New("delivery channel closed")
		}
		fmt.Printf("size.roundtrip:     %v\n", time.Since(start))
		fmt.Printf("size.received:      %d bytes (expected %d)\n", len(msg.Body), *bytesN)
		if len(msg.Body) != *bytesN {
			return errors.New("message was truncated or altered")
		}
	case <-time.After(10 * time.Second):
		return errors.New("timed out waiting for delivery")
	}
	fmt.Println("size.result:        PASS")
	return nil
}

func cmdQueues(url string, args []string) error {
	fs := newFlagSet("queues")
	n := fs.Int("n", 1000, "number of queues to declare")
	w := fs.Int("c", 20, "concurrent workers")
	if err := fs.Parse(args); err != nil {
		return err
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var next atomic.Int64
	var ok atomic.Int64
	var firstErr atomic.Value
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < *w; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, err := conn.Channel()
			if err != nil {
				firstErr.Store(err)
				return
			}
			defer func() { _ = ch.Close() }()
			for {
				if int(next.Add(1)) > *n {
					return
				}
				// Exclusive auto-delete queues: vanish with the connection,
				// no manual cleanup needed.
				if _, err := ch.QueueDeclare(fmt.Sprintf("amqpbench-q-%d", rnd.Int63()), false, true, true, false, nil); err != nil {
					firstErr.Store(err)
					return
				}
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if err, _ := firstErr.Load().(error); err != nil {
		return err
	}
	elapsed := time.Since(start)
	fmt.Printf("queues.declared:    %d/%d in %v (%.0f/s)\n", ok.Load(), *n, elapsed, float64(ok.Load())/elapsed.Seconds())
	fmt.Println("queues.result:      PASS")
	return nil
}

func cmdConns(url string, args []string) error {
	fs := newFlagSet("conns")
	n := fs.Int("n", 50, "number of connections")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var conns []*amqp.Connection
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	start := time.Now()
	for i := 0; i < *n; i++ {
		c, err := amqp.Dial(url)
		if err != nil {
			return fmt.Errorf("connection %d/%d: %w", i+1, *n, err)
		}
		conns = append(conns, c)
	}
	fmt.Printf("conns.opened:       %d in %v\n", *n, time.Since(start))
	fmt.Println("conns.result:       PASS")
	return nil
}

// cmdChurn hammers the exact cycle the psrpc bus performs per subscription:
// open channel, declare fanout exchange, declare exclusive auto-delete queue,
// bind, consume, close. Auto-delete resources vanish with each cycle.
func cmdChurn(url string, args []string) error {
	fs := newFlagSet("churn")
	rate := fs.Int("rate", 10, "subscribe cycles per second")
	dur := fs.Duration("d", time.Minute, "test duration")
	w := fs.Int("c", 5, "concurrent workers")
	if err := fs.Parse(args); err != nil {
		return err
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	tokens := make(chan struct{}, *rate)
	go func() {
		defer close(tokens)
		t := time.NewTicker(time.Second / time.Duration(*rate))
		defer t.Stop()
		deadline := time.After(*dur)
		for {
			select {
			case <-t.C:
				tokens <- struct{}{}
			case <-deadline:
				return
			}
		}
	}()

	var mu sync.Mutex
	var lats []time.Duration
	var ok, fail atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < *w; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range tokens {
				start := time.Now()
				err := subscribeCycle(conn)
				mu.Lock()
				lats = append(lats, time.Since(start))
				mu.Unlock()
				if err != nil {
					fail.Add(1)
				} else {
					ok.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	p50, p95, p99, mx := percentiles(lats)
	fmt.Printf("churn.cycles:       %d ok, %d failed (target %d/s for %v)\n", ok.Load(), fail.Load(), *rate, *dur)
	fmt.Printf("churn.p50:          %v\n", p50)
	fmt.Printf("churn.p95:          %v\n", p95)
	fmt.Printf("churn.p99:          %v\n", p99)
	fmt.Printf("churn.max:          %v\n", mx)
	if fail.Load() > 0 {
		return fmt.Errorf("%d churn cycles failed", fail.Load())
	}
	fmt.Println("churn.result:       PASS")
	return nil
}

func subscribeCycle(conn *amqp.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()

	ex := fmt.Sprintf("amqpbench-ex-%d", rnd.Int63())
	if err = ch.ExchangeDeclare(ex, "fanout", false, true, false, false, nil); err != nil {
		return err
	}
	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return err
	}
	if err = ch.QueueBind(q.Name, ex, ex, false, nil); err != nil {
		return err
	}
	if _, err = ch.Consume(q.Name, "", true, false, false, false, nil); err != nil {
		return err
	}
	return nil
}

func cmdQuorum(url string) error {
	conn, ch, err := dial(url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	name := fmt.Sprintf("amqpbench-quorum-%d", rnd.Int63())
	defer func() { _, _ = ch.QueueDelete(name, false, false, false) }()

	// Quorum queues must be durable; delete explicitly afterwards.
	if _, err = ch.QueueDeclare(name, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		fmt.Printf("quorum.supported:   NO (%v)\n", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = ch.PublishWithContext(ctx, "", name, false, false, amqp.Publishing{Body: []byte("probe")}); err != nil {
		return fmt.Errorf("publish to quorum queue: %w", err)
	}
	deliveries, err := ch.Consume(name, "", true, false, false, false, nil)
	if err != nil {
		return err
	}
	select {
	case <-deliveries:
		fmt.Println("quorum.supported:   YES")
	case <-time.After(10 * time.Second):
		return errors.New("timed out waiting for quorum queue delivery")
	}
	return nil
}

func cmdLatency(url string, args []string) error {
	fs := newFlagSet("latency")
	n := fs.Int("n", 1000, "number of messages")
	if err := fs.Parse(args); err != nil {
		return err
	}

	conn, ch, err := dial(url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return err
	}
	deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		return err
	}
	// Publish on a separate channel: consuming and publishing on the same
	// channel interacts with broker-side batching and inflated earlier
	// measurements.
	pub, err := conn.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = pub.Close() }()

	var mu sync.Mutex
	var lats []time.Duration
	go func() {
		for msg := range deliveries {
			if len(msg.Body) >= 8 {
				send := int64(binary.BigEndian.Uint64(msg.Body[:8]))
				mu.Lock()
				lats = append(lats, time.Since(time.Unix(0, send)))
				mu.Unlock()
			}
		}
	}()

	buf := make([]byte, 72)
	for i := 0; i < *n; i++ {
		binary.BigEndian.PutUint64(buf, uint64(time.Now().UnixNano()))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = pub.PublishWithContext(ctx, "", q.Name, false, false, amqp.Publishing{Body: buf})
		cancel()
		if err != nil {
			return err
		}
	}
	deadline := time.After(30 * time.Second)
	for {
		mu.Lock()
		got := len(lats)
		mu.Unlock()
		if got >= *n {
			break
		}
		select {
		case <-deadline:
			return fmt.Errorf("timed out: received %d/%d", got, *n)
		case <-time.After(10 * time.Millisecond):
		}
	}

	mu.Lock()
	p50, p95, p99, mx := percentiles(lats)
	mu.Unlock()
	fmt.Printf("latency.n:          %d\n", len(lats))
	fmt.Printf("latency.p50:        %v\n", p50)
	fmt.Printf("latency.p95:        %v\n", p95)
	fmt.Printf("latency.p99:        %v\n", p99)
	fmt.Printf("latency.max:        %v\n", mx)
	fmt.Println("latency.result:     PASS")
	return nil
}

func cmdThroughput(url string, args []string) error {
	fs := newFlagSet("throughput")
	clients := fs.Int("c", 8, "competing consumers / publishers")
	qps := fs.Int("qps", 200, "target publish rate (msg/s)")
	dur := fs.Duration("d", 15*time.Second, "test duration")
	if err := fs.Parse(args); err != nil {
		return err
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	// Shared non-exclusive queue with C competing consumers, modeling the
	// bus's SubscribeQueue path.
	name := fmt.Sprintf("amqpbench-tp-%d", rnd.Int63())
	setup, err := conn.Channel()
	if err != nil {
		return err
	}
	if _, err = setup.QueueDeclare(name, false, true, false, false, nil); err != nil {
		return err
	}
	if err = setup.QueueBind(name, name, "amq.direct", false, nil); err != nil {
		return err
	}

	var mu sync.Mutex
	var lats []time.Duration
	var recv atomic.Int64
	var wg sync.WaitGroup
	var consumerChans []*amqp.Channel
	for i := 0; i < *clients; i++ {
		cc, err := conn.Channel()
		if err != nil {
			return err
		}
		consumerChans = append(consumerChans, cc)
		wg.Add(1)
		go func(ch *amqp.Channel) {
			defer wg.Done()
			deliveries, err := ch.Consume(name, "", true, false, false, false, nil)
			if err != nil {
				return
			}
			for msg := range deliveries {
				if len(msg.Body) >= 8 {
					send := int64(binary.BigEndian.Uint64(msg.Body[:8]))
					mu.Lock()
					lats = append(lats, time.Since(time.Unix(0, send)))
					mu.Unlock()
				}
				recv.Add(1)
			}
		}(cc)
	}

	tokens := make(chan struct{}, *qps)
	go func() {
		defer close(tokens)
		t := time.NewTicker(time.Second / time.Duration(*qps))
		defer t.Stop()
		deadline := time.After(*dur)
		for {
			select {
			case <-t.C:
				tokens <- struct{}{}
			case <-deadline:
				return
			}
		}
	}()

	var sent atomic.Int64
	var pubErr atomic.Value
	start := time.Now()
	var wgPub sync.WaitGroup
	for i := 0; i < *clients; i++ {
		wgPub.Add(1)
		go func() {
			defer wgPub.Done()
			pc, err := conn.Channel()
			if err != nil {
				pubErr.Store(err)
				return
			}
			defer func() { _ = pc.Close() }()
			buf := make([]byte, 72)
			for range tokens {
				binary.BigEndian.PutUint64(buf, uint64(time.Now().UnixNano()))
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err = pc.PublishWithContext(ctx, "amq.direct", name, false, false, amqp.Publishing{Body: buf})
				cancel()
				if err != nil {
					pubErr.Store(err)
					return
				}
				sent.Add(1)
			}
		}()
	}
	wgPub.Wait()
	elapsed := time.Since(start)
	time.Sleep(2 * time.Second) // drain in-flight messages
	for _, cc := range consumerChans {
		_ = cc.Close()
	}
	wg.Wait()

	if err, _ := pubErr.Load().(error); err != nil {
		return err
	}
	p50, p95, p99, mx := percentiles(lats)
	fmt.Printf("throughput.target:  %d msg/s for %v\n", *qps, *dur)
	fmt.Printf("throughput.sent:    %d (%.1f/s)\n", sent.Load(), float64(sent.Load())/elapsed.Seconds())
	fmt.Printf("throughput.received:%d (%.1f/s)\n", recv.Load(), float64(recv.Load())/elapsed.Seconds())
	fmt.Printf("throughput.p50:     %v\n", p50)
	fmt.Printf("throughput.p95:     %v\n", p95)
	fmt.Printf("throughput.p99:     %v\n", p99)
	fmt.Printf("throughput.max:     %v\n", mx)
	if recv.Load() < sent.Load() {
		return fmt.Errorf("lost messages: sent %d, received %d", sent.Load(), recv.Load())
	}
	fmt.Println("throughput.result:  PASS")
	return nil
}
