package main

import (
	"fmt"
	"html"
	"html/template"
	"strconv"
	"strings"
)

// ansiToHTML turns a captured terminal session into HTML spans. It only
// knows the handful of SGR codes nodebench itself prints: reset, bold, dim,
// the eight basic colors and 24 bit colors. Carriage returns are handled
// like a terminal would, so spinner frames collapse into the final line,
// and every other escape sequence is dropped.
func ansiToHTML(raw string) template.HTML {
	var out strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if i := strings.LastIndexByte(line, '\r'); i >= 0 {
			line = line[i+1:]
		}
		if strings.TrimSpace(stripANSI(line)) == "" && out.Len() > 0 && strings.HasSuffix(out.String(), "\n\n") {
			continue
		}
		out.WriteString(sgrLine(line))
		out.WriteByte('\n')
	}
	return template.HTML(strings.TrimRight(out.String(), "\n"))
}

type sgr struct {
	bold, dim bool
	color     string
}

func (s sgr) class() string {
	var c []string
	if s.bold {
		c = append(c, "b")
	}
	if s.dim {
		c = append(c, "d")
	}
	return strings.Join(c, " ")
}

var basic = map[int]string{30: "#5c5b57", 31: "#f87171", 32: "#4ade80", 33: "#fbbf24", 34: "#3b82f6", 35: "#8b5cf6", 36: "#22d3ee", 37: "#f2f1ee"}

func sgrLine(line string) string {
	var b strings.Builder
	st := sgr{}
	open := false
	flush := func() {
		if open {
			b.WriteString("</span>")
			open = false
		}
		if st == (sgr{}) {
			return
		}
		b.WriteString("<span")
		if c := st.class(); c != "" {
			fmt.Fprintf(&b, ` class="%s"`, c)
		}
		if st.color != "" {
			fmt.Fprintf(&b, ` style="color:%s"`, st.color)
		}
		b.WriteString(">")
		open = true
	}
	for i := 0; i < len(line); {
		if line[i] != 0x1b {
			j := strings.IndexByte(line[i:], 0x1b)
			if j < 0 {
				j = len(line) - i
			}
			b.WriteString(html.EscapeString(line[i : i+j]))
			i += j
			continue
		}
		// ESC [ params final
		if i+1 >= len(line) || line[i+1] != '[' {
			i++
			continue
		}
		j := i + 2
		for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
			j++
		}
		if j >= len(line) {
			break
		}
		if line[j] == 'm' {
			applySGR(&st, line[i+2:j])
			flush()
		}
		i = j + 1
	}
	if open {
		b.WriteString("</span>")
	}
	return b.String()
}

func applySGR(st *sgr, params string) {
	if params == "" {
		*st = sgr{}
		return
	}
	p := strings.Split(params, ";")
	for k := 0; k < len(p); k++ {
		n, _ := strconv.Atoi(p[k])
		switch {
		case n == 0:
			*st = sgr{}
		case n == 1:
			st.bold = true
		case n == 2:
			st.dim = true
		case n == 22:
			st.bold, st.dim = false, false
		case n == 39:
			st.color = ""
		case n >= 30 && n <= 37:
			st.color = basic[n]
		case n >= 90 && n <= 97:
			st.color = basic[n-60]
		case n == 38 && k+4 < len(p) && p[k+1] == "2":
			r, _ := strconv.Atoi(p[k+2])
			g, _ := strconv.Atoi(p[k+3])
			bl, _ := strconv.Atoi(p[k+4])
			st.color = fmt.Sprintf("#%02x%02x%02x", r&255, g&255, bl&255)
			k += 4
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
