package main

import (
	"strings"
	"testing"
)

func TestANSIToHTML(t *testing.T) {
	in := "\x1b[1mCPU\x1b[0m <x>\n  spin 1\r  spin 2\r\x1b[K\x1b[38;2;59;130;246mdone\x1b[0m\n"
	got := string(ansiToHTML(in))
	for _, want := range []string{`<span class="b">CPU</span> &lt;x&gt;`, `<span style="color:#3b82f6">done</span>`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "spin") || strings.Contains(got, "\x1b") {
		t.Errorf("spinner or escape left over: %q", got)
	}
}
