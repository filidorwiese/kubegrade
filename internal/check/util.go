package check

import (
	"strconv"
	"strings"
)

func fmtInt(i int) string { return strconv.Itoa(i) }

// minor extracts "1.34" from "v1.34.4+k3s1".
func minor(version string) string {
	v := strings.TrimPrefix(version, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return v
	}
	return parts[0] + "." + parts[1]
}

func minorInt(m string) int {
	parts := strings.SplitN(m, ".", 2)
	if len(parts) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(parts[1])
	return n
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmtInt(n) + " " + word + "s"
}
