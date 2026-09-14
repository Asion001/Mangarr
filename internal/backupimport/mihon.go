package backupimport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Mihon backups are kotlinx.serialization protobuf messages (gzipped). The
// field numbers follow mihon's data/backup/models; unknown fields (forks
// use 600+, 800+ and 9000+) are skipped.

type wireType int

const (
	wireVarint wireType = 0
	wire64     wireType = 1
	wireBytes  wireType = 2
	wire32     wireType = 5
)

type field struct {
	num   int
	typ   wireType
	u     uint64 // varint / fixed values
	bytes []byte // length-delimited payload
}

var errTruncated = errors.New("truncated protobuf message")

func uvarint(b []byte) (uint64, int, error) {
	v, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, 0, errTruncated
	}
	return v, n, nil
}

// fields decodes one message level.
func fields(b []byte, fn func(f field) error) error {
	for len(b) > 0 {
		key, n, err := uvarint(b)
		if err != nil {
			return err
		}
		b = b[n:]
		f := field{num: int(key >> 3), typ: wireType(key & 7)}
		switch f.typ {
		case wireVarint:
			if f.u, n, err = uvarint(b); err != nil {
				return err
			}
			b = b[n:]
		case wire64:
			if len(b) < 8 {
				return errTruncated
			}
			f.u, b = binary.LittleEndian.Uint64(b), b[8:]
		case wire32:
			if len(b) < 4 {
				return errTruncated
			}
			f.u, b = uint64(binary.LittleEndian.Uint32(b)), b[4:]
		case wireBytes:
			l, n, err := uvarint(b)
			if err != nil {
				return err
			}
			b = b[n:]
			if uint64(len(b)) < l {
				return errTruncated
			}
			f.bytes, b = b[:l], b[l:]
		default:
			return fmt.Errorf("unsupported protobuf wire type %d (field %d)", f.typ, f.num)
		}
		if err := fn(f); err != nil {
			return err
		}
	}
	return nil
}

func (f field) str() string { return string(f.bytes) }
func (f field) bool() bool  { return f.u != 0 }
func (f field) int() int64  { return int64(f.u) }

func (f field) float32() float64 {
	if f.typ == wire32 {
		return float64(math.Float32frombits(uint32(f.u)))
	}
	return float64(f.u)
}

// ints reads a repeated integer field (packed or not).
func (f field) ints() ([]int64, error) {
	if f.typ != wireBytes {
		return []int64{int64(f.u)}, nil
	}
	var out []int64
	b := f.bytes
	for len(b) > 0 {
		v, n, err := uvarint(b)
		if err != nil {
			return nil, err
		}
		out = append(out, int64(v))
		b = b[n:]
	}
	return out, nil
}

func millis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// mihonStatus maps SManga status codes.
func mihonStatus(n int64) string {
	switch n {
	case 1, 4: // ongoing, publishing finished (scanlation continues)
		return "ongoing"
	case 2:
		return "completed"
	case 5:
		return "cancelled"
	case 6:
		return "hiatus"
	}
	return "unknown"
}

// Mihon tracker sync ids.
var mihonTrackers = map[int64]string{1: TrackerMAL, 2: TrackerAniList, 3: TrackerKitsu, 7: TrackerMangaUpdates}

type mihonCategory struct {
	name  string
	order int64
}

type mihonHistory struct {
	url      string
	lastRead int64
}

func parseMihon(data []byte) (*Backup, error) {
	b := &Backup{Format: FormatMihon, Sources: map[string]string{}, Categories: []string{}, Entries: []Entry{}}
	var rawManga [][]byte
	var cats []mihonCategory
	err := fields(data, func(f field) error {
		switch {
		case f.num == 1 && f.typ == wireBytes:
			rawManga = append(rawManga, f.bytes)
		case f.num == 2 && f.typ == wireBytes:
			var c mihonCategory
			if err := fields(f.bytes, func(g field) error {
				switch g.num {
				case 1:
					c.name = g.str()
				case 2:
					c.order = g.int()
				}
				return nil
			}); err != nil {
				return err
			}
			cats = append(cats, c)
		case f.num == 101 && f.typ == wireBytes:
			var name string
			var id int64
			if err := fields(f.bytes, func(g field) error {
				switch g.num {
				case 1:
					name = g.str()
				case 2:
					id = g.int()
				}
				return nil
			}); err != nil {
				return err
			}
			b.Sources[strconv.FormatInt(id, 10)] = name
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("mihon backup: %w", err)
	}
	sort.SliceStable(cats, func(i, j int) bool { return cats[i].order < cats[j].order })
	catByOrder := map[int64]string{}
	for _, c := range cats {
		b.Categories = append(b.Categories, c.name)
		catByOrder[c.order] = c.name
	}
	for i, raw := range rawManga {
		e, err := parseMihonManga(raw, catByOrder)
		if err != nil {
			return nil, fmt.Errorf("mihon backup: manga %d: %w", i+1, err)
		}
		e.SourceName = b.Sources[e.SourceID]
		b.Entries = append(b.Entries, e)
	}
	if len(b.Entries) == 0 {
		return nil, errors.New("the backup has no manga")
	}
	return b, nil
}

func parseMihonManga(raw []byte, catByOrder map[int64]string) (Entry, error) {
	e := Entry{Favorite: true, Status: "unknown"} // favorite defaults to true and is omitted then
	var history []mihonHistory
	err := fields(raw, func(f field) error {
		switch f.num {
		case 1:
			e.SourceID = strconv.FormatInt(f.int(), 10)
		case 2:
			e.URL = f.str()
		case 3:
			e.Title = f.str()
		case 4:
			e.Artist = f.str()
		case 5:
			e.Author = f.str()
		case 6:
			e.Description = f.str()
		case 7:
			e.Genres = append(e.Genres, f.str())
		case 8:
			e.Status = mihonStatus(f.int())
		case 9:
			e.ThumbnailURL = f.str()
		case 13:
			e.AddedAt = millis(f.int())
		case 16:
			c, err := parseMihonChapter(f.bytes)
			if err != nil {
				return err
			}
			e.Chapters = append(e.Chapters, c)
		case 17:
			orders, err := f.ints()
			if err != nil {
				return err
			}
			for _, o := range orders {
				if name, ok := catByOrder[o]; ok {
					e.Categories = append(e.Categories, name)
				}
			}
		case 18:
			tracker, id, err := parseMihonTracking(f.bytes)
			if err != nil {
				return err
			}
			if tracker != "" && id != "" {
				if e.Trackers == nil {
					e.Trackers = map[string]string{}
				}
				e.Trackers[tracker] = id
			}
		case 100:
			e.Favorite = f.bool()
		case 104:
			var h mihonHistory
			if err := fields(f.bytes, func(g field) error {
				switch g.num {
				case 1:
					h.url = g.str()
				case 2:
					h.lastRead = g.int()
				}
				return nil
			}); err != nil {
				return err
			}
			history = append(history, h)
		case 108:
			e.ExcludedScanlators = append(e.ExcludedScanlators, f.str())
		}
		return nil
	})
	if err != nil {
		return e, err
	}
	if e.URL == "" {
		return e, errors.New("missing url")
	}
	if len(history) > 0 {
		byURL := map[string]int64{}
		for _, h := range history {
			byURL[h.url] = max(byURL[h.url], h.lastRead)
		}
		for i := range e.Chapters {
			if ms, ok := byURL[e.Chapters[i].URL]; ok && ms > 0 {
				t := millis(ms)
				e.Chapters[i].ReadAt = &t
			}
		}
	}
	return e, nil
}

func parseMihonChapter(raw []byte) (Chapter, error) {
	c := Chapter{Number: -1}
	hasNumber := false
	err := fields(raw, func(f field) error {
		switch f.num {
		case 1:
			c.URL = f.str()
		case 2:
			c.Name = f.str()
		case 3:
			c.Scanlator = f.str()
		case 4:
			c.Read = f.bool()
		case 6:
			c.LastPageRead = int(f.int())
		case 9:
			c.Number, hasNumber = f.float32(), true
		}
		return nil
	})
	if !hasNumber {
		c.Number = 0 // kotlinx omits the default 0f
	}
	return c, err
}

func parseMihonTracking(raw []byte) (string, string, error) {
	var syncID, mediaID, mediaIDInt int64
	var trackingURL string
	err := fields(raw, func(f field) error {
		switch f.num {
		case 1:
			syncID = f.int()
		case 3:
			mediaIDInt = f.int()
		case 4:
			trackingURL = f.str()
		case 100:
			mediaID = f.int()
		}
		return nil
	})
	tracker := mihonTrackers[syncID]
	if mediaID == 0 {
		mediaID = mediaIDInt
	}
	id := ""
	if mediaID > 0 {
		id = strconv.FormatInt(mediaID, 10)
	} else if tracker == TrackerMangaUpdates && trackingURL != "" {
		// MangaUpdates ids are base36 slugs in the url
		parts := strings.Split(strings.TrimRight(trackingURL, "/"), "/")
		id = parts[len(parts)-1]
	}
	return tracker, id, err
}
