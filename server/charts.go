package main

import (
	"fmt"
	"html/template"
	"strings"
)

// The charts are plain inline SVG built on the server. Bars carry their
// value in a <title> so hovering shows it; labels and numbers sit in the
// HTML around them, which keeps them readable when the bar shrinks on
// a phone.

func bar(frac float64, class, title string) template.HTML {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	w := frac * 100
	if frac > 0 && w < 1 {
		w = 1
	}
	return template.HTML(fmt.Sprintf(
		`<svg class="bar %s" viewBox="0 0 100 8" preserveAspectRatio="none" role="img" aria-label="%s"><title>%s</title><rect class="track" width="100" height="8" rx="0"/><rect class="fill" width="%.2f" height="8"/></svg>`,
		template.HTMLEscapeString(class), template.HTMLEscapeString(title), template.HTMLEscapeString(title), w))
}

type cpuRow struct {
	Name           string
	Single, Multi  string
	SFrac, MFrac   float64
	Scale          string
	SingleT, MultT string
}

type cpuChart struct {
	Threads    int
	Rows       []cpuRow
	Steal      string
	StealClass string
}

func newCPUChart(c *CPU) *cpuChart {
	if c == nil {
		return nil
	}
	top := max(c.SHA256, c.SHA256N, c.AES, c.AESN)
	if top <= 0 {
		return nil
	}
	ch := &cpuChart{Threads: c.Threads}
	if c.Threads > 1 && c.Steal >= 0 {
		ch.Steal = fmt.Sprintf("%.1f%%", c.Steal)
		switch {
		case c.Steal < 1:
			ch.StealClass = "near"
		case c.Steal < 5:
			ch.StealClass = "mid"
		default:
			ch.StealClass = "far"
		}
	}
	for _, t := range []struct {
		name string
		s, m float64
	}{{"sha256", c.SHA256, c.SHA256N}, {"aes-256-gcm", c.AES, c.AESN}} {
		r := cpuRow{Name: t.name, Single: fmtBytes(t.s), Multi: fmtBytes(t.m), SFrac: t.s / top, MFrac: t.m / top}
		if t.s > 0 && t.m > 0 {
			r.Scale = fmt.Sprintf("%.1fx", t.m/t.s)
		}
		r.SingleT = t.name + ", 1 thread: " + r.Single
		r.MultT = fmt.Sprintf("%s, %d threads: %s", t.name, c.Threads, r.Multi)
		ch.Rows = append(ch.Rows, r)
	}
	return ch
}

type diskRow struct {
	BS                 string
	Read, Write, Total string
	RFrac, WFrac       float64
	IOPS               string
	ReadT, WriteT      string
}

type diskChart struct {
	Tool, Size string
	Device     string
	Rows       []diskRow
}

func newDiskChart(d *Disk) *diskChart {
	if d == nil || len(d.Tests) == 0 {
		return nil
	}
	var top float64
	for _, t := range d.Tests {
		top = max(top, t.ReadKBs, t.WriteKBs)
	}
	if top <= 0 {
		return nil
	}
	ch := &diskChart{Tool: d.Tool, Size: d.Size}
	media := map[string]string{"nvme": "NVMe", "ssd": "SSD", "hdd": "HDD", "virtual": "virtual disk"}[d.Media]
	switch {
	case media != "" && d.FS != "":
		ch.Device = media + ", " + d.FS
	case media != "":
		ch.Device = media
	case d.FS != "":
		ch.Device = d.FS
	}
	for _, t := range d.Tests {
		r := diskRow{
			BS: t.BS, Read: fmtKBs(t.ReadKBs), Write: fmtKBs(t.WriteKBs), Total: fmtKBs(t.ReadKBs + t.WriteKBs),
			RFrac: t.ReadKBs / top, WFrac: t.WriteKBs / top, IOPS: fmtIOPS(t.ReadIOPS + t.WriteIOPS),
		}
		r.ReadT = fmt.Sprintf("%s read: %s, %s IOPS", t.BS, r.Read, fmtIOPS(t.ReadIOPS))
		r.WriteT = fmt.Sprintf("%s write: %s, %s IOPS", t.BS, r.Write, fmtIOPS(t.WriteIOPS))
		ch.Rows = append(ch.Rows, r)
	}
	return ch
}

type netRow struct {
	Location, Provider string
	V6, Ours           bool
	Send, Recv         string
	SFrac, RFrac       float64
	SendFailed         bool
	RecvFailed         bool
	Ping, PingClass    string
	Loss               string
	SendT, RecvT       string
}

type netGroup struct {
	Label string
	Rows  []netRow
}

type netChart struct {
	Tool   string
	Groups []netGroup
	Map    template.HTML
	Origin bool
}

func pingClass(ms float64) string {
	switch {
	case ms <= 0:
		return "none"
	case ms < 50:
		return "near"
	case ms < 150:
		return "mid"
	default:
		return "far"
	}
}

func newNetChart(r *Result) *netChart {
	n := r.Net
	if n == nil || len(n.Tests) == 0 {
		return nil
	}
	var top float64
	for _, t := range n.Tests {
		top = max(top, t.Send, t.Recv)
	}
	if top <= 0 {
		top = 1
	}
	ch := &netChart{Tool: n.Tool}
	for _, t := range n.Tests {
		row := netRow{
			Location: t.Location, Provider: t.Provider, V6: t.Proto == 6,
			Send: fmtMbps(t.Send), Recv: fmtMbps(t.Recv),
			SFrac: t.Send / top, RFrac: t.Recv / top,
			SendFailed: t.Send <= 0, RecvFailed: t.Recv <= 0,
			Ping: fmtPing(t.Ping), PingClass: pingClass(t.Ping),
		}
		if t.Loss > 0 {
			row.Loss = fmt.Sprintf("%g%% loss", t.Loss)
		}
		if s := siteFor(t.Location); s != nil {
			row.Ours = s.Ours
		}
		row.SendT = t.Location + " upload: " + row.Send
		row.RecvT = t.Location + " download: " + row.Recv
		label := "IPv4"
		if row.V6 {
			label = "IPv6"
		}
		if len(ch.Groups) == 0 || ch.Groups[len(ch.Groups)-1].Label != label {
			ch.Groups = append(ch.Groups, netGroup{Label: label})
		}
		g := &ch.Groups[len(ch.Groups)-1]
		g.Rows = append(g.Rows, row)
	}
	if len(ch.Groups) == 1 {
		ch.Groups[0].Label = ""
	}
	ch.Map = netMap(r)
	if r.Location != nil {
		_, ch.Origin = countryPos[strings.ToUpper(r.Location.Country)]
	}
	return ch
}

// netMap draws the test sites on the dotted world map. With a result it
// also draws a line from the tester's country to every site, coloured by
// ping. r may be nil for the plain locations map.
func netMap(r *Result) template.HTML {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="map" viewBox="0 0 %.0f %.0f" role="img" aria-label="Map of the test locations">`, mapW, mapH)
	fmt.Fprintf(&b, `<image class="coarse" href="/static/world.svg" width="%.0f" height="%.0f"/>`, mapW, mapH)
	fmt.Fprintf(&b, `<image class="fine" data-href="/static/world-fine.svg" width="%.0f" height="%.0f"/>`, mapW, mapH)

	ping := map[string]float64{}
	if r != nil && r.Net != nil {
		for _, t := range r.Net.Tests {
			if t.Proto == 4 || ping[t.Location] == 0 {
				ping[t.Location] = t.Ping
			}
		}
	}

	if r != nil && r.Location != nil {
		if pos, ok := countryPos[strings.ToUpper(r.Location.Country)]; ok {
			ox, oy := project(pos[0], pos[1])
			b.WriteString(`<g class="arcs">`)
			for _, s := range sites {
				p, ok := ping[s.Location]
				if !ok {
					continue
				}
				x, y := project(s.Lon, s.Lat)
				fmt.Fprintf(&b, `<path class="arc %s" d="%s"/>`, pingClass(p), arc(ox, oy, x, y))
			}
			fmt.Fprintf(&b, `</g><g class="origin"><circle cx="%.1f" cy="%.1f" r="9"/><circle cx="%.1f" cy="%.1f" r="3.5"/><title>This server, %s</title></g>`,
				ox, oy, ox, oy, template.HTMLEscapeString(r.Location.Country))
		}
	}

	b.WriteString(siteLabels(r == nil, func(loc string) bool { _, ok := ping[loc]; return r == nil || ok }))
	for _, s := range sites {
		x, y := project(s.Lon, s.Lat)
		cls := "site"
		title := s.Location + ", " + s.Provider
		if p, ok := ping[s.Location]; ok {
			cls += " " + pingClass(p)
			title += ", " + fmtPing(p)
		} else if r != nil {
			continue
		}
		if s.Extended {
			cls += " ext"
		}
		if s.Ours {
			cls += " ours"
			fmt.Fprintf(&b, `<circle class="halo" cx="%.1f" cy="%.1f" r="11"/>`, x, y)
		}
		fmt.Fprintf(&b, `<circle class="%s" cx="%.1f" cy="%.1f" r="4.5"><title>%s</title></circle>`, cls, x, y, template.HTMLEscapeString(title))
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

// siteLabels names the sites. The European ones sit too close together at
// world scale, so they share a callout there and only get their own labels
// once the map is zoomed in (the "zoom" ones, shown by the map script).
// Label positions are stored as data so the script can keep them next to
// their dot at any zoom level.
func siteLabels(overview bool, shown func(string) bool) string {
	var b strings.Builder
	b.WriteString(`<g class="labels">`)
	for _, s := range sites {
		if !shown(s.Location) {
			continue
		}
		x, y := project(s.Lon, s.Lat)
		name, _, _ := strings.Cut(s.Location, ",")
		anchor, dx := "start", 10.0
		// west coast labels go left so they don't run into Dallas
		if s.Lon > 60 || s.Lon < -110 {
			anchor, dx = "end", -10
		}
		cls := "zoom"
		if overview && !europe(s) && !s.Extended {
			cls = "always"
		}
		if s.Ours {
			cls += " ours"
		}
		fmt.Fprintf(&b, `<text class="%s" x="%.1f" y="%.1f" data-sx="%.1f" data-sy="%.1f" data-dx="%.0f" text-anchor="%s">%s</text>`,
			cls, x+dx, y+4, x, y, dx, anchor, template.HTMLEscapeString(name))
	}
	if overview {
		n := 0
		for _, s := range sites {
			if europe(s) {
				n++
			}
		}
		x, y := project(4, 52)
		fmt.Fprintf(&b, `<g class="callout"><path class="lead" d="M%.1f %.1fL%.1f %.1fH%.1f"/><text x="%.1f" y="%.1f">Europe, %d sites</text><text class="ours" x="%.1f" y="%.1f">incl. InstantNode NL</text></g>`,
			x, y-8, x+18, y-38, x+30, x+34, y-41, n, x+34, y-27)
	}
	b.WriteString(`</g>`)
	return b.String()
}

func europe(s site) bool { return s.Lon > -30 && s.Lon < 30 && s.Lat > 35 }
