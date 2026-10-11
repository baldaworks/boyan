package projectionsync

import "strings"

const maxCanonicalReadRecords = 256

func isCanonicalID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\r\n\t")
}
