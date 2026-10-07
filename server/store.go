package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const idChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

var errNotFound = errors.New("not found")

type Store struct {
	dir string
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	for i := range b {
		// 62*4 = 248, so values >= 248 would skew the distribution
		for b[i] >= 248 {
			var one [1]byte
			rand.Read(one[:])
			b[i] = one[0]
		}
		b[i] = idChars[b[i]%62]
	}
	return string(b)
}

func validID(id string) bool {
	if len(id) != 8 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// path shards by the first two characters so a single directory
// doesn't end up with millions of files.
func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id[:2], id+".json")
}

func (s *Store) Save(r *Result) error {
	r.Created = time.Now().UTC().Truncate(time.Second)
	for range 5 {
		r.ID = newID()
		p := s.path(r.ID)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			os.Remove(p)
			return err
		}
		return f.Close()
	}
	return errors.New("could not allocate id")
}

func (s *Store) Load(id string) (*Result, error) {
	if !validID(id) {
		return nil, errNotFound
	}
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	r.scrub()
	return &r, nil
}
