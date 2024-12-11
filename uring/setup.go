//go:build linux

package uring

import (
	"errors"
	"syscall"
	"unsafe"
)

type (
	ringParams struct {
		sqEntries    uint32
		cqEntries    uint32
		flags        uint32
		sqThreadCpu  uint32
		sqThreadIdle uint32
		features     uint32
		wqFD         uint32
		resv         [3]uint32
		sqOffset     sqRingParams
		cqOffset     cqRingParams
	}
	sqRingParams struct {
		head        uint32
		tail        uint32
		ringMask    uint32
		ringEntries uint32
		flags       uint32
		dropped     uint32
		array       uint32
		resv1       uint32
		resv2       uint64
	}
	cqRingParams struct {
		head        uint32
		tail        uint32
		ringMsk     uint32
		ringEntries uint32
		overflow    uint32
		cqes        uint32
		flags       uint32
		resv1       uint32
		resv2       uint64
	}
)

// io_uring_setup() flags
const (
	SetupIOPoll       uint32 = 1 << 0
	SetupSQPoll       uint32 = 1 << 1
	SetupSQAff        uint32 = 1 << 2
	SetupCQSize       uint32 = 1 << 3 /* app defines CQ size */
	SetupClamp        uint32 = 1 << 4
	SetupAttachWQ     uint32 = 1 << 5
	SetupRDisabled    uint32 = 1 << 6
	SetupCoopTaskRun  uint32 = 1 << 8
	SetupSingleIssuer uint32 = 1 << 12
)

const (
	cqRingOffset uint64 = 0x8000000
	sqesOffset   uint64 = 0x10000000
)

// feature flags
const (
	featSingleMMap uint32 = 1 << 0
	featNoDrop     uint32 = 1 << 1
	featFastPoll   uint32 = 1 << 5
	featExtArg     uint32 = 1 << 8
)

func (p *ringParams) SingleMMapFeature() bool {
	return p.features&featSingleMMap != 0
}

func (p *ringParams) NoDropFeature() bool {
	return p.features&featNoDrop != 0
}

func (p *ringParams) FastPollFeature() bool {
	return p.features&featFastPoll != 0
}

func (p *ringParams) ExtArgFeature() bool {
	return p.features&featExtArg != 0
}

func (r *Ring) allocRing(params *ringParams) error {
	r.sqRing.ringSize = uint64(params.sqOffset.array) + uint64(params.sqEntries*(uint32)(unsafe.Sizeof(uint32(0))))
	r.cqRing.ringSize = uint64(params.cqOffset.cqes) + uint64(params.cqEntries*(uint32)(unsafe.Sizeof(CQEvent{})))

	if params.SingleMMapFeature() {
		if r.cqRing.ringSize > r.sqRing.ringSize {
			r.sqRing.ringSize = r.cqRing.ringSize
		}
		r.cqRing.ringSize = r.sqRing.ringSize
	}

	data, err := syscall.Mmap(r.fd, 0, int(r.sqRing.ringSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED|syscall.MAP_POPULATE)
	if err != nil {
		return err
	}
	r.sqRing.buff = data

	if params.SingleMMapFeature() {
		r.cqRing.buff = r.sqRing.buff
	} else {
		data, err = syscall.Mmap(r.fd, int64(cqRingOffset), int(r.cqRing.ringSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED|syscall.MAP_POPULATE)
		if err != nil {
			_ = r.freeRing()
			return err
		}
		r.cqRing.buff = data
	}

	ringStart := unsafe.SliceData(r.sqRing.buff)
	r.sqRing.kHead = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.sqOffset.head)))
	r.sqRing.kTail = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.sqOffset.tail)))
	r.sqRing.kRingMask = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.sqOffset.ringMask)))
	r.sqRing.kRingEntries = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.sqOffset.ringEntries)))
	r.sqRing.kFlags = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.sqOffset.flags)))
	r.sqRing.kDropped = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.sqOffset.dropped)))
	r.sqRing.kArray = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.sqOffset.array)))

	sz := uintptr(params.sqEntries) * unsafe.Sizeof(SQEntry{})
	buff, err := syscall.Mmap(r.fd, int64(sqesOffset), int(sz), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED|syscall.MAP_POPULATE)
	if err != nil {
		_ = r.freeRing()
		return err
	}
	r.sqRing.sqeBuff = buff

	ptr := unsafe.SliceData(r.cqRing.buff)

	cqRingPtr := uintptr(unsafe.Pointer(ptr))
	ringStart = ptr

	r.cqRing.kHead = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.cqOffset.head)))
	r.cqRing.kTail = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.cqOffset.tail)))
	r.cqRing.kRingMask = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.cqOffset.ringMsk)))
	r.cqRing.kRingEntries = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.cqOffset.ringEntries)))
	r.cqRing.kOverflow = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.cqOffset.overflow)))
	r.cqRing.cqeBuff = (*CQEvent)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(params.cqOffset.cqes)))
	if params.cqOffset.flags != 0 {
		r.cqRing.kFlags = cqRingPtr + uintptr(params.cqOffset.flags)
	}

	return nil
}

func (r *Ring) freeRing() (err error) {
	err = syscall.Munmap(r.sqRing.buff)

	if r.cqRing.buff == nil || unsafe.SliceData(r.cqRing.buff) == unsafe.SliceData(r.sqRing.buff) {
		return err
	}

	return errors.Join(err, syscall.Munmap(r.cqRing.buff))
}
