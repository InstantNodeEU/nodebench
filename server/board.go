package main

import (
	"bufio"
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The leaderboard lives in memory. It's rebuilt from the json files on
// start and updated on upload, which is plenty for a few hundred thousand
// results and saves running a database next to a flat file store.

type metric struct {
	Key, Label, Short string
	value             func(*entry) float64
	format            func(float64) string
}

var metrics = []metric{
	{"cpu", "CPU, all cores", "sha256, all threads", func(e *entry) float64 { return e.cpuMulti }, fmtBytes},
	{"single", "CPU, one core", "sha256, 1 thread", func(e *entry) float64 { return e.cpuSingle }, fmtBytes},
	{"iops", "Disk 4k IOPS", "random 4k, read+write", func(e *entry) float64 { return e.iops4k }, fmtIOPS},
	{"seq", "Disk 1m throughput", "random 1m, read+write", func(e *entry) float64 { return e.kbs1m }, fmtKBs},
	{"net", "Network", "best iperf3 download", func(e *entry) float64 { return e.netBest }, fmtMbps},
}

func metricByKey(k string) metric {
	for _, m := range metrics {
		if m.Key == k {
			return m
		}
	}
	return metrics[0]
}

type entry struct {
	ID      string
	Created time.Time
	Name    string
	Org     string
	ASN     string
	Country string
	Virt    string
	CPU     string
	Cores   int

	cpuMulti, cpuSingle, iops4k, kbs1m, netBest float64
}

// key groups repeated runs of the same box so one host can't fill the board.
func (e *entry) key() string {
	return strings.ToLower(e.Name + "|" + e.CPU + "|" + e.ASN)
}

func newEntry(r *Result) *entry {
	e := &entry{ID: r.ID, Created: r.Created, Name: r.Name, Virt: virtName(r.System.Virt), CPU: shortCPU(r.System.CPU), Cores: r.System.Cores}
	if l := r.Location; l != nil {
		e.Org, e.ASN, e.Country = l.Org, l.ASN, l.Country
	}
	if e.Name == "" {
		e.Name = e.Org
	}
	if e.Name == "" {
		e.Name = "anonymous"
	}
	if c := r.CPU; c != nil {
		e.cpuMulti, e.cpuSingle = c.SHA256N, c.SHA256
	}
	if r.Disk != nil {
		for _, t := range r.Disk.Tests {
			switch t.BS {
			case "4k":
				e.iops4k = t.ReadIOPS + t.WriteIOPS
			case "1m":
				e.kbs1m = t.ReadKBs + t.WriteKBs
			}
		}
	}
	if t := bestNet(r); t != nil {
		e.netBest = t.Recv
	}
	return e
}

type board struct {
	mu      sync.RWMutex
	entries map[string]*entry
	hidden  map[string]bool
	file    string
}

func newBoard(dir string) *board {
	return &board{entries: map[string]*entry{}, hidden: map[string]bool{}, file: filepath.Join(dir, "hidden.txt")}
}

// load reads the hidden list and every stored result. Broken files are
// skipped, one bad result shouldn't keep the server from starting.
func (b *board) load(s *Store) error {
	if f, err := os.Open(b.file); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if id := strings.TrimSpace(sc.Text()); validID(id) {
				b.hidden[id] = true
			}
		}
		f.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return filepath.WalkDir(s.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		r, err := s.Load(strings.TrimSuffix(d.Name(), ".json"))
		if err == nil {
			b.add(r)
		}
		return nil
	})
}

func (b *board) add(r *Result) {
	// quick runs use shorter tests and a small disk file, they don't compare
	if !r.Leaderboard || r.Quick || !r.plausible() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.hidden[r.ID] {
		b.entries[r.ID] = newEntry(r)
	}
}

func (b *board) hide(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hidden[id] {
		return nil
	}
	f, err := os.OpenFile(b.file, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(id + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	b.hidden[id] = true
	delete(b.entries, id)
	return nil
}

type filter struct {
	Metric  metric
	Virt    string
	Country string
	Limit   int
}

// top returns the best entry per host for the metric, highest first.
func (b *board) top(f filter) []*entry {
	b.mu.RLock()
	var list []*entry
	for _, e := range b.entries {
		if f.Metric.value(e) <= 0 {
			continue
		}
		if f.Virt != "" && !strings.EqualFold(e.Virt, f.Virt) {
			continue
		}
		if f.Country != "" && !strings.EqualFold(e.Country, f.Country) {
			continue
		}
		list = append(list, e)
	}
	b.mu.RUnlock()

	slices.SortFunc(list, func(x, y *entry) int {
		if c := cmp.Compare(f.Metric.value(y), f.Metric.value(x)); c != 0 {
			return c
		}
		return x.Created.Compare(y.Created)
	})
	seen := map[string]bool{}
	out := list[:0]
	for _, e := range list {
		if seen[e.key()] {
			continue
		}
		seen[e.key()] = true
		out = append(out, e)
		if f.Limit > 0 && len(out) == f.Limit {
			break
		}
	}
	return out
}

// facets lists the virt types and countries present, for the filter menus.
func (b *board) facets() (virts, countries []string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	v, c := map[string]bool{}, map[string]bool{}
	for _, e := range b.entries {
		v[e.Virt] = true
		if e.Country != "" {
			c[e.Country] = true
		}
	}
	for k := range v {
		virts = append(virts, k)
	}
	for k := range c {
		countries = append(countries, k)
	}
	slices.Sort(virts)
	slices.Sort(countries)
	return
}

func (b *board) size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.entries)
}

// recent returns the newest entries on the board. Only results that were
// put on the leaderboard show up anywhere in a list, everything else stays
// reachable by its link alone.
func (b *board) recent(n int) []*entry {
	b.mu.RLock()
	list := make([]*entry, 0, len(b.entries))
	for _, e := range b.entries {
		list = append(list, e)
	}
	b.mu.RUnlock()
	slices.SortFunc(list, func(x, y *entry) int { return y.Created.Compare(x.Created) })
	if len(list) > n {
		list = list[:n]
	}
	return list
}

// formatted values for the templates
func (e *entry) SHA() string  { return fmtOr(e.cpuMulti, fmtBytes) }
func (e *entry) IOPS() string { return fmtOr(e.iops4k, fmtIOPS) }
func (e *entry) Down() string { return fmtOr(e.netBest, fmtMbps) }
func (e *entry) Date() string { return e.Created.Format("2006-01-02") }

func fmtOr(v float64, f func(float64) string) string {
	if v <= 0 {
		return "-"
	}
	return f(v)
}
