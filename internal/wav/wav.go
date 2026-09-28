// Package wav reads and writes the small subset of RIFF/WAVE the library needs:
// uncompressed 16 bit PCM.
//
// It exists because the macOS player shells out to afplay (which needs a file),
// the fallback player writes files, and cmd/voicecheck dumps audio for a
// listening pass. Nothing here allocates more than the sample data itself.
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	formatPCM = 1
	hdrSize   = 44
)

// Header returns a 44 byte canonical WAVE header for n samples of 16 bit PCM.
func Header(n, sampleRate, channels int) []byte {
	dataBytes := n * 2
	b := make([]byte, hdrSize)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+dataBytes))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(b[20:], formatPCM)
	binary.LittleEndian.PutUint16(b[22:], uint16(channels))
	binary.LittleEndian.PutUint32(b[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(b[28:], uint32(sampleRate*channels*2)) // byte rate
	binary.LittleEndian.PutUint16(b[32:], uint16(channels*2))            // block align
	binary.LittleEndian.PutUint16(b[34:], 16)                            // bits per sample
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(dataBytes))
	return b
}

// Write emits a mono 16 bit WAVE stream.
func Write(w io.Writer, pcm []int16, sampleRate int) error {
	if _, err := w.Write(Header(len(pcm), sampleRate, 1)); err != nil {
		return err
	}
	buf := make([]byte, len(pcm)*2)
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	_, err := w.Write(buf)
	return err
}

// WriteFile writes pcm to path, creating parent-less files only.
func WriteFile(path string, pcm []int16, sampleRate int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := Write(f, pcm, sampleRate); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Read parses a 16 bit PCM WAVE file and returns mono samples. Stereo input is
// downmixed, because every consumer in this library is mono.
func Read(r io.Reader) (pcm []int16, sampleRate int, err error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, err
	}
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, 0, errors.New("wav: not a RIFF/WAVE stream")
	}
	var channels, bits int
	off := 12
	for off+8 <= len(raw) {
		id := string(raw[off : off+4])
		size := int(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
		body := off + 8
		if body+size > len(raw) {
			size = len(raw) - body // tolerate a truncated final chunk
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, errors.New("wav: short fmt chunk")
			}
			if f := binary.LittleEndian.Uint16(raw[body:]); f != formatPCM {
				return nil, 0, fmt.Errorf("wav: unsupported format %d, want PCM", f)
			}
			channels = int(binary.LittleEndian.Uint16(raw[body+2:]))
			sampleRate = int(binary.LittleEndian.Uint32(raw[body+4:]))
			bits = int(binary.LittleEndian.Uint16(raw[body+14:]))
		case "data":
			if bits != 16 {
				return nil, 0, fmt.Errorf("wav: unsupported bit depth %d, want 16", bits)
			}
			if channels < 1 {
				channels = 1
			}
			frames := size / 2 / channels
			pcm = make([]int16, frames)
			for i := range frames {
				var acc int
				for c := range channels {
					acc += int(int16(binary.LittleEndian.Uint16(raw[body+(i*channels+c)*2:])))
				}
				pcm[i] = int16(acc / channels)
			}
			return pcm, sampleRate, nil
		}
		off = body + size
		if size%2 == 1 {
			off++ // RIFF chunks are word aligned
		}
	}
	return nil, 0, errors.New("wav: no data chunk")
}

// ReadFile is Read on a path.
func ReadFile(path string) ([]int16, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	return Read(f)
}
