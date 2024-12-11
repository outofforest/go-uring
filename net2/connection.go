//go:build linux

package net

import (
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/outofforest/go-uring/uring"
)

type (
	Connection struct {
		RecvOp         *uring.RecvOp
		SendOp         *uring.SendOp
		FD             uintptr
		ConnectionList *ConnectionList
		ToRecvChannel  chan *Connection
		ToSendChannel  chan *Connection
		RecvChannel    chan int32
		SendChannel    chan int32
		Previous       *Connection
		Next           *Connection
		RecvData       UserData
		SendData       UserData
	}
)

func (connection *Connection) Read(buffer []byte) (n int, err error) {
	connection.RecvOp.SetBuffer(buffer)
	connection.ToRecvChannel <- connection

	result := <-connection.RecvChannel

	if result > 0 {
		return int(result), nil
	}

	if result == 0 {
		return 0, &net.OpError{Op: "read", Net: "tcp", Source: nil, Addr: nil, Err: io.EOF}
	}

	err = syscall.Errno(uintptr(-result))

	if err == syscall.ECANCELED {
		err = fmt.Errorf("%w: %s", os.ErrDeadlineExceeded, err.Error())
	}

	return 0, &net.OpError{Op: "read", Net: "tcp", Source: nil, Addr: nil, Err: err}
}

func (connection *Connection) Write(buffer []byte) (n int, err error) {
	connection.SendOp.SetBuffer(buffer)
	connection.ToSendChannel <- connection

	result := <-connection.SendChannel

	if result > 0 {
		return int(result), nil
	}

	if result == 0 {
		return 0, &net.OpError{Op: "write", Net: "tcp", Source: nil, Addr: nil, Err: io.ErrUnexpectedEOF}
	}

	err = syscall.Errno(uintptr(-result))

	if err == syscall.ECANCELED {
		err = fmt.Errorf("%w: %s", os.ErrDeadlineExceeded, err.Error())
	}

	return 0, &net.OpError{Op: "write", Net: "tcp", Source: nil, Addr: nil, Err: err}
}

func (connection *Connection) Close() error {
	connection.Remove(connection.ConnectionList)

	err := unix.Close(int(connection.FD))
	if err != nil {
		return &net.OpError{Op: "close", Net: "tcp", Source: nil, Addr: nil, Err: err}
	}

	return nil
}

func (connection *Connection) LocalAddr() net.Addr {
	return nil
}

func (connection *Connection) RemoteAddr() net.Addr {
	return nil
}

func (connection *Connection) SetDeadline(deadline time.Time) error {
	return nil
}

func (connection *Connection) SetReadDeadline(readDeadline time.Time) error {
	return nil
}

func (connection *Connection) SetWriteDeadline(writeDeadline time.Time) error {
	return nil
}
