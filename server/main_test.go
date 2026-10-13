package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexServesScriptPerClient(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "nodebench.sh"), []byte("#!/usr/bin/env bash\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "nodebench.ps1"), []byte("# windows\n"), 0o644)
	old := *script
	*script = filepath.Join(dir, "nodebench.sh")
	defer func() { *script = old }()

	s := &server{}
	for ua, want := range map[string]string{
		"curl/8.5.0": "#!/usr/bin/env bash",
		"Wget/1.21":  "#!/usr/bin/env bash",
		"Mozilla/5.0 (Windows NT; Windows NT 10.0; de-DE) WindowsPowerShell/5.1.26100.1":      "# windows",
		"Mozilla/5.0 (Windows NT 10.0; Microsoft Windows 10.0.26100; en-US) PowerShell/7.4.6": "# windows",
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("User-Agent", ua)
		s.handleIndex(w, r)
		if got := strings.TrimSpace(w.Body.String()); got != want {
			t.Errorf("%s: got %q, want %q", ua, got, want)
		}
	}
}
