//go:build windows

package audio

// Windows playback through winmm's waveOut API, reached with syscall only: no
// cgo, no third party bindings.
//
// waveOut is the oldest of the three Windows audio APIs (waveOut, DirectSound,
// WASAPI) and the only one usable without COM interface plumbing. For ATC
// audio, whose buffers are whole utterances, its latency is irrelevant and its
// device enumeration is exactly what the settings UI needs: this is what lets a
// user route ATC to a headset while the sim keeps the speakers.

import (
	"fmt"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	voicegoio "github.com/mrlm-net/voice-goio"
)

var (
	winmm    = syscall.NewLazyDLL("winmm.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procWaveOutGetNumDevs      = winmm.NewProc("waveOutGetNumDevs")
	procWaveOutGetDevCapsW     = winmm.NewProc("waveOutGetDevCapsW")
	procWaveOutOpen            = winmm.NewProc("waveOutOpen")
	procWaveOutPrepareHeader   = winmm.NewProc("waveOutPrepareHeader")
	procWaveOutWrite           = winmm.NewProc("waveOutWrite")
	procWaveOutUnprepareHeader = winmm.NewProc("waveOutUnprepareHeader")
	procWaveOutReset           = winmm.NewProc("waveOutReset")
	procWaveOutClose           = winmm.NewProc("waveOutClose")
	procWaveOutGetErrorTextW   = winmm.NewProc("waveOutGetErrorTextW")

	procCreateEventW        = kernel32.NewProc("CreateEventW")
	procResetEvent          = kernel32.NewProc("ResetEvent")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
)

const (
	waveMapper      = 0xFFFFFFFF
	waveFormatPCM   = 1
	callbackEvent   = 0x00050000
	whdrDone        = 0x00000001
	mmsyserrNoError = 0
	infinite        = 0xFFFFFFFF
)

// waveFormatEx describes the stream format. cbSize stays zero for plain PCM.
type waveFormatEx struct {
	wFormatTag      uint16
	nChannels       uint16
	nSamplesPerSec  uint32
	nAvgBytesPerSec uint32
	nBlockAlign     uint16
	wBitsPerSample  uint16
	cbSize          uint16
}

// waveHdr mirrors WAVEHDR. The layout must match the C struct exactly,
// including the pointer sized dwUser and reserved fields.
type waveHdr struct {
	lpData          uintptr
	dwBufferLength  uint32
	dwBytesRecorded uint32
	dwUser          uintptr
	dwFlags         uint32
	dwLoops         uint32
	lpNext          uintptr
	reserved        uintptr
}

type waveOutCapsW struct {
	wMid           uint16
	wPid           uint16
	vDriverVersion uint32
	szPname        [32]uint16
	dwFormats      uint32
	wChannels      uint16
	wReserved1     uint16
	dwSupport      uint32
}

// winSink is one open waveOut handle plus the two buffers it alternates
// between. The buffers are fields rather than locals so they stay reachable for
// the whole time winmm holds pointers into them.
type winSink struct {
	mu    sync.Mutex
	h     uintptr
	event uintptr
	rate  int
	open_ bool
	bufs  [2][]byte
	hdrs  [2]waveHdr
	chunk int // frames per buffer
}

func newSink() (sink, error) { return &winSink{}, nil }

func (s *winSink) selectable() bool { return true }

func mmError(op string, r uintptr) error {
	if r == mmsyserrNoError {
		return nil
	}
	var buf [256]uint16
	procWaveOutGetErrorTextW.Call(r, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if txt := syscall.UTF16ToString(buf[:]); txt != "" {
		return fmt.Errorf("audio: %s: %s (mmsyserr %d)", op, txt, r)
	}
	return fmt.Errorf("audio: %s failed with mmsyserr %d", op, r)
}

func (s *winSink) devices() ([]voicegoio.Device, error) {
	n, _, _ := procWaveOutGetNumDevs.Call()
	out := make([]voicegoio.Device, 0, int(n)+1)
	out = append(out, voicegoio.Device{ID: DefaultDevice, Name: "System default", Default: true})
	var caps waveOutCapsW
	for i := range int(n) {
		r, _, _ := procWaveOutGetDevCapsW.Call(uintptr(i), uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps))
		if r != mmsyserrNoError {
			continue
		}
		out = append(out, voicegoio.Device{ID: strconv.Itoa(i), Name: syscall.UTF16ToString(caps.szPname[:])})
	}
	return out, nil
}

func deviceIndex(id string) (uintptr, error) {
	if id == DefaultDevice {
		return waveMapper, nil
	}
	i, err := strconv.Atoi(id)
	if err != nil || i < 0 {
		return 0, fmt.Errorf("audio: %q is not a waveOut device id", id)
	}
	return uintptr(i), nil
}

func (s *winSink) open(deviceID string, sampleRate int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := deviceIndex(deviceID)
	if err != nil {
		return err
	}
	// Auto reset event: winmm signals it as each buffer completes.
	ev, _, e := procCreateEventW.Call(0, 0, 0, 0)
	if ev == 0 {
		return fmt.Errorf("audio: CreateEvent: %w", e)
	}
	fmtx := waveFormatEx{
		wFormatTag:      waveFormatPCM,
		nChannels:       1,
		nSamplesPerSec:  uint32(sampleRate),
		nAvgBytesPerSec: uint32(sampleRate * 2),
		nBlockAlign:     2,
		wBitsPerSample:  16,
	}
	var h uintptr
	r, _, _ := procWaveOutOpen.Call(
		uintptr(unsafe.Pointer(&h)), idx,
		uintptr(unsafe.Pointer(&fmtx)),
		ev, 0, callbackEvent,
	)
	if err := mmError("waveOutOpen", r); err != nil {
		procCloseHandle.Call(ev)
		return err
	}
	s.h, s.event, s.rate, s.open_ = h, ev, sampleRate, true
	// 100 ms per buffer: long enough that two buffers never underrun on a busy
	// machine, short enough that Close stops audio promptly.
	s.chunk = sampleRate / 10
	for i := range s.bufs {
		s.bufs[i] = make([]byte, s.chunk*2)
		s.hdrs[i] = waveHdr{}
	}
	return nil
}

// write plays pcm, alternating between the two prepared buffers so the device
// is never starved between chunks.
func (s *winSink) write(pcm []int16, gain func() float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.open_ {
		return voicegoio.ErrClosed
	}
	inflight := [2]bool{}
	for off := 0; off < len(pcm); off += s.chunk {
		i := (off / s.chunk) % 2
		if inflight[i] {
			if err := s.waitDone(i); err != nil {
				return err
			}
			inflight[i] = false
		}
		end := min(off+s.chunk, len(pcm))
		n := end - off
		b := s.bufs[i]
		g := gain() // per chunk: a knob turned is heard within one
		for j, sample := range pcm[off:end] {
			if g < 1 {
				sample = int16(float64(sample) * g)
			}
			b[j*2] = byte(uint16(sample))
			b[j*2+1] = byte(uint16(sample) >> 8)
		}
		s.hdrs[i] = waveHdr{lpData: uintptr(unsafe.Pointer(&b[0])), dwBufferLength: uint32(n * 2)}
		if err := mmError("waveOutPrepareHeader", s.call(procWaveOutPrepareHeader, i)); err != nil {
			return err
		}
		if err := mmError("waveOutWrite", s.call(procWaveOutWrite, i)); err != nil {
			s.call(procWaveOutUnprepareHeader, i)
			return err
		}
		inflight[i] = true
	}
	for i := range inflight {
		if inflight[i] {
			if err := s.waitDone(i); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *winSink) call(proc *syscall.LazyProc, i int) uintptr {
	r, _, _ := proc.Call(s.h, uintptr(unsafe.Pointer(&s.hdrs[i])), unsafe.Sizeof(s.hdrs[i]))
	return r
}

// waitDone blocks until winmm marks buffer i complete, then unprepares it.
func (s *winSink) waitDone(i int) error {
	for s.hdrs[i].dwFlags&whdrDone == 0 {
		if r, _, _ := procWaitForSingleObject.Call(s.event, infinite); r != 0 {
			return fmt.Errorf("audio: wait for buffer: result %d", r)
		}
	}
	procResetEvent.Call(s.event)
	return mmError("waveOutUnprepareHeader", s.call(procWaveOutUnprepareHeader, i))
}

func (s *winSink) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.open_ {
		return nil
	}
	s.open_ = false
	procWaveOutReset.Call(s.h)
	for i := range s.hdrs {
		if s.hdrs[i].dwFlags != 0 {
			s.call(procWaveOutUnprepareHeader, i)
		}
	}
	r, _, _ := procWaveOutClose.Call(s.h)
	procCloseHandle.Call(s.event)
	s.h, s.event = 0, 0
	return mmError("waveOutClose", r)
}
