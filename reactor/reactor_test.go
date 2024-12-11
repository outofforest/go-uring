//go:build linux

package reactor

import (
	"context"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/outofforest/go-uring/uring"
)

type ReactorTestSuite struct {
	suite.Suite

	defers uring.Defer

	reactor *Reactor

	stopReactor context.CancelFunc
	wg          *sync.WaitGroup
}

func (ts *ReactorTestSuite) SetupTest() {
	rings, defers, err := uring.CreateMany(4, 64, 4)
	ts.Require().NoError(err)
	ts.defers = defers

	ts.reactor, err = New(rings)
	ts.Require().NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	ts.stopReactor = cancel

	ts.wg = &sync.WaitGroup{}
	ts.wg.Add(1)
	go func() {
		defer ts.wg.Done()
		ts.reactor.Run(ctx)
	}()
}

func (ts *ReactorTestSuite) TearDownTest() {
	ts.stopReactor()
	ts.wg.Wait()
	ts.Require().NoError(ts.defers())
}

func (ts *ReactorTestSuite) TestReactorExecuteReadV() {
	f, err := os.Open("../go.mod")
	ts.Require().NoError(err)
	defer f.Close()

	expected, err := os.ReadFile("../go.mod")
	ts.Require().NoError(err)

	buff := make([]byte, len(expected))

	resultChan := make(chan uring.CQEvent)
	_, err = ts.reactor.Queue(uring.ReadV(f, [][]byte{buff}, 0), func(event uring.CQEvent) {
		resultChan <- event
	})
	ts.Require().NoError(err)

	cqe := <-resultChan

	ts.Require().Equal(string(expected), string(buff))

	ts.Require().Len(expected, int(cqe.Res))
}

func (ts *ReactorTestSuite) TestExecuteWithDeadline() {
	l, fd, err := makeTCPListener("0.0.0.0:8083")
	ts.Require().NoError(err)
	defer l.Close()

	acceptChan := make(chan uring.CQEvent)

	acceptTime := time.Now()
	_, err = ts.reactor.QueueWithDeadline(uring.Accept(uintptr(fd), 0), func(event uring.CQEvent) {
		acceptChan <- event
	}, acceptTime.Add(time.Second))
	ts.Require().NoError(err)

	cqe := <-acceptChan

	ts.Require().NoError(err)
	ts.Require().Error(cqe.Error(), syscall.ECANCELED)
	ts.Require().True(time.Since(acceptTime) > time.Second && time.Since(acceptTime) < time.Second+time.Millisecond*100)
}

func (ts *ReactorTestSuite) TestCancelOperation() {
	l, fd, err := makeTCPListener("0.0.0.0:8084")
	ts.Require().NoError(err)
	defer l.Close()

	acceptChan := make(chan uring.CQEvent)

	nonce, err := ts.reactor.Queue(uring.Accept(uintptr(fd), 0), func(event uring.CQEvent) {
		acceptChan <- event
	})
	ts.Require().NoError(err)

	go func() {
		<-time.After(time.Second)
		ts.Require().NoError(
			ts.reactor.Cancel(nonce),
		)
	}()

	cqe := <-acceptChan
	ts.Require().Error(cqe.Error(), syscall.ECANCELED)
}

func TestReactor(t *testing.T) {
	suite.Run(t, new(ReactorTestSuite))
}
