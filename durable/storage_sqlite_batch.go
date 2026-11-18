package durable

import (
	"bytes"
	"encoding/json"
	"math/bits"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	sqliteCompactFrameBytes = 8 * 1024
	sqliteFrameArrayBytes   = 512 * 1024
	sqliteFrameColumns      = 4
	sqliteFrameParameters   = 100
	sqliteFrameInsert       = "INSERT INTO frames(ordinal,kind,highwater,payload) "
	sqliteFrameValues       = "(?,?,?,?)"
	sqliteCompactInsert     = sqliteFrameInsert +
		"SELECT value ->> 0, value ->> 1, value ->> 2, CAST(value ->> 3 AS BLOB) FROM json_each(?) ORDER BY key"
)

func sqliteCompactFrame(frame storageFrame) bool {
	return frame.kind == 2 && len(frame.payload) <= sqliteCompactFrameBytes &&
		utf8.Valid(frame.payload) && bytes.IndexByte(frame.payload, 0) == -1
}

// insertFrames runs inside appendSeries' SQL transaction. Small frames reuse
// one SQL text with bounded JSON arrays; large frames bind their unmodified
// blobs directly. All insertions remain in ordinal order across chunks.
func (s *SQLiteStorage) insertFrames(frames []storageFrame) error {
	if len(frames) == 1 {
		return s.insertBoundFrames(frames)
	}
	var array, row []byte
	for len(frames) > 0 {
		if !sqliteCompactFrame(frames[0]) {
			n := 1
			for n < len(frames) && n < sqliteFrameParameters/sqliteFrameColumns && !sqliteCompactFrame(frames[n]) {
				n++
			}
			// Limit statement shapes while keeping each insert below the
			// parameter ceiling. A run of 19 becomes 16 + 2 + 1.
			n = 1 << (bits.Len(uint(n)) - 1)
			if err := s.insertBoundFrames(frames[:n]); err != nil {
				return err
			}
			frames = frames[n:]
			continue
		}
		array = append(array[:0], '[')
		for len(frames) > 0 && sqliteCompactFrame(frames[0]) {
			frame := frames[0]
			// Wrap the frame as a JSON string, not as parsed JSON. SQLite's
			// extraction restores the same UTF-8 bytes, including wire escapes.
			payload, err := json.Marshal(string(frame.payload))
			if err != nil {
				return err
			}
			row = append(row[:0], '[')
			row = strconv.AppendUint(row, frame.ordinal, 10)
			row = strconv.AppendUint(append(row, ','), uint64(frame.kind), 10)
			row = strconv.AppendUint(append(row, ','), frame.high, 10)
			row = append(append(append(row, ','), payload...), ']')
			if len(array)+len(row)+2 > sqliteFrameArrayBytes {
				break
			}
			if len(array) > 1 {
				array = append(array, ',')
			}
			array = append(array, row...)
			frames = frames[1:]
		}
		if err := s.exec(sqliteCompactInsert, string(append(array, ']'))); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStorage) insertBoundFrames(frames []storageFrame) error {
	if len(frames) == 1 {
		f := frames[0]
		return s.exec(sqliteFrameInsert+"VALUES"+sqliteFrameValues, int64(f.ordinal), int64(f.kind), int64(f.high), f.payload)
	}
	args := make([]any, 0, len(frames)*sqliteFrameColumns)
	for _, f := range frames {
		args = append(args, int64(f.ordinal), int64(f.kind), int64(f.high), f.payload)
	}
	return s.exec(sqliteFrameInsert+"VALUES"+sqliteFrameValues+strings.Repeat(","+sqliteFrameValues, len(frames)-1), args...)
}
