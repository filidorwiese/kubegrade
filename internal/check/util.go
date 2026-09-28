package check

import (
	"strconv"
	"strings"
	"time"
)

func fmtInt(i int) string { return strconv.Itoa(i) }

// expirySeverity maps days-until-expiry to the shared certificate thresholds.
func expirySeverity(until time.Duration) (Severity, bool) {
	switch {
	case until < 7*24*time.Hour:
		return Critical, true
	case until < 30*24*time.Hour:
		return High, true
	case until < 60*24*time.Hour:
		return Medium, true
	}
	return "", false
}

func expiryWhat(notAfter time.Time, now time.Time) string {
	d := days(notAfter.Sub(now))
	if d < 0 {
		return "expired " + notAfter.Format("2006-01-02") + " (" + fmtInt(-d) + " days ago)"
	}
	return "expires " + notAfter.Format("2006-01-02") + " (" + fmtInt(d) + " days)"
}

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
