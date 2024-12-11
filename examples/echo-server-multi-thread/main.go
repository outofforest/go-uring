//go:build linux

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"runtime"
	"strconv"
	"time"

	uringnet "github.com/outofforest/go-uring/net"
	"github.com/outofforest/go-uring/reactor"
	"github.com/outofforest/go-uring/uring"
)

const (
	MaxConnections   = 4096
	MaxMessageLength = 2048
)

func initBuffs() [][]byte {
	buffs := make([][]byte, MaxConnections)
	for i := range buffs {
		buffs[i] = make([]byte, MaxMessageLength)
	}
	return buffs
}

var buffs = initBuffs()

const (
	modeURing       = "uring"
	modeURingSQPoll = "uring-sq-poll"
	modeDefault     = "default"
)

var (
	mode      = flag.String("mode", "uring", "server async backend: uring/uring-sq-poll/default")
	ringCount = flag.Int("ring-count", 6, "io_uring's count")
	wpCount   = flag.Int("wp-count", 2, "io_uring's work pools count")
)

func main() {
	flag.Parse()

	portStr := flag.Arg(0)
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatal(err)
	}

	listener := net.Listener(nil)
	opts := []uring.SetupOption(nil)

	switch *mode {
	case modeDefault:
		listener, err = net.ListenTCP("tcp", &net.TCPAddr{
			Port: port,
		})
		if err != nil {
			log.Fatal(err)
		}

	case modeURingSQPoll:
		opts = append(opts, uring.WithSQPoll(time.Millisecond*100))
		fallthrough

	case modeURing:
		runtime.GOMAXPROCS(runtime.NumCPU() * 2)

		rings, closeRings, err := uring.CreateMany(*ringCount, uring.MaxEntries>>3, *wpCount, opts...)
		if err != nil {
			log.Fatal(err)
		}

		defer func() {
			err := closeRings()
			if err != nil {
				log.Fatal(err)
			}
		}()

		netReactor, err := reactor.NewNet(rings)
		if err != nil {
			log.Fatal(err)
		}

		listener, err = uringnet.NewListener(net.ListenConfig{}, fmt.Sprintf("0.0.0.0:%d", port), netReactor)
		if err != nil {
			log.Fatal(err)
		}

	default:
		log.Fatal("mode must be one of uring/uring-sq-poll/default")
	}

	if *mode == modeDefault {
		log.Printf("start echo-server, mode: defaut net/http, port: %d", port)
	} else {
		log.Printf("start echo-server, mode: %s, port: %d, rings: %d, pools: %d", *mode, port, *ringCount, *wpCount)
	}

	runServer(listener)
}

func runServer(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}

		go handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
	fd := int(0)

	switch c := conn.(type) {
	case *net.TCPConn:
		f, _ := c.File()
		fd = int(f.Fd())
	case *uringnet.Conn:
		fd = int(c.Fd())
	}

	buff := buffs[fd]
	for {
		n, err := conn.Read(buff)
		if err == io.EOF || n == 0 {
			if err != nil {
				log.Fatal(err)
			}
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		_, err = conn.Write(buff[:n])
		if err != nil {
			log.Fatal(err)
		}
	}
}
