package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := newID()
		if !validID(id) {
			t.Fatalf("generated invalid id %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestValidID(t *testing.T) {
	for _, id := range []string{"", "abc", "abcdefg!", "../../etc", "abcdefghi", "abcd.fgh"} {
		if validID(id) {
			t.Errorf("validID(%q) = true", id)
		}
	}
	if !validID("aZ09bY18") {
		t.Error("rejected a good id")
	}
}

func TestStoreRoundtrip(t *testing.T) {
	s := &Store{dir: t.TempDir()}
	r, err := decodeResult([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(r); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.System.CPU != r.System.CPU || len(got.Disk.Tests) != 4 || got.Created.IsZero() {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if _, err := s.Load("zzzzzzzz"); err != errNotFound {
		t.Fatalf("want errNotFound, got %v", err)
	}
	if _, err := s.Load("../" + r.ID[:5]); err != errNotFound {
		t.Fatalf("path traversal not rejected: %v", err)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := map[string]string{
		"unknown field": strings.Replace(sample, `"ipv4"`, `"ip":"1.2.3.4","ipv4"`, 1),
		"no tests":      `{"version":"1","system":{"cores":1},"ipv4":true,"ipv6":false}`,
		"negative":      strings.Replace(sample, `"recv_mbps": 912.4`, `"recv_mbps": -1`, 1),
		"bad proto":     strings.Replace(sample, `"proto": 4`, `"proto": 5`, 1),
		"long string":   strings.Replace(sample, `"kvm"`, `"`+strings.Repeat("x", 200)+`"`, 1),
		"too big":       sample + strings.Repeat(" ", maxBody),
	}
	for name, body := range cases {
		if _, err := decodeResult([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDecodeStripsControlChars(t *testing.T) {
	body := strings.Replace(sample, `"Ubuntu 24.04.1 LTS"`, `"Ubuntu\u001b[31m 24.04  "`, 1)
	r, err := decodeResult([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if r.System.OS != "Ubuntu[31m 24.04" {
		t.Fatalf("got %q", r.System.OS)
	}
}

func TestShortCPU(t *testing.T) {
	cases := map[string]string{
		"Intel(R) Xeon(R) Gold 6338 CPU @ 2.00GHz":   "Intel Xeon Gold 6338",
		"AMD EPYC 7763 64-Core Processor":            "AMD EPYC 7763 64-Core",
		"AMD Ryzen 7 7840HS w/ Radeon 780M Graphics": "AMD Ryzen 7 7840HS w/ Radeon 780M Graphics",
		"  ": "Unknown CPU",
	}
	for in, want := range cases {
		if got := shortCPU(in); got != want {
			t.Errorf("shortCPU(%q) = %q, want %q", in, got, want)
		}
	}
}

const sample = `{
  "version": "1.0.0",
  "system": {"os": "Ubuntu 24.04.1 LTS", "kernel": "6.8.0-45-generic", "arch": "x86_64",
    "cpu": "AMD EPYC 7763 64-Core Processor", "cores": 4, "mhz": 2445.4, "aes": true, "vmx": false,
    "virt": "kvm", "ram_kib": 8148516, "swap_kib": 0, "disk_kib": 83886080, "uptime_s": 93211},
  "location": {"asn": "AS3320", "org": "Deutsche Telekom AG", "country": "DE", "city": "Frankfurt"},
  "ipv4": true, "ipv6": false,
  "cpu": {"openssl": "OpenSSL 3.0.13", "threads": 4, "sha256_1": 2286660266, "sha256_n": 9010000000, "aes_1": 5472835232, "aes_n": 21000000000},
  "disk": {"tool": "fio", "size": "2G", "tests": [
    {"bs": "4k", "read_kbs": 180000, "write_kbs": 180500, "read_iops": 45000, "write_iops": 45125},
    {"bs": "64k", "read_kbs": 1400000, "write_kbs": 1410000, "read_iops": 21875, "write_iops": 22031},
    {"bs": "512k", "read_kbs": 1900000, "write_kbs": 2000000, "read_iops": 3710, "write_iops": 3906},
    {"bs": "1m", "read_kbs": 2100000, "write_kbs": 2200000, "read_iops": 2050, "write_iops": 2148}]},
  "net": {"tool": "iperf3", "tests": [
    {"provider": "Clouvider", "location": "London, UK", "proto": 4, "send_mbps": 905.1, "recv_mbps": 912.4, "ping_ms": 9.8},
    {"provider": "Leaseweb", "location": "Singapore, SG", "proto": 4, "send_mbps": 610.0, "recv_mbps": 0, "ping_ms": 161.2}]}
}`

func TestASNAliases(t *testing.T) {
	old := aliases
	defer func() { aliases = old }()
	aliases = parseAliases("AS64500=Example Hosting:nl, bogus, AS64501=Plain Net")

	r, err := decodeResult([]byte(`{"version":"1","system":{"cores":1},"location":{"asn":"AS64500","org":"Holding Company GmbH","country":"DE"},"ipv4":true,"ipv6":false,"cpu":{"threads":1,"sha256_1":1,"sha256_n":1,"aes_1":1,"aes_n":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Location.Org != "Example Hosting" || r.Location.Country != "NL" {
		t.Fatalf("got %q / %q", r.Location.Org, r.Location.Country)
	}

	l := &Location{ASN: "AS64501", Org: "x", Country: "FR"}
	applyAlias(l)
	if l.Org != "Plain Net" || l.Country != "FR" {
		t.Fatalf("alias without country: %q / %q", l.Org, l.Country)
	}

	w := httptest.NewRecorder()
	handleAlias(w, httptest.NewRequest("GET", "/api/alias?asn=AS64500", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"org":"Example Hosting"`) {
		t.Fatalf("alias endpoint: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handleAlias(w, httptest.NewRequest("GET", "/api/alias?asn=AS1", nil))
	if w.Code != 404 {
		t.Fatalf("unknown asn: %d", w.Code)
	}
}
