package main

import (
	"fmt"
	"math"
	"strings"
)

// fmtKBs formats fio bandwidth (KiB/s) the way fio and yabs print it.
func fmtKBs(kbs float64) string {
	switch {
	case kbs >= 1024*1024:
		return fmt.Sprintf("%.2f GB/s", kbs/1024/1024)
	case kbs >= 1024:
		return fmt.Sprintf("%.1f MB/s", kbs/1024)
	default:
		return fmt.Sprintf("%.0f KB/s", kbs)
	}
}

func fmtBytes(bps float64) string {
	switch {
	case bps >= 1e9:
		return fmt.Sprintf("%.2f GB/s", bps/1e9)
	default:
		return fmt.Sprintf("%.0f MB/s", bps/1e6)
	}
}

func fmtIOPS(v float64) string {
	switch {
	case v >= 1e6:
		return fmt.Sprintf("%.2fM", v/1e6)
	case v >= 1e5:
		return fmt.Sprintf("%.0fk", v/1e3)
	case v >= 1e4:
		return fmt.Sprintf("%.1fk", v/1e3)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

func fmtMbps(v float64) string {
	switch {
	case v <= 0:
		return "-"
	case v >= 1000:
		return fmt.Sprintf("%.2f Gbit/s", v/1000)
	default:
		return fmt.Sprintf("%.0f Mbit/s", v)
	}
}

func fmtPing(v float64) string {
	if v <= 0 {
		return "-"
	}
	if v < 10 {
		return fmt.Sprintf("%.1f ms", v)
	}
	return fmt.Sprintf("%.0f ms", v)
}

func fmtKiB(kib int64) string {
	v := float64(kib)
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v >= 100 || v == math.Trunc(v) {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func fmtUptime(s int64) string {
	d := s / 86400
	h := s % 86400 / 3600
	m := s % 3600 / 60
	if d > 0 {
		return fmt.Sprintf("%dd %dh", d, h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

func fmtMHz(v float64) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2f GHz", v/1000)
}

// shortCPU strips the usual marketing noise from cpu model strings,
// "Intel(R) Xeon(R) Gold 6338 CPU @ 2.00GHz" -> "Intel Xeon Gold 6338".
func shortCPU(s string) string {
	for _, junk := range []string{"(R)", "(TM)", "(tm)", " CPU", " Processor", " processor"} {
		s = strings.ReplaceAll(s, junk, "")
	}
	if i := strings.Index(s, " @ "); i > 0 {
		s = s[:i]
	}
	if i := strings.Index(s, " with Radeon"); i > 0 {
		s = s[:i]
	}
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "Unknown CPU"
	}
	return s
}

func virtName(v string) string {
	switch strings.ToLower(v) {
	case "", "none":
		return "Dedicated"
	case "kvm":
		return "KVM"
	case "qemu":
		return "QEMU"
	case "lxc":
		return "LXC"
	case "openvz":
		return "OpenVZ"
	case "vmware":
		return "VMware"
	case "microsoft":
		return "Hyper-V"
	case "xen":
		return "Xen"
	case "docker":
		return "Docker"
	}
	return v
}
