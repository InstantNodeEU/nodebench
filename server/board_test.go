package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCityIsDropped(t *testing.T) {
	r, err := decodeResult([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if r.Location.City != "" {
		t.Fatalf("city survived decode: %q", r.Location.City)
	}

	// a result stored before the city was dropped
	s := &Store{dir: t.TempDir()}
	old := `{"id":"oldCity1","version":"1","system":{"cores":1},"location":{"asn":"AS1","org":"X","country":"DE","city":"Idar-Oberstein"},"ipv4":true,"ipv6":false,"cpu":{"threads":1,"sha256_1":1,"sha256_n":1,"aes_1":1,"aes_n":1}}`
	os.MkdirAll(filepath.Join(s.dir, "ol"), 0o755)
	os.WriteFile(filepath.Join(s.dir, "ol", "oldCity1.json"), []byte(old), 0o644)

	srv := &server{store: s, board: newBoard(s.dir)}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/r/oldCity1.json", nil)
	req.SetPathValue("id", "oldCity1.json")
	srv.handleResult(w, req)
	if w.Code != 200 || strings.Contains(w.Body.String(), "Idar") || strings.Contains(w.Body.String(), "city") {
		t.Fatalf("city leaked: %d %s", w.Code, w.Body.String())
	}
}

func TestCleanName(t *testing.T) {
	ok := map[string]string{
		"  fra   edge 01 ": "fra edge 01",
		"Hetzner CX22":     "Hetzner CX22",
		"node.js box":      "node.js box",
		"":                 "",
	}
	for in, want := range ok {
		got := in
		if err := cleanName(&got); err != nil || got != want {
			t.Errorf("cleanName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		strings.Repeat("x", 33),
		"cheap vps at example.com",
		"https://spam",
		"visit www.spam",
		"<b>hi</b>",
		"buy-now.shop",
	} {
		v := bad
		if err := cleanName(&v); err == nil {
			t.Errorf("cleanName(%q) accepted", bad)
		}
	}
}

func boardResult(t *testing.T, id, name string, sha float64, mod func(*Result)) *Result {
	t.Helper()
	r, err := decodeResult([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	r.ID, r.Name, r.Leaderboard = id, name, true
	r.Created = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	r.CPU.SHA256N = sha
	if mod != nil {
		mod(r)
	}
	return r
}

func TestBoard(t *testing.T) {
	b := newBoard(t.TempDir())
	b.add(boardResult(t, "aaaaaaa1", "alpha", 9e9, nil))
	b.add(boardResult(t, "aaaaaaa2", "alpha", 12e9, nil)) // same box, better run
	b.add(boardResult(t, "bbbbbbb1", "beta", 10e9, func(r *Result) { r.System.Virt = "lxc"; r.Location.Country = "NL" }))
	b.add(boardResult(t, "ccccccc1", "gamma", 11e9, func(r *Result) { r.Leaderboard = false }))
	b.add(boardResult(t, "ddddddd1", "fake", 9e15, func(r *Result) { r.CPU.SHA256N = 9e12 }))

	got := b.top(filter{Metric: metricByKey("cpu")})
	if len(got) != 2 || got[0].ID != "aaaaaaa2" || got[1].ID != "bbbbbbb1" {
		t.Fatalf("unexpected order: %v", ids(got))
	}
	if got := b.top(filter{Metric: metricByKey("cpu"), Virt: "lxc"}); len(got) != 1 || got[0].ID != "bbbbbbb1" {
		t.Fatalf("virt filter: %v", ids(got))
	}
	if got := b.top(filter{Metric: metricByKey("cpu"), Country: "nl"}); len(got) != 1 {
		t.Fatalf("country filter: %v", ids(got))
	}
	if got := b.top(filter{Metric: metricByKey("net")}); len(got) != 2 {
		t.Fatalf("net metric: %v", ids(got))
	}

	if err := b.hide("aaaaaaa2"); err != nil {
		t.Fatal(err)
	}
	got = b.top(filter{Metric: metricByKey("cpu")})
	if len(got) != 2 || got[0].ID != "bbbbbbb1" || got[1].ID != "aaaaaaa1" {
		t.Fatalf("after hide: %v", ids(got))
	}

	// the hidden list survives a restart
	b2 := newBoard(filepath.Dir(b.file))
	if err := b2.load(&Store{dir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	b2.add(boardResult(t, "aaaaaaa2", "alpha", 12e9, nil))
	if b2.size() != 0 {
		t.Fatal("hidden result came back after reload")
	}
}

func TestHideEndpoint(t *testing.T) {
	dir := t.TempDir()
	s := &server{store: &Store{dir: dir}, board: newBoard(dir)}
	s.board.add(boardResult(t, "aaaaaaa1", "alpha", 9e9, nil))
	t.Setenv("NODEBENCH_ADMIN_TOKEN", "s3cret")

	call := func(auth string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/results/aaaaaaa1/hide", nil)
		req.SetPathValue("id", "aaaaaaa1")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		s.handleHide(w, req)
		return w.Code
	}
	if c := call(""); c != http.StatusUnauthorized {
		t.Fatalf("no token: %d", c)
	}
	if c := call("Bearer wrong"); c != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", c)
	}
	if s.board.size() != 1 {
		t.Fatal("hidden without a valid token")
	}
	if c := call("Bearer s3cret"); c != http.StatusNoContent {
		t.Fatalf("good token: %d", c)
	}
	if s.board.size() != 0 {
		t.Fatal("still on the board")
	}
}

func TestPlausible(t *testing.T) {
	r, _ := decodeResult([]byte(sample))
	if !r.plausible() {
		t.Fatal("sample rejected")
	}
	r.Disk.Tests[0].ReadIOPS = 9e6
	if r.plausible() {
		t.Fatal("9M IOPS accepted")
	}
}

func ids(es []*entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}
