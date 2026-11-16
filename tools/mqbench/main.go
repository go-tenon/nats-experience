// mqbench: NSQ vs NATS JetStream 作为持久化工作队列的压测工具。
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nsqio/go-nsq"
)

var (
	sysF      = flag.String("sys", "nsq", "nsq | js")
	modeF     = flag.String("mode", "pub", "pub | sub | e2e | crash | defer")
	nsqTCP    = flag.String("nsqd", "127.0.0.1:14150", "")
	nsqHTTP   = flag.String("nsqhttp", "127.0.0.1:14151", "")
	natsURL   = flag.String("nats", "nats://127.0.0.1:14222", "")
	topicF    = flag.String("topic", "bench", "")
	sizeF     = flag.Int("size", 1024, "message size")
	nF        = flag.Int("n", 100000, "messages")
	cF        = flag.Int("c", 16, "producers / consumer handlers")
	asyncF    = flag.Int("async", 0, "async publish window per producer (0 = sync)")
	inflightF = flag.Int("inflight", 1000, "consumer max in flight / max ack pending")
	rateF     = flag.Int("rate", 10000, "e2e/crash: total publish rate msg/s")
	durF      = flag.Duration("dur", 60*time.Second, "e2e duration")
	workF     = flag.Duration("work", 0, "crash: per-message handler time")
	delayF    = flag.Duration("delay", 60*time.Second, "defer: requeue delay")
	waitF     = flag.Duration("wait", 120*time.Second, "idle timeout before giving up")
	obtF      = flag.Duration("obt", 25*time.Millisecond, "nsq output_buffer_timeout (tenon 用 25ms)")
	maxAckF   = flag.Int("maxack", 0, "js MaxAckPending (0 = inflight)")
	setupF    = flag.Bool("setup", true, "create topic/channel or stream/consumer")
)

// ---------- broker abstraction ----------

type Msg interface {
	Data() []byte
	Ack()
	Nak(d time.Duration)
}

type Pub interface {
	Publish(b []byte) error
	PublishAsync(b []byte, done func(error))
	Close()
}

type Broker interface {
	Setup() error
	NewPub() (Pub, error)
	Consume(conc, inflight int, h func(Msg)) (stop func(), err error)
}

// ---------- NSQ ----------

type nsqBroker struct{}

func quietNSQ() (*log.Logger, nsq.LogLevel) {
	return log.New(os.Stderr, "nsq ", log.LstdFlags), nsq.LogLevelError
}

func (nsqBroker) Setup() error {
	for _, p := range []string{
		"/topic/create?topic=" + url.QueryEscape(*topicF),
		"/channel/create?topic=" + url.QueryEscape(*topicF) + "&channel=worker",
	} {
		resp, err := http.Post("http://"+*nsqHTTP+p, "", nil)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("%s: %s", p, resp.Status)
		}
	}
	return nil
}

type nsqPub struct {
	p    *nsq.Producer
	done chan *nsq.ProducerTransaction
}

func (nsqBroker) NewPub() (Pub, error) {
	cfg := nsq.NewConfig()
	p, err := nsq.NewProducer(*nsqTCP, cfg)
	if err != nil {
		return nil, err
	}
	p.SetLogger(quietNSQ())
	np := &nsqPub{p: p, done: make(chan *nsq.ProducerTransaction, 100000)}
	go func() {
		for t := range np.done {
			t.Args[0].(func(error))(t.Error)
		}
	}()
	return np, nil
}

func (p *nsqPub) Publish(b []byte) error { return p.p.Publish(*topicF, b) }
func (p *nsqPub) PublishAsync(b []byte, done func(error)) {
	if err := p.p.PublishAsync(*topicF, b, p.done, done); err != nil {
		done(err)
	}
}
func (p *nsqPub) Close() { p.p.Stop() }

type nsqMsg struct{ m *nsq.Message }

func (m nsqMsg) Data() []byte        { return m.m.Body }
func (m nsqMsg) Ack()                { m.m.Finish() }
func (m nsqMsg) Nak(d time.Duration) { m.m.RequeueWithoutBackoff(d) }

func (nsqBroker) Consume(conc, inflight int, h func(Msg)) (func(), error) {
	cfg := nsq.NewConfig()
	cfg.MaxInFlight = inflight
	cfg.MaxAttempts = 0
	cfg.LookupdPollInterval = time.Second // 直连 nsqd 时也是断线重连间隔
	cfg.MaxRequeueDelay = time.Hour
	cfg.OutputBufferTimeout = *obtF
	c, err := nsq.NewConsumer(*topicF, "worker", cfg)
	if err != nil {
		return nil, err
	}
	c.SetLogger(quietNSQ())
	c.AddConcurrentHandlers(nsq.HandlerFunc(func(m *nsq.Message) error {
		m.DisableAutoResponse()
		h(nsqMsg{m})
		return nil
	}), conc)
	if err := c.ConnectToNSQD(*nsqTCP); err != nil {
		return nil, err
	}
	return func() { c.Stop(); <-c.StopChan }, nil
}

// ---------- NATS JetStream ----------

type jsBroker struct{}

func natsConn() (*nats.Conn, jetstream.JetStream, error) {
	nc, err := nats.Connect(*natsURL, nats.MaxReconnects(-1), nats.ReconnectWait(500*time.Millisecond),
		nats.ReconnectBufSize(64<<20))
	if err != nil {
		return nil, nil, err
	}
	js, err := jetstream.New(nc)
	return nc, js, err
}

func (jsBroker) Setup() error {
	nc, js, err := natsConn()
	if err != nil {
		return err
	}
	defer nc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: "BENCH", Subjects: []string{*topicF}, Storage: jetstream.FileStorage,
		Retention: jetstream.WorkQueuePolicy, Replicas: 1,
	}); err != nil {
		return err
	}
	_, err = js.CreateOrUpdateConsumer(ctx, "BENCH", jetstream.ConsumerConfig{
		Durable: "worker", AckPolicy: jetstream.AckExplicitPolicy, AckWait: 30 * time.Second,
		MaxAckPending: maxAck(), MaxDeliver: -1,
	})
	return err
}

type jsPub struct {
	nc   *nats.Conn
	js   jetstream.JetStream
	futs chan jsFut
}
type jsFut struct {
	f    jetstream.PubAckFuture
	done func(error)
}

func (jsBroker) NewPub() (Pub, error) {
	nc, js, err := natsConn()
	if err != nil {
		return nil, err
	}
	if *asyncF > 0 {
		js, _ = jetstream.New(nc, jetstream.WithPublishAsyncMaxPending(*asyncF*2))
	}
	p := &jsPub{nc: nc, js: js, futs: make(chan jsFut, 100000)}
	go func() {
		for f := range p.futs {
			select {
			case <-f.f.Ok():
				f.done(nil)
			case err := <-f.f.Err():
				f.done(err)
			}
		}
	}()
	return p, nil
}

func (p *jsPub) Publish(b []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := p.js.Publish(ctx, *topicF, b)
	return err
}
func (p *jsPub) PublishAsync(b []byte, done func(error)) {
	f, err := p.js.PublishAsync(*topicF, b)
	if err != nil {
		done(err)
		return
	}
	p.futs <- jsFut{f, done}
}
func (p *jsPub) Close() { p.nc.Drain() }

type jsMsg struct{ m jetstream.Msg }

func (m jsMsg) Data() []byte        { return m.m.Data() }
func (m jsMsg) Ack()                { m.m.Ack() }
func (m jsMsg) Nak(d time.Duration) { m.m.NakWithDelay(d) }

func (jsBroker) Consume(conc, inflight int, h func(Msg)) (func(), error) {
	nc, js, err := natsConn()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	cons, err := js.Consumer(ctx, "BENCH", "worker")
	if err != nil {
		return nil, err
	}
	work := make(chan jetstream.Msg, inflight)
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range work {
				h(jsMsg{m})
			}
		}()
	}
	cc, err := cons.Consume(func(m jetstream.Msg) { work <- m },
		jetstream.PullMaxMessages(inflight),
		jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			log.Printf("consume err: %v", err)
		}))
	if err != nil {
		return nil, err
	}
	return func() { cc.Stop(); <-cc.Closed(); close(work); wg.Wait(); nc.Close() }, nil
}

// ---------- helpers ----------

func broker() Broker {
	if *sysF == "js" {
		return jsBroker{}
	}
	return nsqBroker{}
}

func body(seq uint64, ts int64) []byte {
	b := make([]byte, *sizeF)
	binary.LittleEndian.PutUint64(b, seq)
	binary.LittleEndian.PutUint64(b[8:], uint64(ts))
	for i := 16; i < len(b); i++ {
		b[i] = byte('a' + i%26)
	}
	return b
}

func newHist() *hdrhistogram.Histogram { return hdrhistogram.New(1, 120_000_000, 3) } // µs

func pct(h *hdrhistogram.Histogram) string {
	ms := func(v int64) float64 { return float64(v) / 1000 }
	return fmt.Sprintf("p50=%.2fms p99=%.2fms p999=%.2fms max=%.2fms",
		ms(h.ValueAtQuantile(50)), ms(h.ValueAtQuantile(99)), ms(h.ValueAtQuantile(99.9)), ms(h.Max()))
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func publishRetry(p Pub, b []byte, errs *atomic.Int64) {
	for {
		if err := p.Publish(b); err == nil {
			return
		}
		errs.Add(1)
		time.Sleep(100 * time.Millisecond)
	}
}

// ---------- modes ----------

func modePub(b Broker) {
	per := *nF / *cF
	var mu sync.Mutex
	h := newHist()
	var errs atomic.Int64
	var wg sync.WaitGroup
	pubs := make([]Pub, *cF)
	for i := range pubs {
		p, err := b.NewPub()
		must(err)
		must(p.Publish(body(0, 0))) // 预热连接
		pubs[i] = p
	}
	start := time.Now()
	for i := 0; i < *cF; i++ {
		wg.Add(1)
		go func(p Pub) {
			defer wg.Done()
			lh := newHist()
			msg := body(1, 0)
			if *asyncF > 0 {
				sem := make(chan struct{}, *asyncF)
				var pw sync.WaitGroup
				for j := 0; j < per; j++ {
					sem <- struct{}{}
					pw.Add(1)
					p.PublishAsync(msg, func(err error) {
						if err != nil {
							errs.Add(1)
						}
						<-sem
						pw.Done()
					})
				}
				pw.Wait()
				return
			}
			for j := 0; j < per; j++ {
				t0 := time.Now()
				if err := p.Publish(msg); err != nil {
					errs.Add(1)
				}
				lh.RecordValue(time.Since(t0).Microseconds())
			}
			mu.Lock()
			h.Merge(lh)
			mu.Unlock()
		}(pubs[i])
	}
	wg.Wait()
	el := time.Since(start)
	total := per * *cF
	rate := float64(total) / el.Seconds()
	lat := "async"
	if *asyncF == 0 {
		lat = pct(h)
	}
	fmt.Printf("RESULT pub sys=%s size=%d c=%d async=%d n=%d elapsed=%.1fs rate=%.0f msg/s %.1f MB/s errors=%d %s\n",
		*sysF, *sizeF, *cF, *asyncF, total, el.Seconds(), rate, rate*float64(*sizeF)/1e6, errs.Load(), lat)
	for _, p := range pubs {
		p.Close()
	}
}

func modeSub(b Broker) {
	var got atomic.Int64
	var last atomic.Int64
	done := make(chan struct{})
	var once sync.Once
	start := time.Now()
	stop, err := b.Consume(*cF, *inflightF, func(m Msg) {
		m.Ack()
		last.Store(time.Now().UnixNano())
		if got.Add(1) >= int64(*nF) {
			once.Do(func() { close(done) })
		}
	})
	must(err)
	tick := time.NewTicker(time.Second)
	for {
		select {
		case <-done:
		case <-tick.C:
			if l := last.Load(); l > 0 && time.Since(time.Unix(0, l)) > 10*time.Second {
				log.Printf("idle, stopping at %d", got.Load())
				goto out
			}
			continue
		}
		break
	}
out:
	el := time.Since(start)
	n := got.Load()
	rate := float64(n) / el.Seconds()
	fmt.Printf("RESULT sub sys=%s size=%d c=%d inflight=%d n=%d elapsed=%.1fs rate=%.0f msg/s %.1f MB/s\n",
		*sysF, *sizeF, *cF, *inflightF, n, el.Seconds(), rate, rate*float64(*sizeF)/1e6)
	stop()
}

// 按固定速率发布（以计划发送时间计时，避免协调遗漏），消费端统计端到端延迟。
func modeE2E(b Broker) {
	var mu sync.Mutex
	win, all := newHist(), newHist()
	var sent, recv, pubErr atomic.Int64
	stop, err := b.Consume(*cF, *inflightF, func(m Msg) {
		d := m.Data()
		ts := int64(binary.LittleEndian.Uint64(d[8:]))
		lat := (time.Now().UnixNano() - ts) / 1000
		m.Ack()
		mu.Lock()
		win.RecordValue(lat)
		all.RecordValue(lat)
		mu.Unlock()
		recv.Add(1)
	})
	must(err)
	interval := time.Duration(int64(time.Second) * int64(*cF) / int64(*rateF))
	start := time.Now()
	end := start.Add(*durF)
	var wg sync.WaitGroup
	for i := 0; i < *cF; i++ {
		p, err := b.NewPub()
		must(err)
		wg.Add(1)
		go func(i int, p Pub) {
			defer wg.Done()
			next := start.Add(interval * time.Duration(i) / time.Duration(*cF))
			for seq := uint64(0); ; seq++ {
				if next.After(end) {
					return
				}
				if d := time.Until(next); d > 0 {
					time.Sleep(d)
				}
				if err := p.Publish(body(seq, next.UnixNano())); err != nil {
					pubErr.Add(1)
				} else {
					sent.Add(1)
				}
				next = next.Add(interval)
			}
		}(i, p)
	}
	go func() {
		t := time.NewTicker(10 * time.Second)
		var ls, lr int64
		for range t.C {
			s, r := sent.Load(), recv.Load()
			mu.Lock()
			line := pct(win)
			win.Reset()
			mu.Unlock()
			fmt.Printf("WIN t=%3.0fs pub=%6.0f/s recv=%6.0f/s lag=%d pubErr=%d %s\n", time.Since(start).Seconds(),
				float64(s-ls)/10, float64(r-lr)/10, s-r, pubErr.Load(), line)
			ls, lr = s, r
		}
	}()
	wg.Wait()
	dl := time.Now().Add(30 * time.Second)
	for recv.Load() < sent.Load() && time.Now().Before(dl) {
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	fmt.Printf("RESULT e2e sys=%s size=%d rate=%d dur=%s sent=%d recv=%d pubErr=%d %s\n",
		*sysF, *sizeF, *rateF, *durF, sent.Load(), recv.Load(), pubErr.Load(), pct(all))
	mu.Unlock()
	stop()
}

// 发布带编号的消息（ack 成功才算已发布），消费端处理后 ack；期间由外部脚本 kill -9 服务端。
// 结束时统计：已确认发布但从未收到的（丢失）、重复投递的次数。
func modeCrash(b Broker) {
	n := *nF
	acked := make([]atomic.Bool, n)
	seen := make([]atomic.Int32, n)
	var pubErr, recv, ackedN atomic.Int64
	var lastRecv atomic.Int64
	stop, err := b.Consume(*cF, *inflightF, func(m Msg) {
		id := binary.LittleEndian.Uint64(m.Data())
		if *workF > 0 {
			time.Sleep(*workF)
		}
		seen[id].Add(1)
		recv.Add(1)
		lastRecv.Store(time.Now().UnixNano())
		m.Ack()
	})
	must(err)
	const producers = 8
	interval := time.Duration(int64(time.Second) * producers / int64(*rateF))
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < producers; i++ {
		p, err := b.NewPub()
		must(err)
		wg.Add(1)
		go func(i int, p Pub) {
			defer wg.Done()
			next := time.Now()
			for id := i; id < n; id += producers {
				if d := time.Until(next); d > 0 {
					time.Sleep(d)
				}
				publishRetry(p, body(uint64(id), 0), &pubErr)
				acked[id].Store(true)
				ackedN.Add(1)
				next = next.Add(interval)
			}
		}(i, p)
	}
	pubDone := make(chan struct{})
	go func() { wg.Wait(); close(pubDone) }()
	t := time.NewTicker(5 * time.Second)
	var pdone time.Time
	lastRecv.Store(time.Now().UnixNano())
loop:
	for {
		select {
		case <-pubDone:
			pdone = time.Now()
			pubDone = nil
		case <-t.C:
			fmt.Printf("PROG t=%3.0fs acked=%d recv=%d pubErr=%d\n", time.Since(start).Seconds(), ackedN.Load(), recv.Load(), pubErr.Load())
			if !pdone.IsZero() {
				uniq := 0
				for i := range seen {
					if seen[i].Load() > 0 {
						uniq++
					}
				}
				if uniq == n || time.Since(time.Unix(0, lastRecv.Load())) > *waitF {
					break loop
				}
			}
		}
	}
	lost, dup, uniq := 0, 0, 0
	var firstLost []int
	for i := 0; i < n; i++ {
		s := int(seen[i].Load())
		if s > 0 {
			uniq++
			dup += s - 1
		} else if acked[i].Load() {
			lost++
			if len(firstLost) < 5 {
				firstLost = append(firstLost, i)
			}
		}
	}
	fmt.Printf("RESULT crash sys=%s n=%d acked=%d recvUnique=%d lost=%d dupDeliveries=%d pubErr=%d sampleLost=%v\n",
		*sysF, n, ackedN.Load(), uniq, lost, dup, pubErr.Load(), firstLost)
	stop()
}

// 发布 n 条，首次投递时全部延迟 delay 重新投递；记录全部延迟完成的时间点，然后等待重投。
func modeDefer(b Broker) {
	n := *nF
	first := make([]atomic.Bool, n)
	var deferred, redelivered atomic.Int64
	var lastAct atomic.Int64
	start := time.Now()
	allDeferred := make(chan struct{})
	var once sync.Once
	stop, err := b.Consume(*cF, *inflightF, func(m Msg) {
		id := binary.LittleEndian.Uint64(m.Data())
		lastAct.Store(time.Now().UnixNano())
		if first[id].CompareAndSwap(false, true) {
			m.Nak(*delayF)
			if deferred.Add(1) == int64(n) {
				once.Do(func() { close(allDeferred) })
			}
			return
		}
		m.Ack()
		redelivered.Add(1)
	})
	must(err)
	p, err := b.NewPub()
	must(err)
	var pe atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, 1000)
	for i := 0; i < n; i++ {
		sem <- struct{}{}
		wg.Add(1)
		p.PublishAsync(body(uint64(i), 0), func(err error) {
			if err != nil {
				pe.Add(1)
			}
			<-sem
			wg.Done()
		})
	}
	wg.Wait()
	fmt.Printf("PROG published n=%d errs=%d t=%.1fs\n", n, pe.Load(), time.Since(start).Seconds())
	lastAct.Store(time.Now().UnixNano())
	t := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-allDeferred:
			fmt.Printf("DEFERRED_ALL t=%.1fs\n", time.Since(start).Seconds())
			allDeferred = nil
			continue
		case <-t.C:
		}
		fmt.Printf("PROG t=%3.0fs deferred=%d redelivered=%d\n", time.Since(start).Seconds(), deferred.Load(), redelivered.Load())
		if redelivered.Load() >= int64(n) || time.Since(time.Unix(0, lastAct.Load())) > *waitF {
			break
		}
	}
	fmt.Printf("RESULT defer sys=%s n=%d delay=%s inflight=%d deferred=%d redelivered=%d\n",
		*sysF, n, *delayF, *inflightF, deferred.Load(), redelivered.Load())
	stop()
}

func main() {
	flag.Parse()
	b := broker()
	if *setupF && *modeF != "check" {
		must(b.Setup())
	}
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		os.Exit(1)
	}()
	switch *modeF {
	case "pub":
		modePub(b)
	case "sub":
		modeSub(b)
	case "e2e":
		modeE2E(b)
	case "crash":
		modeCrash(b)
	case "defer":
		modeDefer(b)
	case "pubids":
		modePubIDs(b)
	case "worker":
		modeWorker(b)
	case "check":
		modeCheck()
	}
}

func maxAck() int {
	if *maxAckF > 0 {
		return *maxAckF
	}
	return *inflightF
}
