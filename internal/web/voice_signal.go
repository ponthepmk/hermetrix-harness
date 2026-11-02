package web

import (
	"encoding/binary"
	"math"
)

// This is a conservative signal gate, not a speech classifier. Reject silence,
// DC offsets and isolated clicks before ASR; preserve language-independent audio.
func voiceSignalPresent(pcm []byte) bool {
	const frameBytes = 640 // 20 ms, mono PCM16 at 16 kHz.
	active := 0
	for start := 0; start+frameBytes <= len(pcm); start += frameBytes {
		var sum, squares float64
		for i := start; i < start+frameBytes; i += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32768
			sum += v
			squares += v * v
		}
		variance := squares/320 - math.Pow(sum/320, 2)
		if variance > .006*.006 {
			active++
		}
	}
	return active >= 8 // At least 160 ms; isolated transients are not utterances.
}

func voiceWAVSignalPresent(wav []byte) bool {
	if len(wav) < 12 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return false
	}
	for offset := 12; offset+8 <= len(wav); {
		size := uint64(binary.LittleEndian.Uint32(wav[offset+4:]))
		start := offset + 8
		if size > uint64(len(wav)-start) {
			return false
		}
		if string(wav[offset:offset+4]) == "data" {
			return voiceSignalPresent(wav[start : start+int(size)])
		}
		offset = start + int(size) + int(size%2)
	}
	return false
}
