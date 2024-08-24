package main

import (
	"flag"
	"io"
	"log"
	gonet "net"
	"time"

	net "github.com/godzie44/go-uring/net2"
	"github.com/godzie44/go-uring/uring"
)

var (
	ringCount = flag.Int("ring-count", 6, "io_uring's count")
	wpCount   = flag.Int("wp-count", 2, "io_uring's work pools count")
)

func main() {
	flag.Parse()

	rings := make([]*uring.Ring, 0, *ringCount)
	defer func() {
		for i := range rings {
			rings[i].Close()
		}
	}()

	ratio := *ringCount / *wpCount

	for i := 0; i < *wpCount; i += 1 {
		ringWIthWQ, err := uring.New(uring.MaxEntries, uring.WithSQPoll(time.Second))
		if err != nil {
			log.Panic(err)
		}

		rings = append(rings, ringWIthWQ)

		for j := 1; j < ratio; j += 1 {
			ring, err := uring.New(uring.MaxEntries, uring.WithSQPoll(time.Second), uring.WithAttachedWQ(ringWIthWQ.Fd()))
			if err != nil {
				log.Panic(err)
			}

			rings = append(rings, ring)
		}
	}

	listener, err := net.Listen(rings)
	if err != nil {
		log.Panic(err)
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Failed to accept conn.", err)
			continue
		}

		go func(conn gonet.Conn) {
			defer conn.Close()
			io.Copy(conn, conn)
		}(conn)
	}
}
