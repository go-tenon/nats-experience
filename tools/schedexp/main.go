package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	nc, _ := nats.Connect("nats://127.0.0.1:54222")
	defer nc.Close()
	js, _ := jetstream.New(nc)
	ctx := context.Background()
	defer js.DeleteStream(ctx, "XP")
	s, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "XP", Subjects: []string{"XP.>"}, Retention: jetstream.WorkQueuePolicy, AllowMsgSchedules: true})
	if err != nil {
		panic(err)
	}
	js.Publish(ctx, "XP.job.a.b", []byte("1"))
	js.Publish(ctx, "XP.job.a.b", []byte("2"))
	js.Publish(ctx, "XP.job.c", []byte("3"))
	for i := range 3 {
		_, err := js.PublishMsg(ctx, &nats.Msg{Subject: fmt.Sprintf("XP.delay.%d", i), Data: []byte("d")},
			jetstream.WithScheduleAt(time.Now().Add(time.Hour)), jetstream.WithScheduleTarget("XP.job.c"))
		if err != nil {
			panic(err)
		}
	}
	info, _ := s.Info(ctx, jetstream.WithSubjectFilter("XP.job.>"))
	fmt.Println("filtered: msgs", info.State.Msgs, "numSubjects", info.State.NumSubjects, "subjects", info.State.Subjects)
	c, _ := js.CreateConsumer(ctx, "XP", jetstream.ConsumerConfig{Durable: "c", FilterSubject: "XP.job.c", AckPolicy: jetstream.AckExplicitPolicy})
	m, _ := c.Next()
	m.Ack()
	time.Sleep(100 * time.Millisecond)
	info, _ = s.Info(ctx, jetstream.WithSubjectFilter("XP.job.>"))
	fmt.Println("after ack: msgs", info.State.Msgs, "numSubjects", info.State.NumSubjects, "subjects", info.State.Subjects)
	until := time.Now().Add(time.Hour)
	r, err := js.PauseConsumer(ctx, "XP", "c", until)
	fmt.Println("pause:", r, err)
	ci, _ := c.Info(ctx)
	fmt.Println("paused", ci.Paused, ci.PauseRemaining.Round(time.Second), ci.Config.PauseUntil)
	// update consumer without PauseUntil: does it unpause?
	_, err = js.CreateOrUpdateConsumer(ctx, "XP", jetstream.ConsumerConfig{Durable: "c", FilterSubject: "XP.job.c", AckPolicy: jetstream.AckExplicitPolicy, AckWait: 10 * time.Second})
	ci, _ = c.Info(ctx)
	fmt.Println("after update without pause_until: paused", ci.Paused, "err", err)
}
