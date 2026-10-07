package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Result struct {
	ID      string    `json:"id,omitempty"`
	Created time.Time `json:"created,omitempty"`

	Version  string    `json:"version"`
	Duration int64     `json:"duration_s,omitempty"`
	Quick    bool      `json:"quick,omitempty"`
	System   System    `json:"system"`
	Location *Location `json:"location,omitempty"`
	IPv4     bool      `json:"ipv4"`
	IPv6     bool      `json:"ipv6"`
	CPU      *CPU      `json:"cpu,omitempty"`
	Disk     *Disk     `json:"disk,omitempty"`
	Net      *Net      `json:"net,omitempty"`

	// Leaderboard is opt-in from the client (-L). Name is what shows up
	// there, it falls back to the provider name.
	Leaderboard bool   `json:"leaderboard,omitempty"`
	Name        string `json:"name,omitempty"`
}

type System struct {
	OS      string  `json:"os"`
	Kernel  string  `json:"kernel"`
	Arch    string  `json:"arch"`
	CPU     string  `json:"cpu"`
	Cores   int     `json:"cores"`
	MHz     float64 `json:"mhz"`
	AES     bool    `json:"aes"`
	VMX     bool    `json:"vmx"`
	Virt    string  `json:"virt"`
	RAM     int64   `json:"ram_kib"`
	Swap    int64   `json:"swap_kib"`
	DiskKiB int64   `json:"disk_kib"`
	Uptime  int64   `json:"uptime_s"`
}

type Location struct {
	ASN     string `json:"asn"`
	Org     string `json:"org"`
	Country string `json:"country"`
	// Older clients send the city. It's accepted so they keep working,
	// but never stored or shown: together with the ASN it's too close
	// to pinning down a home connection.
	City string `json:"city,omitempty"`
}

// CPU throughput values are in bytes per second as reported by openssl speed.
type CPU struct {
	OpenSSL string  `json:"openssl"`
	Threads int     `json:"threads"`
	SHA256  float64 `json:"sha256_1"`
	SHA256N float64 `json:"sha256_n"`
	AES     float64 `json:"aes_1"`
	AESN    float64 `json:"aes_n"`
	// share of cpu time taken by the hypervisor during the multi thread run
	Steal float64 `json:"steal_pct,omitempty"`
}

type Disk struct {
	Tool  string     `json:"tool"`
	Size  string     `json:"size"`
	FS    string     `json:"fs,omitempty"`
	Media string     `json:"media,omitempty"`
	Tests []DiskTest `json:"tests"`
}

type DiskTest struct {
	BS        string  `json:"bs"`
	ReadKBs   float64 `json:"read_kbs"`
	WriteKBs  float64 `json:"write_kbs"`
	ReadIOPS  float64 `json:"read_iops"`
	WriteIOPS float64 `json:"write_iops"`
}

type Net struct {
	Tool  string    `json:"tool"`
	Tests []NetTest `json:"tests"`
}

type NetTest struct {
	Provider string  `json:"provider"`
	Location string  `json:"location"`
	Proto    int     `json:"proto"`
	Send     float64 `json:"send_mbps"`
	Recv     float64 `json:"recv_mbps"`
	Ping     float64 `json:"ping_ms"`
	Loss     float64 `json:"loss_pct,omitempty"`
}

const maxBody = 32 << 10

func decodeResult(b []byte) (*Result, error) {
	if len(b) > maxBody {
		return nil, errors.New("body too large")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var r Result
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("bad json: %w", err)
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	r.ID = ""
	r.Created = time.Time{}
	r.scrub()
	return &r, nil
}

// scrub drops everything that must not be published. It runs on upload
// and again on every load, so results stored before a field was dropped
// don't leak it either.
func (r *Result) scrub() {
	if r.Location != nil {
		r.Location.City = ""
		// InstantNode's own network, the servers are in Eygelshoven
		if r.Location.ASN == "AS49581" {
			r.Location.Org, r.Location.Country = "InstantNode", "NL"
		}
	}
	if !r.Leaderboard {
		r.Name = ""
	}
}

func (r *Result) validate() error {
	if r.CPU == nil && r.Disk == nil && r.Net == nil {
		return errors.New("no test results")
	}
	s := &r.System
	for _, f := range []*string{&r.Version, &s.OS, &s.Kernel, &s.Arch, &s.CPU, &s.Virt} {
		if err := cleanString(f, 96); err != nil {
			return err
		}
	}
	if r.Duration < 0 || r.Duration > 86400 {
		return errors.New("duration out of range")
	}
	if s.Cores < 0 || s.Cores > 4096 {
		return errors.New("cores out of range")
	}
	if err := nonneg(s.MHz, float64(s.RAM), float64(s.Swap), float64(s.DiskKiB), float64(s.Uptime)); err != nil {
		return err
	}

	if err := cleanName(&r.Name); err != nil {
		return err
	}

	if l := r.Location; l != nil {
		for _, f := range []*string{&l.ASN, &l.Org, &l.Country, &l.City} {
			if err := cleanString(f, 64); err != nil {
				return err
			}
		}
	}

	if c := r.CPU; c != nil {
		if err := cleanString(&c.OpenSSL, 64); err != nil {
			return err
		}
		if c.Threads < 0 || c.Threads > 4096 {
			return errors.New("threads out of range")
		}
		if err := nonneg(c.SHA256, c.SHA256N, c.AES, c.AESN); err != nil {
			return err
		}
		if c.Steal < 0 || c.Steal > 100 {
			return errors.New("steal out of range")
		}
	}

	if d := r.Disk; d != nil {
		if len(d.Tests) == 0 || len(d.Tests) > 8 {
			return errors.New("disk: bad number of tests")
		}
		if err := cleanString(&d.Tool, 16); err != nil {
			return err
		}
		if err := cleanString(&d.Size, 16); err != nil {
			return err
		}
		if err := cleanString(&d.FS, 16); err != nil {
			return err
		}
		switch d.Media {
		case "", "nvme", "ssd", "hdd", "virtual":
		default:
			return errors.New("disk: unknown media")
		}
		for i := range d.Tests {
			t := &d.Tests[i]
			if err := cleanString(&t.BS, 16); err != nil {
				return err
			}
			if err := nonneg(t.ReadKBs, t.WriteKBs, t.ReadIOPS, t.WriteIOPS); err != nil {
				return err
			}
		}
	}

	if n := r.Net; n != nil {
		if len(n.Tests) == 0 || len(n.Tests) > 40 {
			return errors.New("net: bad number of tests")
		}
		if err := cleanString(&n.Tool, 16); err != nil {
			return err
		}
		for i := range n.Tests {
			t := &n.Tests[i]
			if err := cleanString(&t.Provider, 32); err != nil {
				return err
			}
			if err := cleanString(&t.Location, 48); err != nil {
				return err
			}
			if t.Proto != 4 && t.Proto != 6 {
				return errors.New("net: proto must be 4 or 6")
			}
			if err := nonneg(t.Send, t.Recv, t.Ping); err != nil {
				return err
			}
			if t.Loss < 0 || t.Loss > 100 {
				return errors.New("net: loss out of range")
			}
		}
	}
	return nil
}

func cleanString(s *string, max int) error {
	if !utf8.ValidString(*s) {
		return errors.New("invalid utf-8")
	}
	v := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, *s)
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > max {
		return fmt.Errorf("string too long: %.20q...", v)
	}
	*s = v
	return nil
}

// cleanName allows a short display name for the leaderboard. Anything that
// looks like a link is refused rather than mangled, so nobody gets a
// free ad slot out of it.
func cleanName(s *string) error {
	if err := cleanString(s, 32); err != nil {
		return errors.New("name: longer than 32 characters")
	}
	v := strings.Join(strings.Fields(*s), " ")
	for _, r := range v {
		if !unicode.IsPrint(r) {
			return errors.New("name: unprintable character")
		}
	}
	low := strings.ToLower(v)
	for _, bad := range []string{"://", "www.", "<", ">"} {
		if strings.Contains(low, bad) {
			return errors.New("name: no links or markup")
		}
	}
	if domainLike.MatchString(low) {
		return errors.New("name: no links or markup")
	}
	*s = v
	return nil
}

var domainLike = regexp.MustCompile(`[a-z0-9-]\.(com|net|org|io|eu|de|gg|xyz|me|co|dev|app|cloud|host|ru|cn|uk|nl|link|shop|site|online|store)\b`)

// plausible catches numbers no real machine produces today. A result that
// fails this is still stored and shareable, it just never ranks.
func (r *Result) plausible() bool {
	if c := r.CPU; c != nil {
		perCore := c.SHA256N / float64(max(c.Threads, 1))
		if c.SHA256 > 8e9 || c.AES > 30e9 || perCore > 8e9 || c.SHA256N > 2e12 || c.AESN > 6e12 {
			return false
		}
		if c.SHA256N > 0 && c.SHA256 > c.SHA256N*1.5 {
			return false
		}
	}
	if d := r.Disk; d != nil {
		for _, t := range d.Tests {
			// about 30 GB/s and 5M IOPS combined, beyond a fast NVMe array
			if t.ReadKBs+t.WriteKBs > 30e6 || t.ReadIOPS+t.WriteIOPS > 5e6 {
				return false
			}
		}
	}
	if n := r.Net; n != nil {
		for _, t := range n.Tests {
			if t.Send > 100000 || t.Recv > 100000 || (t.Ping > 0 && t.Ping < 0.05) {
				return false
			}
		}
	}
	return r.System.Cores <= 1024
}

// 1e13 is roughly 10 TB/s, anything above is garbage.
func nonneg(v ...float64) error {
	for _, f := range v {
		if f < 0 || f > 1e13 || f != f {
			return errors.New("number out of range")
		}
	}
	return nil
}
