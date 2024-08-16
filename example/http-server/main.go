//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	gonet "net"
	"net/http"
	"runtime"
	"strconv"

	_ "embed"

	net "github.com/godzie44/go-uring/net"
	reactor "github.com/godzie44/go-uring/reactor"
	"github.com/godzie44/go-uring/uring"
)

//go:embed gopher.png
var gopher []byte

var (
	noURing   = flag.Bool("no-uring", false, "dont use io_uring based http server")
	ringCount = flag.Int("r", 1, "io_urings count")
)

func main() {
	flag.Parse()

	portStr := flag.Arg(0)
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/ping", func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong\n"))
	})

	mux.HandleFunc("/post", func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer request.Body.Close()

		body, err := io.ReadAll(request.Body)
		if err != nil {
			log.Println(err)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("you send: %s", string(body))))
	})

	mux.HandleFunc("/get-file", func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(gopher)
	})

	mux.HandleFunc("/file-info", func(w http.ResponseWriter, request *http.Request) {
		request.ParseMultipartForm(10 << 20)

		file, handler, err := request.FormFile("file")
		if err != nil {
			log.Println(err)
			return
		}
		defer file.Close()

		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, "Uploaded File: %+v\n", handler.Filename)
		fmt.Fprintf(w, "File Size: %+v\n", handler.Size)
		fmt.Fprintf(w, "MIME Header: %+v\n", handler.Header)
	})

	if *noURing {
		log.Println("start default net/http server")
		defSrv(mux, port)
	} else {
		log.Println("start io_uring server")
		ringSrv(mux, port)
	}
}

func defSrv(mux *http.ServeMux, port int) {
	lc := gonet.ListenConfig{}

	l, err := lc.Listen(context.Background(), "tcp", "0.0.0.0:8080")
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Handler: mux,
	}

	err = server.Serve(l)
	if err != nil {
		log.Fatal(err)
	}
}

func ringSrv(mux *http.ServeMux, port int) {
	ringCnt := *ringCount

	var params []uring.SetupOption
	runtime.GOMAXPROCS(runtime.NumCPU() * 2)

	rings, stop, err := uring.CreateMany(ringCnt, uring.MaxEntries>>2, 1, params...)
	if err != nil {
		log.Fatal(err)
	}
	defer stop() //nolint

	reactor, err := reactor.NewNet(rings)
	if err != nil {
		log.Fatal(err)
	}

	l, err := net.NewListener(gonet.ListenConfig{}, "0.0.0.0:8080", reactor)
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{Addr: "0.0.0.0:8080", Handler: mux}
	defer server.Close()

	err = server.Serve(l)
	if err != nil {
		log.Fatal(err)
	}
}
