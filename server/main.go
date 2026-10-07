package main

import (
	"bytes"
	"compress/gzip"
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

//go:embed static/site.css
var siteCSS []byte

// a real run of the script, shown on the home page
//
//go:embed terminal.ansi
var terminalCapture string

//go:embed fonts/*.ttf
var fontFS embed.FS

//go:embed static
var staticFS embed.FS

var (
	listen  = flag.String("listen", env("NODEBENCH_LISTEN", ":8080"), "listen address")
	dataDir = flag.String("data", env("NODEBENCH_DATA", "./data"), "directory for stored results")
	baseURL = flag.String("base", env("NODEBENCH_BASE", "http://localhost:8080"), "public base url, used for share links")
	script  = flag.String("script", env("NODEBENCH_SCRIPT", "../nodebench.sh"), "path to nodebench.sh, served to curl and wget")
	proxied = flag.Bool("proxy", env("NODEBENCH_PROXY", "") != "", "trust X-Forwarded-For (only behind a reverse proxy)")
	perHour = flag.Int("rate", 20, "max uploads per ip per hour")
	example = flag.String("example", env("NODEBENCH_EXAMPLE", ""), "result id shown as the example on the landing page")
	admin   = flag.String("admin-token", "", "bearer token for moderation, also read from NODEBENCH_ADMIN_TOKEN")
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type server struct {
	store   *Store
	board   *board
	pages   map[string]*template.Template
	cssVer  string
	world   []byte
	fine    []byte
	limiter *limiter
}

func main() {
	flag.Parse()
	*baseURL = strings.TrimRight(*baseURL, "/")

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatal(err)
	}

	s := &server{
		store:   &Store{dir: *dataDir},
		board:   newBoard(*dataDir),
		world:   worldSVG(mapStep, mapDot),
		fine:    gzipped(worldSVG(fineStep, fineDot)),
		limiter: newLimiter(*perHour, time.Hour),
	}
	s.loadTemplates()
	start := time.Now()
	if err := s.board.load(s.store); err != nil {
		log.Fatal(err)
	}
	log.Printf("leaderboard: %d entries loaded in %s", s.board.size(), time.Since(start).Round(time.Millisecond))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	for name, path := range pagePath {
		if name != "home" {
			mux.HandleFunc("GET "+path, s.handlePage(name))
		}
	}
	mux.Handle("GET /how", http.RedirectHandler("/docs", http.StatusMovedPermanently))
	mux.HandleFunc("POST /api/results", s.handleUpload)
	mux.HandleFunc("POST /api/results/{id}/hide", s.handleHide)
	mux.HandleFunc("GET /r/{id}", s.handleResult)
	mux.Handle("GET /fonts/", longCache(http.FileServerFS(fontFS)))
	mux.HandleFunc("GET /static/world.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=2592000")
		w.Write(s.world)
	})
	mux.HandleFunc("GET /static/world-fine.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=2592000")
		w.Header().Set("Vary", "Accept-Encoding")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			w.Write(s.fine)
			return
		}
		zr, err := gzip.NewReader(bytes.NewReader(s.fine))
		if err == nil {
			io.Copy(w, zr)
		}
	})
	mux.Handle("GET /static/", longCache(http.FileServerFS(staticFS)))
	mux.Handle("GET /favicon.ico", http.RedirectHandler("/static/favicon.svg", http.StatusMovedPermanently))
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
	s.handlePage("home")(w, r)
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
	s.board.add(res)
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
		v := s.view(res)
		s.render(w, "result", page{
			Title: v.CPU + " - nodebench", Desc: v.Summary, URL: v.URL,
			OG: v.URL + ".png", OGW: 1200, OGH: 630, Data: v,
		})
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

func gzipped(b []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

func longCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=2592000")
		h.ServeHTTP(w, r)
	})
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
