package files

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Entry is one line of a directory listing.
type Entry struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // file, dir, link, other
	Mode     string `json:"mode"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Target   string `json:"target,omitempty"`
}

// Stat is what the editor remembers about a file to detect changes.
type Stat struct {
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

// lsLine matches GNU and BusyBox `ls -l`: mode, links, owner, group, size (or "major, minor"), date, name.
var lsLine = regexp.MustCompile(`^([-dlbcpsDn])([rwxsStTl-]{9})[.+@]?\s+\d+\s+\S+\s+\S+\s+(?:(\d+),\s*\d+|(\d+))\s+([A-Z][a-z]{2})\s+(\d{1,2})\s+(\d{1,2}:\d{2}|\d{4})\s(.*)$`)

var months = map[string]time.Month{
	"Jan": time.January, "Feb": time.February, "Mar": time.March, "Apr": time.April, "May": time.May, "Jun": time.June,
	"Jul": time.July, "Aug": time.August, "Sep": time.September, "Oct": time.October, "Nov": time.November, "Dec": time.December,
}

// parseLine reads one ls line; ok is false for lines that are not entries.
func parseLine(line string, now time.Time) (e Entry, ok bool) {
	m := lsLine.FindStringSubmatch(line)
	if m == nil {
		return Entry{}, false
	}
	e.Mode = m[1] + m[2]
	switch m[1] {
	case "-":
		e.Type = "file"
	case "d":
		e.Type = "dir"
	case "l":
		e.Type = "link"
	default:
		e.Type = "other"
	}
	sizeText := m[4]
	if m[3] != "" {
		sizeText = "0" // a device file has no size
	}
	e.Size, _ = strconv.ParseInt(sizeText, 10, 64)
	e.Modified = parseTime(m[5], m[6], m[7], now)
	name := m[8]
	if e.Type == "link" {
		if i := strings.Index(name, " -> "); i >= 0 {
			name, e.Target = name[:i], name[i+4:]
		}
	}
	e.Name = name
	return e, true
}

// parseTime turns `Jan  2 15:04` or `Jan  2  2006` into RFC 3339 (UTC). Without a year, the latest such
// date that is not in the future is meant.
func parseTime(mon, day, yearOrTime string, now time.Time) string {
	d, _ := strconv.Atoi(day)
	if !strings.Contains(yearOrTime, ":") {
		y, _ := strconv.Atoi(yearOrTime)
		return time.Date(y, months[mon], d, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	hm := strings.SplitN(yearOrTime, ":", 2)
	h, _ := strconv.Atoi(hm[0])
	mi, _ := strconv.Atoi(hm[1])
	t := time.Date(now.Year(), months[mon], d, h, mi, 0, 0, time.UTC)
	if t.After(now.Add(24 * time.Hour)) {
		t = t.AddDate(-1, 0, 0)
	}
	return t.Format(time.RFC3339)
}

// parseListing reads the output of `ls -lA`. The `total` line is ignored; lines that cannot be read are counted.
func parseListing(out string, now time.Time) (entries []Entry, skipped int) {
	entries = []Entry{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, "total ") {
			continue
		}
		e, ok := parseLine(strings.TrimRight(line, "\r"), now)
		if !ok {
			skipped++
			continue
		}
		entries = append(entries, e)
	}
	return entries, skipped
}
