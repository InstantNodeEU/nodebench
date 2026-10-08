package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// An alias replaces the provider name and country that ipinfo reports for an
// ASN. Useful when a network is registered under a different company than the
// one people know it by, or in a different country than the servers are in.
//
//	NODEBENCH_ASN_ALIASES="AS64500=Example Hosting:NL,AS64501=Other Net"
type alias struct{ Org, Country string }

var aliases = map[string]alias{}

func parseAliases(s string) map[string]alias {
	m := map[string]alias{}
	for _, part := range strings.Split(s, ",") {
		asn, rest, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || !strings.HasPrefix(asn, "AS") || rest == "" {
			continue
		}
		org, country, _ := strings.Cut(rest, ":")
		m[asn] = alias{strings.TrimSpace(org), strings.ToUpper(strings.TrimSpace(country))}
	}
	return m
}

func applyAlias(l *Location) {
	if l == nil {
		return
	}
	a, ok := aliases[l.ASN]
	if !ok {
		return
	}
	l.Org = a.Org
	if a.Country != "" {
		l.Country = a.Country
	}
}

// GET /api/alias?asn=AS64500 lets the script print the same name in the
// terminal that the result page will show. Only the ASN is sent, not the IP.
func handleAlias(w http.ResponseWriter, r *http.Request) {
	a, ok := aliases[r.URL.Query().Get("asn")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	json.NewEncoder(w).Encode(map[string]string{"org": a.Org, "country": a.Country})
}
