package nativeprocess

import (
	"errors"
	"strconv"
	"strings"
)

var errProcRecord = errors.New("invalid or incomplete proc process record")

// parseProcStart reads field 22 without splitting the parenthesized comm field:
// comm can contain whitespace, parentheses and newlines. Start ticks are opaque
// kernel clock ticks since boot, not wall-clock seconds.
func parseProcStart(data []byte, pid int) (uint64, error) {
	s := string(data)
	prefix := strconv.Itoa(pid) + " ("
	end := strings.LastIndex(s, ") ")
	if pid <= 0 || !strings.HasPrefix(s, prefix) || end < len(prefix) {
		return 0, errProcRecord
	}
	fields := strings.Fields(s[end+2:])
	if len(fields) < 20 || len(fields[0]) != 1 {
		return 0, errProcRecord
	}
	ticks, e := strconv.ParseUint(fields[19], 10, 64)
	if e != nil || ticks == 0 {
		return 0, errProcRecord
	}
	return ticks, nil
}

// Effective UID is the second of the four kernel status UID values.
func parseProcUID(data []byte) (uint32, error) {
	var uid uint64
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		if found {
			return 0, errProcRecord
		}
		found = true
		fields := strings.Fields(strings.TrimPrefix(line, "Uid:"))
		if len(fields) != 4 {
			return 0, errProcRecord
		}
		for i, field := range fields {
			value, e := strconv.ParseUint(field, 10, 32)
			if e != nil {
				return 0, errProcRecord
			}
			if i == 1 {
				uid = value
			}
		}
	}
	if !found {
		return 0, errProcRecord
	}
	return uint32(uid), nil
}
