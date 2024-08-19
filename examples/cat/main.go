package main

import (
	"fmt"
	"log"
	"os"
	"unsafe"

	"github.com/godzie44/go-uring/uring"
)

type (
	Request struct {
		File    *os.File
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

	requests := make([]Request, len(args))
	request := (*Request)(nil)

	i := 0
	j := 0

	for i < len(args) {
		request = &requests[i]

		request.File, err = os.Open(args[i])
		if err != nil {
			log.Fatalln(err)
		}

		err = request.SubmitRead(ring)
		if err != nil {
			log.Fatalln(err)
		}

		i += 1
		j += len(request.Buffers)
	}

	cqe := (*uring.CQEvent)(nil)

	for j >= 0 {
		cqe, err = ring.WaitCQEvents(1)
		if err != nil {
			log.Fatalln(err)
		}

		request = (*Request)(unsafe.Pointer(uintptr(cqe.UserData)))
		request.Wait -= 1

		if request.Wait == 0 {
			fmt.Println(request.File.Name() + ":")

			for i = range request.Buffers {
				fmt.Printf("%s", request.Buffers[i])
			}

			fmt.Println()
		}

		j -= 1
	}
}

func (request *Request) SubmitRead(ring *uring.Ring) error {
	file := request.File

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	request.Buffers = getBuffers(stat.Size())

	i := 0
	j := uint64(0)

	for i < len(request.Buffers) {
		err = ring.QueueSQE(uring.Read(file.Fd(), request.Buffers[i], j), 0, uint64(uintptr(unsafe.Pointer(request))))
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
