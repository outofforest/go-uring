//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	gonet "net"
	"runtime"
	"strconv"

	net "github.com/godzie44/go-uring/net"
	reactor "github.com/godzie44/go-uring/reactor"
	"github.com/godzie44/go-uring/uring"
)

var (
	noURing   = flag.Bool("no-uring", false, "dont use io_uring based http server")
	ringCount = flag.Int("ring-count", 6, "io_urings count")
	wpCount   = flag.Int("wp-count", 2, "io_uring's work pools count")
)

func main() {
	flag.Parse()

	portStr := flag.Arg(0)
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatal(err)
	}

	if *noURing {
		log.Println("start default net/http server")
		defSrv(port)
	} else {
		log.Println("start io_uring server")
		ringSrv(port)
	}
}

func defSrv(port int) {
	lc := gonet.ListenConfig{}

	listener, err := lc.Listen(context.Background(), "tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		log.Fatal(err)
	}

	serve(listener)
}

func ringSrv(port int) {
	runtime.GOMAXPROCS(runtime.NumCPU() * 2)

	rings, stop, err := uring.CreateMany(*ringCount, uring.MaxEntries>>2, *wpCount)
	if err != nil {
		log.Fatal(err)
	}
	defer stop()

	reactor, err := reactor.NewNet(rings)
	if err != nil {
		log.Fatal(err)
	}

	addr := fmt.Sprintf("0.0.0.0:%d", port)

	listener, err := net.NewListener(gonet.ListenConfig{}, addr, reactor)
	if err != nil {
		log.Panic(err)
	}

	serve(listener)
}

func serve(listener gonet.Listener) {
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
