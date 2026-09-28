// Package grade turns findings into category scores, letters and the
// overall capped grade.
package grade

import (
	"sort"
	"strconv"

	"github.com/filidorwiese/kubegrade/internal/check"
)

var points = map[check.Severity]int{
	check.Info:     0,
	check.Low:      3,
	check.Medium:   8,
	check.High:     15,
	check.Critical: 30,
}

func Points(f check.Finding) int {
	n := f.Count
	if n < 1 {
		n = 1
	}
	return points[f.Severity] * n
}

// letters ordered best to worst; index is used for the cap rule.
var letters = []struct {
	Letter string
	Min    int
	Max    int
}{
	{"A+", 95, 100}, {"A", 85, 94}, {"B", 70, 84}, {"C", 55, 69}, {"D", 40, 54}, {"F", 0, 39},
}

func Letter(score int) string {
	for _, l := range letters {
		if score >= l.Min {
			return l.Letter
		}
	}
	return "F"
}

func letterIndex(letter string) int {
	for i, l := range letters {
		if l.Letter == letter {
			return i
		}
	}
	return len(letters) - 1
}

type Category struct {
	ID      string
	Name    string
	Grade   string
	Score   int
	Summary string
}

type Result struct {
	Grade      string
	Score      int
	CappedBy   string
	Categories []Category
}

func Compute(findings []check.Finding) Result {
	scores := map[string]int{}
	for _, c := range check.Categories {
		scores[c] = 100
	}
	for _, f := range findings {
		scores[f.Category] -= Points(f)
		if scores[f.Category] < 0 {
			scores[f.Category] = 0
		}
	}

	var r Result
	total, worst := 0, 0
	for _, id := range check.Categories {
		c := Category{ID: id, Name: check.CategoryNames[id], Score: scores[id], Summary: summary(findings, id)}
		c.Grade = Letter(c.Score)
		r.Categories = append(r.Categories, c)
		total += c.Score
		if li := letterIndex(c.Grade); li > worst {
			worst = li
			r.CappedBy = id
		}
	}
	r.Score = total / len(check.Categories)
	r.Grade = Letter(r.Score)

	// Cap: never more than one letter above the worst category.
	allowed := worst - 1
	if allowed < 0 {
		allowed = 0
	}
	if letterIndex(r.Grade) < allowed {
		r.Grade = letters[allowed].Letter
		r.Score = letters[allowed].Max
	} else {
		r.CappedBy = ""
	}
	return r
}

// summary is the "what" of the costliest finding plus a count of the rest.
func summary(findings []check.Finding, category string) string {
	var fs []check.Finding
	for _, f := range findings {
		if f.Category == category && f.Severity != check.Info {
			fs = append(fs, f)
		}
	}
	if len(fs) == 0 {
		return ""
	}
	sort.SliceStable(fs, func(i, j int) bool { return Points(fs[i]) > Points(fs[j]) })
	s := fs[0].Resource + ": " + fs[0].What
	if n := len(fs) - 1; n > 0 {
		s += ", +" + strconv.Itoa(n) + " more"
	}
	return s
}
