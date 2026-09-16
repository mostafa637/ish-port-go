package time

import (
	"encoding/binary"
	stdtime "time"
)

func clockData(id uint32, start stdtime.Time) []byte {
	d, now, b := stdtime.Since(start), stdtime.Now(), make([]byte, 8)
	if id == 1 {
		binary.LittleEndian.PutUint32(b, uint32(d/stdtime.Second))
		binary.LittleEndian.PutUint32(b[4:], uint32(d%stdtime.Second))
	} else {
		binary.LittleEndian.PutUint32(b, uint32(now.Unix()))
		binary.LittleEndian.PutUint32(b[4:], uint32(now.Nanosecond()))
	}
	return b
}
