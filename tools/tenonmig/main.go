// tenonmig：多租户和迁移演练。发布方和 worker 是两个进程，处理结果记在业务表中，用 SQL 核对丢失和重复。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/go-tenon/tenon/database"
	"github.com/go-tenon/tenon/database/migrate"
	"github.com/go-tenon/tenon/natsx"
	"github.com/go-tenon/tenon/queue"
)

var (
	modeF  = flag.String("mode", "work", "setup | pub | work | check")
	dsnF   = flag.String("dsn", "postgres://postgres:postgres@127.0.0.1:5432/bench?sslmode=disable", "")
	rateF  = flag.Int("rate", 100, "pub: jobs per second")
	delayF = flag.Int("delayed", 10, "pub: one in N jobs is delayed 2-3 minutes")
)

type job struct {
	ID uuid.UUID `json:"id"`
}

var topic = queue.Topic[job]("demo.job")

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := database.DefaultConfig()
	cfg.DSN = *dsnF
	db, err := database.Open(context.Background(), cfg)
	must(err)
	defer db.Close()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	switch *modeF {
	case "setup":
		p, err := migrate.NewProvider(db, queue.Migrations())
		must(err)
		must(migrate.Up(ctx, p))
		_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS items (id uuid PRIMARY KEY, created_at timestamptz NOT NULL,
			run_at timestamptz NOT NULL, handled int NOT NULL DEFAULT 0, done_at timestamptz)`)
		must(err)
		fmt.Println("setup done")
	case "pub":
		n, delayed := 0, 0
		t := time.NewTicker(time.Second / time.Duration(*rateF))
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				fmt.Printf("published %d jobs (%d delayed)\n", n, delayed)
				return
			case <-t.C:
			}
			id := uuid.Must(uuid.NewV7())
			var opts []queue.PublishOption
			runAt := time.Now()
			if *delayF > 0 && n%*delayF == 0 {
				d := 2*time.Minute + time.Duration(rand.IntN(60))*time.Second
				runAt = runAt.Add(d)
				opts = append(opts, queue.Delay(d))
				delayed++
			}
			err := db.RunInTx(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error {
				if _, err := tx.ExecContext(ctx, "INSERT INTO items (id, created_at, run_at) VALUES (?, now(), ?)", id, runAt); err != nil {
					return err
				}
				return topic.Publish(ctx, tx, job{ID: id}, opts...)
			})
			must(err)
			n++
		}
	case "work":
		nc, err := natsx.Connect(natsx.Config{URL: os.Getenv("NATS_URL")}, "tenonmig", logger)
		must(err)
		defer nc.Close()
		qcfg := queue.DefaultConfig()
		qcfg.ShutdownTimeout = 10 * time.Second
		w := queue.NewWorker(db, nc, qcfg, logger)
		topic.Handle(w, func(ctx context.Context, j job) error {
			_, err := db.ExecContext(ctx, "UPDATE items SET handled = handled + 1, done_at = coalesce(done_at, now()) WHERE id = ?", j.ID)
			return err
		})
		must(w.Run(ctx))
		fmt.Println("worker stopped")
	case "check":
		var r struct {
			Total   int `bun:"total"`
			Done    int `bun:"done"`
			Lost    int `bun:"lost"`
			Waiting int `bun:"waiting"`
			Dup     int `bun:"dup"`
			Extra   int `bun:"extra"`
			Delayed int `bun:"delayed"`
			Early   int `bun:"early"`
		}
		err := db.NewRaw(`SELECT count(*) AS total,
			count(*) FILTER (WHERE handled > 0) AS done,
			count(*) FILTER (WHERE handled = 0 AND run_at <= now() - interval '30 seconds') AS lost,
			count(*) FILTER (WHERE handled = 0 AND run_at > now() - interval '30 seconds') AS waiting,
			count(*) FILTER (WHERE handled > 1) AS dup,
			coalesce(sum(handled - 1) FILTER (WHERE handled > 1), 0) AS extra,
			count(*) FILTER (WHERE run_at > created_at + interval '1 minute') AS delayed,
			count(*) FILTER (WHERE done_at < run_at - interval '1 second') AS early
			FROM items`).Scan(ctx, &r)
		must(err)
		fmt.Printf("total=%d handled=%d not_handled_overdue=%d not_due_yet=%d duplicated=%d (extra runs %d) delayed_jobs=%d ran_early=%d\n",
			r.Total, r.Done, r.Lost, r.Waiting, r.Dup, r.Extra, r.Delayed, r.Early)
	}
}
