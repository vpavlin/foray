package server

// Writes sent twice, applied once (docs/design.md §7.1). An app's outbox
// sends a write again when it got no answer, and the first may have been
// applied: the answer was what got lost. Each write carries the outbox's id
// for it as Idempotency-Key; a repeat within a day gets the first answer and
// changes nothing.
//
// Kept in memory: a service restarted between the two sends applies the
// second. That is rare beside a lost answer, which is the case this is for.

import (
	"bytes"
	"net/http"
	"sync"
	"time"
)

const (
	onceFor    = 24 * time.Hour
	onceKeyMax = 128
)

type onceAnswer struct {
	code   int
	header http.Header
	body   []byte
	at     time.Time
	done   chan struct{} // closed once the first is answered
}

type onceCache struct {
	mu   sync.Mutex
	seen map[string]*onceAnswer
}

// begin is the answer kept for key, or nil when this is the first: then the
// caller answers it and calls finish. A repeat that arrives while the first
// is still being answered waits for it.
func (c *onceCache) begin(key string, now time.Time) (*onceAnswer, bool) {
	c.mu.Lock()
	if c.seen == nil {
		c.seen = map[string]*onceAnswer{}
	}
	for k, a := range c.seen {
		if now.Sub(a.at) > onceFor {
			delete(c.seen, k)
		}
	}
	if a, ok := c.seen[key]; ok {
		c.mu.Unlock()
		<-a.done
		return a, true
	}
	a := &onceAnswer{at: now, done: make(chan struct{})}
	c.seen[key] = a
	c.mu.Unlock()
	return a, false
}

// finish keeps what the first was answered. A write that failed on the
// service's side (5xx) is forgotten, so sending it again tries it again.
func (c *onceCache) finish(key string, a *onceAnswer, rec *onceRecorder) {
	a.code, a.header, a.body = rec.code, rec.Header().Clone(), rec.buf.Bytes()
	if a.code >= 500 {
		c.mu.Lock()
		delete(c.seen, key)
		c.mu.Unlock()
	}
	close(a.done)
}

func (a *onceAnswer) replay(w http.ResponseWriter) {
	for k, vs := range a.header {
		w.Header()[k] = vs
	}
	w.Header().Set("Idempotent-Replayed", "true")
	w.WriteHeader(a.code)
	w.Write(a.body)
}

// onceRecorder answers the caller and keeps a copy of the answer.
type onceRecorder struct {
	http.ResponseWriter
	code int
	buf  bytes.Buffer
}

func (r *onceRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *onceRecorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	r.buf.Write(b)
	return r.ResponseWriter.Write(b)
}

// once runs h for a write at most once per Idempotency-Key, per list and
// writer: two phones' keys never meet.
func (s *Server) once(w http.ResponseWriter, r *http.Request, l *list, by string, h listHandler) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > onceKeyMax {
		h(w, r, l, by)
		return
	}
	full := l.conf.Name + "\x00" + by + "\x00" + key
	a, repeat := s.applied.begin(full, time.Now())
	if repeat {
		a.replay(w)
		return
	}
	rec := &onceRecorder{ResponseWriter: w}
	defer func() { s.applied.finish(full, a, rec) }()
	h(rec, r, l, by)
}
