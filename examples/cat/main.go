//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"unsafe"

	"github.com/godzie44/go-uring/uring"
	"golang.org/x/sys/unix"
)

type (
	File struct {
		Name    string
		FD      int
		Buffers [][]byte
		Wait    int
	}
)

const (
	QUEUE_DEPTH = 1
	BLOCK_SIZE  = 32 * 1024
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s [file name] <[file name] ...>\n", os.Args[0])
	}

	ring, err := uring.New(QUEUE_DEPTH)
	if err != nil {
		log.Fatalln(err)
	}
	defer ring.Close()

	args := os.Args[1:]

	files := make([]File, len(args))
	file := (*File)(nil)

	i := 0
	j := 0

	for i < len(args) {
		file = &files[i]

		file.Name = args[i]

		file.FD, err = unix.Open(args[i], os.O_RDONLY, 0)
		if err != nil {
			log.Fatalln(err)
		}

		err = file.SubmitRead(ring)
		if err != nil {
			log.Fatalln(err)
		}

		i += 1
		j += len(file.Buffers)
	}

	cqe := (*uring.CQEvent)(nil)

	for j > 0 {
		cqe, err = ring.PeekCQE()
		if err != nil {
			log.Fatalln(err)
		}

		file = (*File)(unsafe.Pointer(uintptr(cqe.UserData)))
		file.Wait -= 1

		if file.Wait == 0 {
			fmt.Println(file.Name + ":")

			for i = range file.Buffers {
				fmt.Printf("%s", file.Buffers[i])
			}

			fmt.Println()
		}

		ring.SeenCQE(cqe)

		j -= 1
	}
}

func (request *File) SubmitRead(ring *uring.Ring) error {
	FD := request.FD

	stat := new(unix.Stat_t)

	err := unix.Fstat(FD, stat)
	if err != nil {
		return err
	}

	request.Buffers = getBuffers(stat.Size)

	i := 0
	j := uint64(0)

	for i < len(request.Buffers) {
		err = ring.QueueSQE(uring.Read(uintptr(FD), request.Buffers[i], j), 0, uint64(uintptr(unsafe.Pointer(request))))
		if err != nil {
			return err
		}

		_, err = ring.Submit()
		if err != nil {
			log.Fatalln(err)
		}

		i += 1
		j += BLOCK_SIZE
	}

	request.Wait = i

	return nil
}

func getBuffers(size int64) [][]byte {
	blocks := int(size / BLOCK_SIZE)
	if size%BLOCK_SIZE != 0 {
		blocks += 1
	}

	buffers := make([][]byte, blocks)
	buffer := make([]byte, size)

	i := 0
	j := 0
	k := BLOCK_SIZE

	for i < blocks {
		if k > len(buffer) {
			k = len(buffer)
		}

		buffers[i] = buffer[j:k]

		j = k

		i += 1
		k += BLOCK_SIZE
	}

	return buffers
}
