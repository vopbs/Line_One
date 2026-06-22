package media

import (
	"encoding/binary"
	"fmt"
	"os"
)

type WAV struct {
	SampleRate int
	Samples    []int16
}

func ReadWAV(path string) (WAV, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return WAV{}, err
	}
	return ParseWAV(data)
}

func ParseWAV(data []byte) (WAV, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return WAV{}, fmt.Errorf("audio file must be PCM WAV")
	}

	var format, channels, bits uint16
	var sampleRate uint32
	var pcm []byte
	for offset := 12; offset+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start := offset + 8
		end := start + size
		if end > len(data) {
			return WAV{}, fmt.Errorf("invalid WAV chunk")
		}
		switch string(data[offset : offset+4]) {
		case "fmt ":
			if size < 16 {
				return WAV{}, fmt.Errorf("invalid WAV format")
			}
			format = binary.LittleEndian.Uint16(data[start : start+2])
			channels = binary.LittleEndian.Uint16(data[start+2 : start+4])
			sampleRate = binary.LittleEndian.Uint32(data[start+4 : start+8])
			bits = binary.LittleEndian.Uint16(data[start+14 : start+16])
		case "data":
			pcm = data[start:end]
		}
		offset = end + size%2
	}
	if format != 1 || channels == 0 || sampleRate == 0 || len(pcm) == 0 {
		return WAV{}, fmt.Errorf("only uncompressed PCM WAV is supported")
	}
	if bits != 8 && bits != 16 {
		return WAV{}, fmt.Errorf("only 8-bit or 16-bit PCM WAV is supported")
	}

	bytesPerSample := int(bits / 8)
	frameSize := bytesPerSample * int(channels)
	samples := make([]int16, 0, len(pcm)/frameSize)
	for offset := 0; offset+frameSize <= len(pcm); offset += frameSize {
		total := 0
		for channel := 0; channel < int(channels); channel++ {
			pos := offset + channel*bytesPerSample
			if bits == 16 {
				total += int(int16(binary.LittleEndian.Uint16(pcm[pos : pos+2])))
			} else {
				total += (int(pcm[pos]) - 128) << 8
			}
		}
		samples = append(samples, int16(total/int(channels)))
	}
	return WAV{SampleRate: int(sampleRate), Samples: resample(samples, int(sampleRate), 8000)}, nil
}

func resample(input []int16, sourceRate, targetRate int) []int16 {
	if sourceRate == targetRate || len(input) == 0 {
		return input
	}
	length := len(input) * targetRate / sourceRate
	output := make([]int16, length)
	for i := range output {
		position := float64(i) * float64(sourceRate) / float64(targetRate)
		left := int(position)
		if left >= len(input)-1 {
			output[i] = input[len(input)-1]
			continue
		}
		fraction := position - float64(left)
		output[i] = int16(float64(input[left])*(1-fraction) + float64(input[left+1])*fraction)
	}
	return output
}
