package main

import (
	"fmt"
	"html/template"
	"strings"
)

type resultView struct {
	R   *Result
	CSS template.CSS
	URL string

	CPU, Specs, Summary, Date, OneLiner    string
	MHz, Virt, RAM, Swap, DiskSize, Uptime string

	CPUMain              string
	Disk4k, Disk4kIOPS   string
	BestNet, BestNetFrom string
}

func (s *server) view(r *Result) resultView {
	sys := r.System
	v := resultView{
		R:        r,
		CSS:      s.css,
		URL:      *baseURL + "/r/" + r.ID,
		CPU:      shortCPU(sys.CPU),
		Date:     r.Created.Format("2006-01-02 15:04 UTC"),
		OneLiner: s.oneLiner(),
		MHz:      fmtMHz(sys.MHz),
		Virt:     virtName(sys.Virt),
		RAM:      fmtKiB(sys.RAM),
		Swap:     fmtKiB(sys.Swap),
		DiskSize: fmtKiB(sys.DiskKiB),
		Uptime:   fmtUptime(sys.Uptime),
	}
	v.Specs = specLine(r)

	var sum []string
	if c := r.CPU; c != nil {
		v.CPUMain = fmtBytes(c.SHA256N)
		sum = append(sum, "CPU sha256 "+v.CPUMain)
	}
	if t := disk4k(r); t != nil {
		v.Disk4k = fmtKBs(t.ReadKBs + t.WriteKBs)
		v.Disk4kIOPS = fmtIOPS(t.ReadIOPS + t.WriteIOPS)
		sum = append(sum, "disk 4k "+v.Disk4k)
	}
	if t := bestNet(r); t != nil {
		v.BestNet = fmtMbps(t.Recv)
		v.BestNetFrom = "from " + t.Location
		sum = append(sum, "download up to "+v.BestNet)
	}
	v.Summary = v.Specs + ". " + strings.Join(sum, ", ") + "."
	return v
}

func specLine(r *Result) string {
	sys := r.System
	parts := []string{fmt.Sprintf("%d cores", sys.Cores)}
	if sys.Cores == 1 {
		parts[0] = "1 core"
	}
	parts = append(parts, fmtKiB(sys.RAM)+" RAM")
	if sys.DiskKiB > 0 {
		parts = append(parts, fmtKiB(sys.DiskKiB)+" disk")
	}
	parts = append(parts, virtName(sys.Virt))
	if l := r.Location; l != nil && l.Org != "" {
		p := l.Org
		if l.Country != "" {
			p += ", " + l.Country
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " / ")
}

func disk4k(r *Result) *DiskTest {
	if r.Disk == nil || len(r.Disk.Tests) == 0 {
		return nil
	}
	for i := range r.Disk.Tests {
		if r.Disk.Tests[i].BS == "4k" {
			return &r.Disk.Tests[i]
		}
	}
	return &r.Disk.Tests[0]
}

func bestNet(r *Result) *NetTest {
	if r.Net == nil {
		return nil
	}
	var best *NetTest
	for i := range r.Net.Tests {
		t := &r.Net.Tests[i]
		if t.Recv > 0 && (best == nil || t.Recv > best.Recv) {
			best = t
		}
	}
	return best
}
