package engine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
)

// Limits that keep UserWave memory in check on a device that is also running
// Live. A wave is stored as float32 mono.
const (
	MaxWaveSeconds  = 10
	MaxWaveSamples  = 4 << 20 // across every slot together: about 16 MB
	maxWaveFileSize = 128 << 20
)

// ErrWaveTooLong means the sample is over MaxWaveSeconds.
var ErrWaveTooLong = errors.New("sample is longer than 10 seconds")

// DecodeWAV reads a RIFF/WAVE file and returns its audio mixed down to mono
// float32 in -1..1, with its sample rate. It reads 8, 16, 24 and 32 bit PCM
// and 32 and 64 bit float, any channel count, and WAVE_FORMAT_EXTENSIBLE.
func DecodeWAV(r io.Reader) (data []float32, rate int, err error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxWaveFileSize+1))
	if err != nil {
		return nil, 0, err
	}
	if len(raw) > maxWaveFileSize {
		return nil, 0, errors.New("file is too large")
	}
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, 0, errors.New("not a WAV file (no RIFF/WAVE header)")
	}
	var (
		format, channels, bits, blockAlign int
		haveFmt                            bool
		pcm                                []byte
	)
	pos := 12
	for pos+8 <= len(raw) {
		id := string(raw[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(raw[pos+4 : pos+8]))
		body := pos + 8
		end := body + size
		if end > len(raw) || end < body { // a streamed file may lie about its size
			end = len(raw)
		}
		switch id {
		case "fmt ":
			if end-body < 16 {
				return nil, 0, errors.New("fmt chunk is too short")
			}
			f := raw[body:end]
			format = int(binary.LittleEndian.Uint16(f[0:2]))
			channels = int(binary.LittleEndian.Uint16(f[2:4]))
			rate = int(binary.LittleEndian.Uint32(f[4:8]))
			blockAlign = int(binary.LittleEndian.Uint16(f[12:14]))
			bits = int(binary.LittleEndian.Uint16(f[14:16]))
			if format == 0xFFFE && len(f) >= 26 {
				format = int(binary.LittleEndian.Uint16(f[24:26]))
			}
			haveFmt = true
		case "data":
			pcm = raw[body:end]
		}
		pos = end
		if size%2 == 1 && pos < len(raw) {
			pos++
		}
		if haveFmt && pcm != nil {
			break
		}
	}
	if !haveFmt {
		return nil, 0, errors.New("WAV has no fmt chunk")
	}
	if pcm == nil {
		return nil, 0, errors.New("WAV has no data chunk")
	}
	if channels < 1 || channels > 32 {
		return nil, 0, fmt.Errorf("unsupported channel count %d", channels)
	}
	if rate < 1000 || rate > 384000 {
		return nil, 0, fmt.Errorf("unsupported sample rate %d", rate)
	}
	bytesPer := bits / 8
	if blockAlign != channels*bytesPer {
		blockAlign = channels * bytesPer
	}
	if bytesPer < 1 || blockAlign == 0 {
		return nil, 0, fmt.Errorf("unsupported bit depth %d", bits)
	}
	frames := len(pcm) / blockAlign
	if frames == 0 {
		return nil, 0, errors.New("WAV holds no audio")
	}
	if float64(frames)/float64(rate) > MaxWaveSeconds {
		return nil, 0, ErrWaveTooLong
	}
	data = make([]float32, frames)
	sample := func(b []byte) (float32, bool) {
		switch {
		case format == 1 && bits == 8:
			return (float32(b[0]) - 128) / 128, true
		case format == 1 && bits == 16:
			return float32(int16(binary.LittleEndian.Uint16(b))) / 32768, true
		case format == 1 && bits == 24:
			v := int32(b[0]) | int32(b[1])<<8 | int32(int8(b[2]))<<16
			return float32(v) / 8388608, true
		case format == 1 && bits == 32:
			return float32(int32(binary.LittleEndian.Uint32(b))) / 2147483648, true
		case format == 3 && bits == 32:
			return math.Float32frombits(binary.LittleEndian.Uint32(b)), true
		case format == 3 && bits == 64:
			return float32(math.Float64frombits(binary.LittleEndian.Uint64(b))), true
		}
		return 0, false
	}
	if _, ok := sample(make([]byte, 8)); !ok {
		return nil, 0, fmt.Errorf("unsupported WAV format %d with %d bits (use PCM or float)", format, bits)
	}
	inv := 1 / float32(channels)
	for f := 0; f < frames; f++ {
		var sum float32
		for c := 0; c < channels; c++ {
			s, _ := sample(pcm[f*blockAlign+c*bytesPer:])
			sum += s
		}
		v := sum * inv
		if v != v || v > 8 || v < -8 { // NaN or garbage
			v = 0
		}
		data[f] = v
	}
	return data, rate, nil
}

// EncodeWAV16 writes 16-bit PCM. channels is 1 (data is mono) or 2 (data is
// interleaved L,R).
func EncodeWAV16(data []float32, rate, channels int) []byte {
	var b bytes.Buffer
	dataLen := len(data) * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataLen))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*channels*2))
	binary.Write(&b, binary.LittleEndian, uint16(channels*2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(dataLen))
	for _, v := range data {
		if v > 1 {
			v = 1
		}
		if v < -1 {
			v = -1
		}
		binary.Write(&b, binary.LittleEndian, int16(math.Round(float64(v)*32767)))
	}
	return b.Bytes()
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9 _.+-]+`)

// CleanWaveName makes a short, file-safe display name from a file name: no
// extension, no path, ASCII only (the Push screen draws ASCII only), at
// most 12 characters.
func CleanWaveName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndex(name, "."); i > 0 {
		name = name[:i]
	}
	name = strings.TrimSpace(unsafeName.ReplaceAllString(name, ""))
	if len(name) > 12 {
		name = strings.TrimSpace(name[:12])
	}
	if name == "" {
		name = "WAVE"
	}
	return name
}
