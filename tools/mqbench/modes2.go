package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

var logF = flag.String("log", "", "worker: processed-id log file")

// 预先发布编号 0..n-1，全部确认后退出。
func modePubIDs(b Broker) {
	p, err := b.NewPub()
	must(err)
	var e atomic.Int64
	for i := 0; i < *nF; i++ {
		publishRetry(p, body(uint64(i), 0), &e)
	}
	p.Close()
	fmt.Printf("PROG published %d\n", *nF)
}

// 处理完一条就把编号追加到日志，再 ack；空闲 wait 后退出。
func modeWorker(b Broker) {
	f, err := os.OpenFile(*logF, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	must(err)
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	stop, err := b.Consume(*cF, *inflightF, func(m Msg) {
		time.Sleep(*workF)
		var buf [8]byte
		copy(buf[:], m.Data()[:8])
		f.Write(buf[:])
		last.Store(time.Now().UnixNano())
		m.Ack()
	})
	must(err)
	for time.Since(time.Unix(0, last.Load())) < *waitF {
		time.Sleep(time.Second)
	}
	stop()
}

func modeCheck() {
	data, err := os.ReadFile(*logF)
	must(err)
	seen := make([]int, *nF)
	for i := 0; i+8 <= len(data); i += 8 {
		seen[binary.LittleEndian.Uint64(data[i:])]++
	}
	lost, dup := 0, 0
	var sample []int
	for i, s := range seen {
		if s == 0 {
			lost++
			if len(sample) < 5 {
				sample = append(sample, i)
			}
		} else {
			dup += s - 1
		}
	}
	fmt.Printf("RESULT check sys=%s n=%d processed=%d lost=%d dup=%d sampleLost=%v\n", *sysF, *nF, len(data)/8, lost, dup, sample)
}
