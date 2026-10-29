package backup

import (
	"regexp"
	"sort"
	"strings"
)

var runPathRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})/(\d{8}T\d{6}Z)/(.+)$`)

type runObjects struct {
	date, id string
	keys     []string
	complete bool
}

// PruneKeys returns the keys retention deletes, given every key under prefix.
//
// The rules:
//   - Only keys shaped <prefix><YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>/... are ever
//     considered. Anything else under the prefix is left alone.
//   - A run is complete when it has manifest.json; a date is complete when
//     it has at least one complete run.
//   - The newest keepDays complete dates are kept. Every date older than the
//     oldest kept one is deleted, complete or not. With fewer than keepDays
//     complete dates, no date is deleted.
//   - Within a kept date, runs older than its newest complete run are deleted.
//
// Call it only after a successful run: a failed run must never prune.
func PruneKeys(prefix string, keys []string, keepDays int) []string {
	if keepDays < 1 {
		return nil
	}
	byDate := groupRuns(prefix, keys)
	kept, cutoff := keptDates(byDate, keepDays)

	var del []string
	for d, rs := range byDate {
		switch {
		case cutoff != "" && d < cutoff:
			for _, r := range rs {
				del = append(del, r.keys...)
			}
		case kept[d]:
			del = append(del, supersededKeys(rs)...)
		}
	}
	sort.Strings(del)
	return del
}

// groupRuns collects the run-shaped keys under prefix by date.
func groupRuns(prefix string, keys []string) map[string][]*runObjects {
	runs := map[string]*runObjects{}
	for _, k := range keys {
		rest, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		m := runPathRe.FindStringSubmatch(rest)
		if m == nil {
			continue
		}
		id := m[1] + "/" + m[2]
		r := runs[id]
		if r == nil {
			r = &runObjects{date: m[1], id: m[2]}
			runs[id] = r
		}
		r.keys = append(r.keys, k)
		if m[3] == ManifestName {
			r.complete = true
		}
	}
	byDate := map[string][]*runObjects{}
	for _, r := range runs {
		byDate[r.date] = append(byDate[r.date], r)
	}
	return byDate
}

// keptDates returns the newest keepDays complete dates and the cutoff below
// which every date is deleted. The cutoff is empty while fewer than keepDays
// complete dates exist.
func keptDates(byDate map[string][]*runObjects, keepDays int) (kept map[string]bool, cutoff string) {
	var complete []string
	for d, rs := range byDate {
		if newestComplete(rs) != "" {
			complete = append(complete, d)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(complete)))
	kept = map[string]bool{}
	for i, d := range complete {
		if i < keepDays {
			kept[d] = true
		}
	}
	if len(complete) < keepDays {
		return kept, ""
	}
	return kept, complete[keepDays-1]
}

// newestComplete is the id of the newest run with a manifest, or "".
func newestComplete(rs []*runObjects) string {
	newest := ""
	for _, r := range rs {
		if r.complete && r.id > newest {
			newest = r.id
		}
	}
	return newest
}

// supersededKeys are the keys of runs older than the date's newest complete run.
func supersededKeys(rs []*runObjects) []string {
	newest := newestComplete(rs)
	var out []string
	for _, r := range rs {
		if r.id < newest {
			out = append(out, r.keys...)
		}
	}
	return out
}
