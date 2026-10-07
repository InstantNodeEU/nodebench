package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
)

// page is what every template gets. Data holds the page specific part.
type page struct {
	Name     string
	Title    string
	Desc     string
	URL      string
	OG       string
	OGW, OGH int
	Base     string
	OneLiner string
	CSSVer   string
	Data     any
}

var pageMeta = map[string]struct{ title, desc string }{
	"home":        {"nodebench - benchmark a Linux server in one line", "CPU, disk and network benchmark for Linux servers. One command, no root, nothing left behind, and a link you can share."},
	"docs":        {"How it works - nodebench", "What nodebench measures, how, every option, and answers to the usual questions."},
	"locations":   {"Test locations - nodebench", "The iperf3 servers nodebench tests against, on four continents."},
	"leaderboard": {"Leaderboard - nodebench", "Self-reported nodebench results, ranked by CPU, disk and network."},
	"selfhost":    {"Self-host - nodebench", "Run your own nodebench share server. One Go binary, results stored as JSON files."},
	"privacy":     {"Privacy - nodebench", "What a nodebench upload contains, what it doesn't, and how to get a result removed."},
	"result":      {"", ""},
}

var pagePath = map[string]string{
	"home": "/", "docs": "/docs", "locations": "/locations", "leaderboard": "/leaderboard",
	"selfhost": "/self-host", "privacy": "/privacy",
}

func (s *server) loadTemplates() {
	funcs := template.FuncMap{
		"bytes": fmtBytes,
		"kbs":   fmtKBs,
		"iops":  fmtIOPS,
		"mbps":  fmtMbps,
		"ping":  fmtPing,
		"bar":   bar,
		"add":   func(a, b float64) float64 { return a + b },
		"inc":   func(i int) int { return i + 1 },
		"yesno": func(b bool) string {
			if b {
				return "yes"
			}
			return "no"
		},
	}
	base := template.Must(template.New("base").Funcs(funcs).ParseFS(templateFS,
		"templates/layout.html", "templates/charts.html"))
	s.pages = map[string]*template.Template{}
	for name := range pageMeta {
		t := template.Must(base.Clone())
		s.pages[name] = template.Must(t.ParseFS(templateFS, "templates/"+name+".html"))
	}
	sum := sha256.Sum256(siteCSS)
	s.cssVer = hex.EncodeToString(sum[:4])
}

func (s *server) render(w http.ResponseWriter, name string, p page) {
	p.Name = name
	p.Base = *baseURL
	p.OneLiner = s.oneLiner()
	p.CSSVer = s.cssVer
	if m := pageMeta[name]; p.Title == "" {
		p.Title, p.Desc = m.title, m.desc
	}
	if p.OG == "" {
		p.OG, p.OGW, p.OGH = *baseURL+"/static/social.png", 1280, 640
	}
	if p.URL == "" {
		p.URL = *baseURL + pagePath[name]
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.pages[name].ExecuteTemplate(w, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (s *server) handlePage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := page{}
		switch name {
		case "home":
			p.Data = s.homeData()
		case "docs":
			p.Data = map[string]any{"Sites": standardSites(), "Total": len(sites)}
		case "locations":
			p.Data = map[string]any{"Sites": sites, "Map": netMap(nil), "Regions": regionNames, "Standard": standardSites(), "Total": len(sites), "Extra": len(sites) - standardSites()}
		case "leaderboard":
			p.Data = s.boardData(r)
		}
		s.render(w, name, p)
	}
}

type change struct{ Date, Text string }

// changelog is short on purpose, the git log has the details.
var changelog = []change{
	{"2026-10-07", "1.1.0: steal time, disk type, packet loss, -q quick runs, -l regions, -x for 17 locations"},
	{"2026-10-07", "Opt-in leaderboard with -L"},
	{"2026-10-07", "The city is no longer stored or shown, also for older results"},
	{"2026-10-07", "InstantNode Eygelshoven added as the first test location"},
}

func (s *server) homeData() map[string]any {
	d := map[string]any{
		"Sites": standardSites(), "Total": len(sites), "Extra": len(sites) - standardSites(), "SiteList": sites,
		"Terminal": ansiToHTML(terminalCapture), "Recent": s.board.recent(10),
		"Top": s.board.top(filter{Metric: metrics[0], Limit: 5}), "Changes": changelog,
	}
	if *example == "" {
		return d
	}
	if r, err := s.store.Load(*example); err == nil {
		d["Example"] = s.view(r)
	}
	return d
}

type boardRow struct {
	Rank  int
	E     *entry
	Value string
	Frac  float64
}

func (s *server) boardData(r *http.Request) map[string]any {
	q := r.URL.Query()
	f := filter{Metric: metricByKey(q.Get("m")), Virt: q.Get("virt"), Country: strings.ToUpper(q.Get("country")), Limit: 100}
	list := s.board.top(f)
	rows := make([]boardRow, len(list))
	for i, e := range list {
		v := f.Metric.value(e)
		rows[i] = boardRow{Rank: i + 1, E: e, Value: f.Metric.format(v), Frac: v / f.Metric.value(list[0])}
	}
	virts, countries := s.board.facets()
	return map[string]any{
		"Metrics": metrics, "M": f.Metric, "Virt": f.Virt, "Country": f.Country,
		"Rows": rows, "Virts": virts, "Countries": countries, "Total": s.board.size(),
	}
}

// handleHide takes a result off the leaderboard. The result page itself
// stays up, whoever has the link can still see it.
func (s *server) handleHide(w http.ResponseWriter, r *http.Request) {
	token := os.Getenv("NODEBENCH_ADMIN_TOKEN")
	if *admin != "" {
		token = *admin
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		jsonError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	if !validID(id) {
		jsonError(w, http.StatusNotFound, "no such result")
		return
	}
	if err := s.board.hide(id); err != nil {
		log.Printf("hide %s: %v", id, err)
		jsonError(w, http.StatusInternalServerError, "could not hide")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
