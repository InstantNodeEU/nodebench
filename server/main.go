package main

import (
	"embed"
	"encoding/json"
	"flag"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

//go:embed templates
var templateFS embed.FS

//go:embed fonts/*.ttf
var fontFS embed.FS

var (
	listen  = flag.String("listen", env("NODEBENCH_LISTEN", ":8080"), "listen address")
	dataDir = flag.String("data", env("NODEBENCH_DATA", "./data"), "directory for stored results")
	baseURL = flag.String("base", env("NODEBENCH_BASE", "http://localhost:8080"), "public base url, used for share links")
	script  = flag.String("script", env("NODEBENCH_SCRIPT", "../nodebench.sh"), "path to nodebench.sh, served to curl and wget")
	proxied = flag.Bool("proxy", env("NODEBENCH_PROXY", "") != "", "trust X-Forwarded-For (only behind a reverse proxy)")
	perHour = flag.Int("rate", 20, "max uploads per ip per hour")
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type server struct {
	store   *Store
	css     template.CSS
	result  *template.Template
	index   *template.Template
	limiter *limiter
}

func main() {
	flag.Parse()
	*baseURL = strings.TrimRight(*baseURL, "/")

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatal(err)
	}

	css, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		log.Fatal(err)
	}
	funcs := template.FuncMap{
		"bytes": fmtBytes,
		"kbs":   fmtKBs,
		"iops":  fmtIOPS,
		"mbps":  fmtMbps,
		"ping":  fmtPing,
		"add":   func(a, b float64) float64 { return a + b },
		"yesno": func(b bool) string {
			if b {
				return "yes"
			}
			return "no"
		},
	}
	s := &server{
		store:   &Store{dir: *dataDir},
		css:     template.CSS(css),
		result:  template.Must(template.New("result.html").Funcs(funcs).ParseFS(templateFS, "templates/result.html")),
		index:   template.Must(template.New("index.html").ParseFS(templateFS, "templates/index.html")),
		limiter: newLimiter(*perHour, time.Hour),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("POST /api/results", s.handleUpload)
	mux.HandleFunc("GET /r/{id}", s.handleResult)
	mux.HandleFunc("GET /fonts/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=2592000")
		http.FileServerFS(fontFS).ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok\n") })

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	log.Printf("nodebench server on %s, data in %s", *listen, *dataDir)
	log.Fatal(srv.ListenAndServe())
}

func (s *server) oneLiner() string {
	return "curl -sL " + strings.TrimPrefix(strings.TrimPrefix(*baseURL, "https://"), "http://") + " | bash"
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	ua := strings.ToLower(r.UserAgent())
	if strings.HasPrefix(ua, "curl/") || strings.HasPrefix(ua, "wget/") {
		f, err := os.Open(*script)
		if err != nil {
			log.Printf("script: %v", err)
			http.Error(w, "script not available", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		io.Copy(w, f)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := s.index.Execute(w, map[string]any{"CSS": s.css, "OneLiner": s.oneLiner()})
	if err != nil {
		log.Printf("index: %v", err)
	}
}

func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(clientIP(r)) {
		jsonError(w, http.StatusTooManyRequests, "too many uploads, try again later")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		jsonError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	res, err := decodeResult(body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.Save(res); err != nil {
		log.Printf("save: %v", err)
		jsonError(w, http.StatusInternalServerError, "could not save result")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"id":  res.ID,
		"url": *baseURL + "/r/" + res.ID,
	})
}

func (s *server) handleResult(w http.ResponseWriter, r *http.Request) {
	id, ext, _ := strings.Cut(r.PathValue("id"), ".")
	res, err := s.store.Load(id)
	if err == errNotFound {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("load %s: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// results never change once stored
	w.Header().Set("Cache-Control", "public, max-age=86400")

	switch ext {
	case "":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.result.Execute(w, s.view(res)); err != nil {
			log.Printf("render %s: %v", id, err)
		}
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(res)
	case "png":
		w.Header().Set("Content-Type", "image/png")
		if err := renderCard(w, res); err != nil {
			log.Printf("card %s: %v", id, err)
		}
	default:
		http.NotFound(w, r)
	}
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func clientIP(r *http.Request) string {
	if *proxied {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limiter is a fixed window counter per ip. Good enough to stop
// someone from filling the disk with a loop.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*bucket
}

type bucket struct {
	n     int
	start time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	l := &limiter{max: max, window: window, hits: map[string]*bucket{}}
	go func() {
		for range time.Tick(window) {
			l.mu.Lock()
			for k, b := range l.hits {
				if time.Since(b.start) > l.window {
					delete(l.hits, k)
				}
			}
			l.mu.Unlock()
		}
	}()
	return l
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.hits[ip]
	if b == nil || time.Since(b.start) > l.window {
		l.hits[ip] = &bucket{n: 1, start: time.Now()}
		return true
	}
	if b.n >= l.max {
		return false
	}
	b.n++
	return true
}
