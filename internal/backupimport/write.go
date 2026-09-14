package backupimport

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"math"
	"strconv"
)

// MarshalMihon writes b as a gzipped Mihon backup (the fields this package
// reads). Tests use it to build backups.
func MarshalMihon(b *Backup) []byte {
	var out enc
	cats := map[string]int64{}
	for i, c := range b.Categories {
		cats[c] = int64(i + 1)
	}
	for _, e := range b.Entries {
		m := &enc{}
		src, _ := strconv.ParseInt(e.SourceID, 10, 64)
		m.varint(1, src).str(2, e.URL).str(3, e.Title).str(4, e.Artist).str(5, e.Author).str(6, e.Description)
		for _, g := range e.Genres {
			m.str(7, g)
		}
		m.varint(8, map[string]int64{"ongoing": 1, "completed": 2, "cancelled": 5, "hiatus": 6}[e.Status]).str(9, e.ThumbnailURL)
		if !e.AddedAt.IsZero() {
			m.varint(13, e.AddedAt.UnixMilli())
		}
		for _, c := range e.Chapters {
			ch := (&enc{}).str(1, c.URL).str(2, c.Name).str(3, c.Scanlator).bool(4, c.Read).varint(6, int64(c.LastPageRead)).float(9, float32(c.Number))
			m.msg(16, ch)
		}
		for _, c := range e.Categories {
			if o, ok := cats[c]; ok {
				m.varint(17, o)
			}
		}
		for tracker, id := range e.Trackers {
			sync := map[string]int64{TrackerMAL: 1, TrackerAniList: 2, TrackerKitsu: 3, TrackerMangaUpdates: 7}[tracker]
			n, _ := strconv.ParseInt(id, 10, 64)
			if sync != 0 && n != 0 {
				m.msg(18, (&enc{}).varint(1, sync).varint(100, n))
			}
		}
		if !e.Favorite {
			m.key(100, wireVarint)
			m.b = append(m.b, 0)
		}
		for _, c := range e.Chapters {
			if c.ReadAt != nil {
				m.msg(104, (&enc{}).str(1, c.URL).varint(2, c.ReadAt.UnixMilli()))
			}
		}
		for _, s := range e.ExcludedScanlators {
			m.str(108, s)
		}
		out.msg(1, m)
	}
	for i, c := range b.Categories {
		out.msg(2, (&enc{}).str(1, c).varint(2, int64(i+1)))
	}
	for id, name := range b.Sources {
		n, _ := strconv.ParseInt(id, 10, 64)
		out.msg(101, (&enc{}).str(1, name).varint(2, n))
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(out.b)
	_ = w.Close()
	return buf.Bytes()
}

type enc struct{ b []byte }

func (p *enc) key(num int, typ wireType) { p.b = binary.AppendUvarint(p.b, uint64(num)<<3|uint64(typ)) }

func (p *enc) varint(num int, v int64) *enc {
	if v == 0 {
		return p // defaults are omitted, like kotlinx does
	}
	p.key(num, wireVarint)
	p.b = binary.AppendUvarint(p.b, uint64(v))
	return p
}

func (p *enc) bool(num int, v bool) *enc {
	if v {
		return p.varint(num, 1)
	}
	return p
}

func (p *enc) str(num int, s string) *enc {
	if s == "" {
		return p
	}
	p.key(num, wireBytes)
	p.b = binary.AppendUvarint(p.b, uint64(len(s)))
	p.b = append(p.b, s...)
	return p
}

func (p *enc) msg(num int, m *enc) *enc {
	p.key(num, wireBytes)
	p.b = binary.AppendUvarint(p.b, uint64(len(m.b)))
	p.b = append(p.b, m.b...)
	return p
}

func (p *enc) float(num int, f float32) *enc {
	if f == 0 {
		return p
	}
	p.key(num, wire32)
	p.b = binary.LittleEndian.AppendUint32(p.b, math.Float32bits(f))
	return p
}
