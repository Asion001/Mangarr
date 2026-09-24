package komgaapi

import (
	"crypto/md5"
	"encoding/hex"
	"testing"
)

// KOReader samples 1 KiB at offsets 0, 1024, 4096, 16384, …; a 5000-byte
// file gives samples [0,1024), [1024,2048) and [4096,5000).
func TestArchivePartialMD5MatchesKOReader(t *testing.T) {
	data := make([]byte, 5000)
	for i := range data {
		data[i] = byte(i * 7)
	}
	var want []byte
	want = append(want, data[0:1024]...)
	want = append(want, data[1024:2048]...)
	want = append(want, data[4096:5000]...)
	sum := md5.Sum(want)
	if got := archivePartialMD5(data); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("partial MD5 = %s, want %x", got, sum)
	}
}
