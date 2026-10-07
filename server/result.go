package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Result struct {
	ID      string    `json:"id,omitempty"`
	Created time.Time `json:"created,omitempty"`

	Version  string    `json:"version"`
	System   System    `json:"system"`
	Location *Location `json:"location,omitempty"`
	IPv4     bool      `json:"ipv4"`
	IPv6     bool      `json:"ipv6"`
	CPU      *CPU      `json:"cpu,omitempty"`
	Disk     *Disk     `json:"disk,omitempty"`
	Net      *Net      `json:"net,omitempty"`
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
	City    string `json:"city"`
}

// CPU throughput values are in bytes per second as reported by openssl speed.
type CPU struct {
	OpenSSL string  `json:"openssl"`
	Threads int     `json:"threads"`
	SHA256  float64 `json:"sha256_1"`
	SHA256N float64 `json:"sha256_n"`
	AES     float64 `json:"aes_1"`
	AESN    float64 `json:"aes_n"`
}

type Disk struct {
	Tool  string     `json:"tool"`
	Size  string     `json:"size"`
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
	return &r, nil
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
	if s.Cores < 0 || s.Cores > 4096 {
		return errors.New("cores out of range")
	}
	if err := nonneg(s.MHz, float64(s.RAM), float64(s.Swap), float64(s.DiskKiB), float64(s.Uptime)); err != nil {
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
		if len(n.Tests) == 0 || len(n.Tests) > 16 {
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

// 1e13 is roughly 10 TB/s, anything above is garbage.
func nonneg(v ...float64) error {
	for _, f := range v {
		if f < 0 || f > 1e13 || f != f {
			return errors.New("number out of range")
		}
	}
	return nil
}
