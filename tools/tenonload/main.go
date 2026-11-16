// tenonload：在单机上按真实路径压测 tenon 队列：事务中插入业务行并发布任务 → 复制槽 → relay → JetStream → Worker。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/uptrace/bun"

	"github.com/go-tenon/tenon/database"
	"github.com/go-tenon/tenon/database/migrate"
	"github.com/go-tenon/tenon/natsx"
	"github.com/go-tenon/tenon/queue"
)

var (
	modeF   = flag.String("mode", "run", "setup | run | defer")
	dsnF    = flag.String("dsn", "postgres://postgres:postgres@127.0.0.1:5432/bench?sslmode=disable", "")
	natsF   = flag.String("nats", "nats://127.0.0.1:4222", "")
	ratesF  = flag.String("rates", "200,500,1000,2000", "comma separated job/s; 0 = as fast as possible")
	stepF   = flag.Duration("step", 60*time.Second, "duration of each rate step")
	pubsF   = flag.Int("pubs", 32, "publisher goroutines")
	workF   = flag.String("work", "noop", "noop | db (handler updates the business row)")
	concF   = flag.Int("concurrency", 10, "queue concurrency per topic")
	nF      = flag.Int("n", 100000, "defer: jobs to schedule")
	delayF  = flag.Duration("delay", 30*time.Minute, "defer: delay")
	cgroupF = flag.String("cgroups", "", "name=path,... container cgroups to sample")
)

type job struct {
	ID   uuid.UUID `json:"id"`
	Sent int64     `json:"sent"`
}

var benchTopic = queue.Topic[job]("bench.job")

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func openDB(ctx context.Context) *bun.DB {
	cfg := database.DefaultConfig()
	cfg.DSN = *dsnF
	cfg.SlowQuery = 0
	db, err := database.Open(ctx, cfg)
	must(err)
	return db
}

func queueConfig() queue.Config {
	cfg := queue.DefaultConfig()
	cfg.Stream = "bench"
	cfg.Concurrency = *concF
	return cfg
}

func main() {
	flag.Parse()
	ctx := context.Background()
	db := openDB(ctx)
	defer db.Close()
	switch *modeF {
	case "setup":
		p, err := migrate.NewProvider(db, queue.Migrations())
		must(err)
		must(migrate.Up(ctx, p))
		_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS bench_items (id uuid PRIMARY KEY, created_at timestamptz NOT NULL, done_at timestamptz)`)
		must(err)
		fmt.Println("setup done")
	case "run":
		run(ctx, db)
	case "defer":
		deferJobs(ctx, db)
	}
}

// ---------- 采样 ----------

type cpuSample struct {
	app      int64            // 本进程 CPU 微秒
	groups   map[string]int64 // 容器 CPU 微秒
	idle, tt int64            // /proc/stat 的 idle 和 total jiffies
	at       time.Time
}

func cgroups() map[string]string {
	m := map[string]string{}
	if *cgroupF == "" {
		return m
	}
	for _, kv := range strings.Split(*cgroupF, ",") {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

func sample(groups map[string]string) cpuSample {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	s := cpuSample{app: ru.Utime.Nano()/1000 + ru.Stime.Nano()/1000, groups: map[string]int64{}, at: time.Now()}
	for name, path := range groups {
		b, err := os.ReadFile(path + "/cpu.stat")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "usage_usec "); ok {
				s.groups[name], _ = strconv.ParseInt(v, 10, 64)
			}
		}
	}
	if b, err := os.ReadFile("/proc/stat"); err == nil {
		f := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])
		for i, v := range f[1:] {
			n, _ := strconv.ParseInt(v, 10, 64)
			s.tt += n
			if i == 3 || i == 4 { // idle + iowait
				s.idle += n
			}
		}
	}
	return s
}

// cpuLine 返回两次采样之间各部分的 CPU 占用（100% = 1 核）和整机使用率。
func cpuLine(a, b cpuSample, groups map[string]string) string {
	wall := float64(b.at.Sub(a.at).Microseconds())
	parts := []string{fmt.Sprintf("app=%.0f%%", float64(b.app-a.app)/wall*100)}
	for name := range groups {
		parts = append(parts, fmt.Sprintf("%s=%.0f%%", name, float64(b.groups[name]-a.groups[name])/wall*100))
	}
	if b.tt > a.tt {
		parts = append(parts, fmt.Sprintf("host=%.0f%%", 100*(1-float64(b.idle-a.idle)/float64(b.tt-a.tt))))
	}
	return strings.Join(parts, " ")
}

func memLine(groups map[string]string) string {
	var parts []string
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	parts = append(parts, fmt.Sprintf("app_rss_max=%dMB", ru.Maxrss/1024))
	for name, path := range groups {
		b, err := os.ReadFile(path + "/memory.stat")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "anon "); ok {
				n, _ := strconv.ParseInt(v, 10, 64)
				parts = append(parts, fmt.Sprintf("%s_anon=%dMB", name, n>>20))
			}
		}
	}
	return strings.Join(parts, " ")
}

// ---------- 压测 ----------

func newHist() *hdrhistogram.Histogram { return hdrhistogram.New(1, 600_000_000, 3) } // µs

func pct(h *hdrhistogram.Histogram) string {
	ms := func(v int64) float64 { return float64(v) / 1000 }
	return fmt.Sprintf("p50=%.1fms p99=%.1fms max=%.0fms", ms(h.ValueAtQuantile(50)), ms(h.ValueAtQuantile(99)), ms(h.Max()))
}

// connectNATS 按框架的约定连接 NATS，队列的 Worker 和 Admin 共用这条连接。
func connectNATS() *nats.Conn {
	nc, err := natsx.Connect(natsx.Config{URL: *natsF}, "tenonload", slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	must(err)
	return nc
}

func run(ctx context.Context, db *bun.DB) {
	groups := cgroups()
	cfg := queueConfig()
	nc := connectNATS()
	defer nc.Close()
	var mu sync.Mutex
	e2eWin, e2eStep := newHist(), newHist()
	txWin, txStep := newHist(), newHist()
	var published, handled, pubErr atomic.Int64

	w := queue.NewWorker(db, nc, cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	benchTopic.Handle(w, func(ctx context.Context, j job) error {
		if *workF == "db" {
			if _, err := db.NewUpdate().Table("bench_items").Set("done_at = now()").Where("id = ?", j.ID).Exec(ctx); err != nil {
				return err
			}
		}
		lat := (time.Now().UnixNano() - j.Sent) / 1000
		mu.Lock()
		e2eWin.RecordValue(lat)
		e2eStep.RecordValue(lat)
		mu.Unlock()
		handled.Add(1)
		return nil
	})
	wctx, stopWorker := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- w.Run(wctx) }()
	admin := queue.NewAdmin(nc, cfg)
	time.Sleep(2 * time.Second)

	publishOne := func(intended time.Time) {
		id := uuid.Must(uuid.NewV7())
		err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if _, err := tx.ExecContext(ctx, "INSERT INTO bench_items (id, created_at) VALUES (?, now())", id); err != nil {
				return err
			}
			return benchTopic.Publish(ctx, tx, job{ID: id, Sent: time.Now().UnixNano()})
		})
		if err != nil {
			pubErr.Add(1)
			return
		}
		lat := time.Since(intended).Microseconds()
		mu.Lock()
		txWin.RecordValue(lat)
		txStep.RecordValue(lat)
		mu.Unlock()
		published.Add(1)
	}

	for _, rs := range strings.Split(*ratesF, ",") {
		rate, _ := strconv.Atoi(rs)
		mu.Lock()
		e2eStep.Reset()
		txStep.Reset()
		mu.Unlock()
		p0, h0 := published.Load(), handled.Load()
		start := time.Now()
		end := start.Add(*stepF)
		var wg sync.WaitGroup
		for i := 0; i < *pubsF; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if rate == 0 {
					for time.Now().Before(end) {
						publishOne(time.Now())
					}
					return
				}
				interval := time.Duration(int64(time.Second) * int64(*pubsF) / int64(rate))
				next := start.Add(interval * time.Duration(i) / time.Duration(*pubsF))
				for next.Before(end) {
					if d := time.Until(next); d > 0 {
						time.Sleep(d)
					}
					publishOne(next)
					next = next.Add(interval)
				}
			}(i)
		}
		fmt.Printf("== step rate=%d pubs=%d work=%s concurrency=%d\n", rate, *pubsF, *workF, *concF)
		stop := make(chan struct{})
		var mon sync.WaitGroup
		mon.Go(func() {
			t := time.NewTicker(10 * time.Second)
			defer t.Stop()
			last := sample(groups)
			lp, lh := published.Load(), handled.Load()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
				}
				now := sample(groups)
				p, h := published.Load(), handled.Load()
				mu.Lock()
				e2e, tx := pct(e2eWin), pct(txWin)
				e2eWin.Reset()
				txWin.Reset()
				mu.Unlock()
				fmt.Printf("WIN t=%3.0fs pub=%5.0f/s handled=%5.0f/s backlog=%d | tx %s | e2e %s | %s\n",
					time.Since(start).Seconds(), float64(p-lp)/10, float64(h-lh)/10, p-h, tx, e2e, cpuLine(last, now, groups))
				last, lp, lh = now, p, h
			}
		})
		wg.Wait()
		close(stop)
		mon.Wait()
		el := time.Since(start).Seconds()
		p, h := published.Load()-p0, handled.Load()-h0
		backlog := published.Load() - handled.Load()
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		st, _ := queue.ReadStatus(sctx, db)
		js, _ := admin.Status(sctx)
		cancel()
		var pending uint64
		for _, t := range js.Topics {
			pending += t.Pending + uint64(t.InFlight)
		}
		mu.Lock()
		fmt.Printf("RESULT rate=%d pub=%.0f/s handled=%.0f/s backlog_at_end=%d slot_lag=%dB js_pending=%d pubErr=%d | tx %s | e2e %s | %s\n",
			rate, float64(p)/el, float64(h)/el, backlog, st.Lag, pending, pubErr.Load(), pct(txStep), pct(e2eStep), memLine(groups))
		mu.Unlock()
		// 排空，下一档从零积压开始
		deadline := time.Now().Add(120 * time.Second)
		for published.Load() > handled.Load() && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
		}
		if published.Load() > handled.Load() {
			fmt.Printf("DRAIN timeout: backlog=%d\n", published.Load()-handled.Load())
		}
	}
	stopWorker()
	must(<-done)
}

// deferJobs 发布 n 个延迟任务（不超过 MaxDefer，作为 JetStream 的定时消息），用于观察 NATS 的内存。
func deferJobs(ctx context.Context, db *bun.DB) {
	cfg := queueConfig()
	nc := connectNATS()
	defer nc.Close()
	w := queue.NewWorker(db, nc, cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	wctx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- w.Run(wctx) }() // 只转发
	admin := queue.NewAdmin(nc, cfg)
	start := time.Now()
	const batch = 1000
	for i := 0; i < *nF; i += batch {
		jobs := make([]job, min(batch, *nF-i))
		for k := range jobs {
			jobs[k] = job{ID: uuid.Must(uuid.NewV7()), Sent: time.Now().UnixNano()}
		}
		must(benchTopic.PublishAll(ctx, db, jobs, queue.Delay(*delayF)))
	}
	fmt.Printf("published %d delayed jobs in %.1fs\n", *nF, time.Since(start).Seconds())
	for {
		st, err := admin.Status(ctx)
		must(err)
		fmt.Printf("deferred=%d stream_msgs=%d stream_bytes=%d %s\n", st.Deferred, st.Messages, st.Bytes, memLine(cgroups()))
		if st.Deferred >= *nF {
			break
		}
		time.Sleep(2 * time.Second)
	}
	stop()
	must(<-done)
}
