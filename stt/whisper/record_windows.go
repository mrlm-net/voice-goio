//go:build windows

package whisper

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/internal/wav"
)

// Windows capture through winmm's waveIn API, reached with syscall only, as
// audio plays through waveOut: no cgo, no dependency.

var (
	winmm    = syscall.NewLazyDLL("winmm.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procWaveInGetNumDevs      = winmm.NewProc("waveInGetNumDevs")
	procWaveInGetDevCapsW     = winmm.NewProc("waveInGetDevCapsW")
	procWaveInOpen            = winmm.NewProc("waveInOpen")
	procWaveInPrepareHeader   = winmm.NewProc("waveInPrepareHeader")
	procWaveInAddBuffer       = winmm.NewProc("waveInAddBuffer")
	procWaveInUnprepareHeader = winmm.NewProc("waveInUnprepareHeader")
	procWaveInStart           = winmm.NewProc("waveInStart")
	procWaveInReset           = winmm.NewProc("waveInReset")
	procWaveInClose           = winmm.NewProc("waveInClose")
	procWaveInGetErrorTextW   = winmm.NewProc("waveInGetErrorTextW")

	procCreateEventW        = kernel32.NewProc("CreateEventW")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
)

const (
	waveMapper    = 0xFFFFFFFF
	waveFormatPCM = 1
	callbackEvent = 0x00050000
	whdrDone      = 0x00000001
	recordRate    = 16000
	recordBufs    = 8 // of 100 ms each
)

type waveFormatEx struct {
	wFormatTag      uint16
	nChannels       uint16
	nSamplesPerSec  uint32
	nAvgBytesPerSec uint32
	nBlockAlign     uint16
	wBitsPerSample  uint16
	cbSize          uint16
}

// waveHdr mirrors WAVEHDR, pointer sized fields included.
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

type waveInCapsW struct {
	wMid           uint16
	wPid           uint16
	vDriverVersion uint32
	szPname        [32]uint16
	dwFormats      uint32
	wChannels      uint16
	wReserved1     uint16
}

func waveInError(op string, r uintptr) error {
	if r == 0 {
		return nil
	}
	var buf [256]uint16
	procWaveInGetErrorTextW.Call(r, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if txt := syscall.UTF16ToString(buf[:]); txt != "" {
		return fmt.Errorf("whisper: %s: %s (mmsyserr %d)", op, txt, r)
	}
	return fmt.Errorf("whisper: %s failed with mmsyserr %d", op, r)
}

// InputDevices lists the microphones: "" is the system default, then each
// waveIn device by its index. Names are cut at 31 characters by winmm.
func InputDevices() ([]voicegoio.Device, error) {
	n, _, _ := procWaveInGetNumDevs.Call()
	out := []voicegoio.Device{{ID: "", Name: "System default", Default: true}}
	var caps waveInCapsW
	for i := range int(n) {
		r, _, _ := procWaveInGetDevCapsW.Call(uintptr(i), uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps))
		if r != 0 {
			continue
		}
		out = append(out, voicegoio.Device{ID: strconv.Itoa(i), Name: syscall.UTF16ToString(caps.szPname[:])})
	}
	return out, nil
}

// inputIndex resolves a device: "" the default, an index, or the first
// device whose name contains it (any case).
func inputIndex(device string) (uintptr, error) {
	if device == "" {
		return waveMapper, nil
	}
	devs, _ := InputDevices()
	for _, d := range devs[1:] {
		if d.ID == device {
			i, _ := strconv.Atoi(d.ID)
			return uintptr(i), nil
		}
	}
	for _, d := range devs[1:] {
		if strings.Contains(strings.ToLower(d.Name), strings.ToLower(device)) {
			i, _ := strconv.Atoi(d.ID)
			return uintptr(i), nil
		}
	}
	return 0, fmt.Errorf("whisper: no microphone %q", device)
}

// WaveInRecorder records the microphone with winmm, 16 kHz mono 16 bit:
// the default recorder on Windows.
//
// A device can take most of a second from opening to its first sample (a
// wireless headset 0.8 to 1.1 s), so the start of a call made straight
// after the press would be lost. KeepOpen keeps the device open from the
// first Start to Close and starts each recording PreRoll before the
// press; between recordings only that PreRoll of audio is held.
type WaveInRecorder struct {
	// Device is "" for the system default, a device index or part of its
	// name (InputDevices).
	Device string
	// KeepOpen keeps the microphone open between recordings, until Close.
	KeepOpen bool
	// PreRoll is how much audio before Start a KeepOpen recording begins
	// with; 0: 300 ms.
	PreRoll time.Duration

	mu  sync.Mutex
	cur *waveIn
}

// waveIn is one open capture: the buffers are fields so they stay
// reachable while winmm holds pointers into them.
type waveIn struct {
	h, event uintptr
	bufs     [recordBufs][]byte
	hdrs     [recordBufs]waveHdr
	next     int
	stop     chan struct{}
	done     chan struct{}

	mu        sync.Mutex
	pcm       []int16
	recording bool
	mark      int // where the recording starts in pcm
	keep      int // samples held while not recording
}

// Warm opens the device without recording, when KeepOpen: a device takes
// up to a second to deliver its first sample.
func (w *WaveInRecorder) Warm() error {
	if !w.KeepOpen {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.open()
}

func (w *WaveInRecorder) open() error {
	if w.cur != nil {
		return nil
	}
	s, err := openWaveIn(w.Device)
	if err != nil {
		return err
	}
	pre := w.PreRoll
	if pre <= 0 {
		pre = 300 * time.Millisecond
	}
	s.keep = int(pre.Seconds() * recordRate)
	w.cur = s
	return nil
}

// Start starts a recording, opening the device unless it is open.
func (w *WaveInRecorder) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.open(); err != nil {
		return err
	}
	s := w.cur
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recording {
		return errors.New("whisper: already recording")
	}
	s.recording = true
	s.mark = max(0, len(s.pcm)-s.keep)
	if !w.KeepOpen {
		s.mark = len(s.pcm)
	}
	return nil
}

func openWaveIn(device string) (*waveIn, error) {
	idx, err := inputIndex(device)
	if err != nil {
		return nil, err
	}
	ev, _, e := procCreateEventW.Call(0, 0, 0, 0)
	if ev == 0 {
		return nil, fmt.Errorf("whisper: CreateEvent: %w", e)
	}
	f := waveFormatEx{wFormatTag: waveFormatPCM, nChannels: 1, nSamplesPerSec: recordRate,
		nAvgBytesPerSec: recordRate * 2, nBlockAlign: 2, wBitsPerSample: 16}
	s := &waveIn{event: ev, stop: make(chan struct{}), done: make(chan struct{})}
	r, _, _ := procWaveInOpen.Call(uintptr(unsafe.Pointer(&s.h)), idx, uintptr(unsafe.Pointer(&f)), ev, 0, callbackEvent)
	if err := waveInError("waveInOpen", r); err != nil {
		procCloseHandle.Call(ev)
		return nil, err
	}
	for i := range s.bufs {
		s.bufs[i] = make([]byte, recordRate/10*2)
		s.hdrs[i] = waveHdr{lpData: uintptr(unsafe.Pointer(&s.bufs[i][0])), dwBufferLength: uint32(len(s.bufs[i]))}
		r, _, _ := procWaveInPrepareHeader.Call(s.h, uintptr(unsafe.Pointer(&s.hdrs[i])), unsafe.Sizeof(s.hdrs[i]))
		if err == nil {
			err = waveInError("waveInPrepareHeader", r)
		}
		r, _, _ = procWaveInAddBuffer.Call(s.h, uintptr(unsafe.Pointer(&s.hdrs[i])), unsafe.Sizeof(s.hdrs[i]))
		if err == nil {
			err = waveInError("waveInAddBuffer", r)
		}
	}
	if err == nil {
		r, _, _ := procWaveInStart.Call(s.h)
		err = waveInError("waveInStart", r)
	}
	if err != nil {
		s.close()
		return nil, err
	}
	go s.run()
	return s, nil
}

// run collects filled buffers and gives them back to the device until
// stopped.
func (s *waveIn) run() {
	defer close(s.done)
	for {
		select {
		case <-s.stop:
			procWaveInReset.Call(s.h) // every buffer returned, marked done
			s.collect(false)
			s.close()
			return
		default:
		}
		procWaitForSingleObject.Call(s.event, 50)
		s.collect(true)
	}
}

// collect appends the buffers done, in the order they were queued, and with
// requeue hands each back to the device. Not recording, only keep samples
// are held.
func (s *waveIn) collect(requeue bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for range recordBufs {
		h := &s.hdrs[s.next]
		if atomic.LoadUint32(&h.dwFlags)&whdrDone == 0 {
			break
		}
		b := s.bufs[s.next][:h.dwBytesRecorded]
		for i := 0; i+1 < len(b); i += 2 {
			s.pcm = append(s.pcm, int16(uint16(b[i])|uint16(b[i+1])<<8))
		}
		if requeue {
			atomic.StoreUint32(&h.dwFlags, atomic.LoadUint32(&h.dwFlags)&^whdrDone)
			h.dwBytesRecorded = 0
			procWaveInAddBuffer.Call(s.h, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
		}
		s.next = (s.next + 1) % recordBufs
	}
	if !s.recording && len(s.pcm) > 2*s.keep {
		s.pcm = append(s.pcm[:0], s.pcm[len(s.pcm)-s.keep:]...)
	}
}

func (s *waveIn) close() {
	procWaveInReset.Call(s.h)
	for i := range s.hdrs {
		procWaveInUnprepareHeader.Call(s.h, uintptr(unsafe.Pointer(&s.hdrs[i])), unsafe.Sizeof(s.hdrs[i]))
	}
	procWaveInClose.Call(s.h)
	procCloseHandle.Call(s.event)
}

// shut stops the capture and waits for the device to be closed.
func (s *waveIn) shut() {
	close(s.stop)
	<-s.done
}

// Stop ends the recording and writes what was heard to a temporary WAV.
// Without KeepOpen the device is closed.
func (w *WaveInRecorder) Stop() (string, error) {
	w.mu.Lock()
	s := w.cur
	if s != nil && !w.KeepOpen {
		w.cur = nil
	}
	w.mu.Unlock()
	if s == nil {
		return "", errors.New("whisper: not recording")
	}
	if !w.KeepOpen {
		s.shut()
	} else {
		// What is already in the device's buffers belongs to this call.
		time.Sleep(120 * time.Millisecond)
		s.collect(true)
	}
	s.mu.Lock()
	if !s.recording {
		s.mu.Unlock()
		return "", errors.New("whisper: not recording")
	}
	pcm := append([]int16(nil), s.pcm[min(s.mark, len(s.pcm)):]...)
	s.recording = false
	s.mu.Unlock()
	if len(pcm) == 0 {
		return "", errors.New("whisper: the microphone recorded nothing")
	}
	f, err := os.CreateTemp("", "voicegoio-ptt-*.wav")
	if err != nil {
		return "", err
	}
	f.Close()
	if err := wav.WriteFile(f.Name(), pcm, recordRate); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// Close closes the microphone if it is open (KeepOpen).
func (w *WaveInRecorder) Close() error {
	w.mu.Lock()
	s := w.cur
	w.cur = nil
	w.mu.Unlock()
	if s != nil {
		s.shut()
	}
	return nil
}

// MicRecorder is the platform's recorder for a microphone (InputDevices),
// kept open between presses so a call is heard from its first word; the
// recogniser's Close closes it.
func MicRecorder(device string) Recorder { return &WaveInRecorder{Device: device, KeepOpen: true} }
