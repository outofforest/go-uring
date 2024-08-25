//go:build linux

package net

import (
	"log"
	"net"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/godzie44/go-uring/uring"
)

type (
	ListenConfig struct {
		Backlog int
	}

	Listener struct {
		Rings           []*uring.Ring
		AcceptOp        *uring.AcceptOp
		FD              uintptr
		Config          *ListenConfig
		ToAcceptChannel chan struct{}
		ToRecvChannel   chan *Connection
		ToSendChannel   chan *Connection
		AcceptChannel   chan int32
		ResultsChannel  chan []Result
		ConnectionList  ConnectionList
		AcceptData      UserData
	}

	UserData struct {
		Type       UserDataType
		Connection *Connection
	}

	UserDataType uint

	Result struct {
		UserData *UserData
		Result   int32
	}
)

const (
	_ UserDataType = iota
	UserDataTypeAccept
	UserDataTypeRecv
	UserDataTypeSend
)

func Listen(rings []*uring.Ring) (net.Listener, error) {
	FD, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}

	err = unix.SetsockoptInt(FD, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
	if err != nil {
		return nil, err
	}

	err = unix.SetsockoptInt(FD, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	if err != nil {
		return nil, err
	}

	err = unix.SetsockoptInt(FD, unix.IPPROTO_TCP, unix.TCP_FASTOPEN, 1)
	if err != nil {
		return nil, err
	}

	err = unix.SetsockoptInt(FD, unix.IPPROTO_TCP, unix.TCP_NODELAY, 1)
	if err != nil {
		return nil, err
	}

	err = unix.Bind(FD, &unix.SockaddrInet4{Port: 8080})
	if err != nil {
		return nil, err
	}

	err = unix.Listen(FD, unix.SOMAXCONN)
	if err != nil {
		return nil, err
	}

	config := &ListenConfig{
		Backlog: unix.SOMAXCONN,
	}

	numCPU := runtime.NumCPU()

	listener := &Listener{
		Rings:           rings,
		AcceptOp:        uring.Accept(uintptr(FD), 0),
		FD:              uintptr(FD),
		Config:          config,
		ToAcceptChannel: make(chan struct{}, 1),
		ToRecvChannel:   make(chan *Connection, numCPU),
		ToSendChannel:   make(chan *Connection, numCPU),
		AcceptChannel:   make(chan int32),
		ResultsChannel:  make(chan []Result),
		AcceptData: UserData{
			Type: UserDataTypeAccept,
		},
	}

	i := 0

	for i = range rings {
		go listener.b1(i)
	}

	go listener.b2()
	go listener.b3()

	return listener, nil
}

func (listener *Listener) Accept() (net.Conn, error) {
	listener.ToAcceptChannel <- struct{}{}

	result := <-listener.AcceptChannel

	if result < 0 {
		return nil, syscall.Errno(uintptr(-result))
	}

	FD := uintptr(result)

	node := listener.ConnectionList.PushHead(&Connection{
		RecvOp:         uring.Recv(FD, nil, 0),
		SendOp:         uring.Send(FD, nil, 0),
		FD:             FD,
		ConnectionList: &listener.ConnectionList,
		ToRecvChannel:  listener.ToRecvChannel,
		ToSendChannel:  listener.ToSendChannel,
		RecvChannel:    make(chan int32),
		SendChannel:    make(chan int32),
		RecvData: UserData{
			Type: UserDataTypeRecv,
		},
		SendData: UserData{
			Type: UserDataTypeSend,
		},
	})

	node.RecvData.Connection = node
	node.SendData.Connection = node

	return node, nil
}

func (listener *Listener) Addr() net.Addr {
	return nil
}

func (listener *Listener) Close() error {
	return unix.Close(int(listener.FD))
}

func (listener *Listener) b1(i int) {
	ring := listener.Rings[i]
	CQEs := make([]*uring.CQEvent, listener.Config.Backlog)
	results := []Result(nil)
	CQE, err := (*uring.CQEvent)(nil), error(nil)

	n := 0
	j := 0

	for {
		_, err = ring.WaitCQEvents(1)
		if err != nil {
			switch typed := err.(type) {
			case *os.SyscallError:
				if typed.Err == syscall.EAGAIN || typed.Err == syscall.EINTR {
					continue
				}
			}

			log.Panic(err)
		}

		for {
			n = ring.PeekCQEventBatch(CQEs)
			if n == 0 {
				break
			}

			results = make([]Result, n)

			for j, CQE = range CQEs[:n] {
				results[j] = Result{
					UserData: Uint64ToPtr[UserData](CQE.UserData),
					Result:   CQE.Res,
				}
			}

			listener.ResultsChannel <- results
			ring.AdvanceCQ(uint32(n))
		}
	}
}

func (listener *Listener) b2() {
	ring := (*uring.Ring)(nil)
	connection, err := (*Connection)(nil), error(nil)

	i := 0
	last := len(listener.Rings) - 1

	for {
		ring = listener.Rings[i]

		select {
		case <-listener.ToAcceptChannel:
			err = ring.QueueSQE(listener.AcceptOp, 0, PtrToUint64(&listener.AcceptData))
		case connection = <-listener.ToRecvChannel:
			err = ring.QueueSQE(connection.RecvOp, 0, PtrToUint64(&connection.RecvData))
		case connection = <-listener.ToSendChannel:
			err = ring.QueueSQE(connection.SendOp, 0, PtrToUint64(&connection.SendData))
		}
		if err != nil {
			log.Panic(err)
		}

		_, err := ring.Submit()
		if err != nil {
			log.Panic(err)
		}

		if i == last {
			i = 0
		} else {
			i += 1
		}
	}
}

func (listener *Listener) b3() {
	results := []Result(nil)

	for {
		select {
		case results = <-listener.ResultsChannel:
			listener.handleCQEs(results)
		}
	}
}

func (listener *Listener) handleCQEs(results []Result) {
	result := Result{}

	for _, result = range results {
		switch result.UserData.Type {
		case UserDataTypeAccept:
			listener.AcceptChannel <- result.Result
		case UserDataTypeRecv:
			result.UserData.Connection.RecvChannel <- result.Result
		case UserDataTypeSend:
			result.UserData.Connection.SendChannel <- result.Result
		}
	}
}

func PtrToUint64[T any](ptr *T) uint64 {
	return uint64(uintptr(unsafe.Pointer(ptr)))
}

func Uint64ToPtr[T any](value uint64) *T {
	return (*T)(unsafe.Pointer(uintptr(value)))
}
