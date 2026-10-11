// Package server is Foray's HTTP API, served on this machine's mesh
// addresses and on loopback.
//
// A list exists only on its own mesh: asked from any other, it is not there
// (the rule shrooms-agent applies to sessions). Whoever the mesh names may
// read and edit a list on it — that is the access control, and the point:
// the people on the home mesh share the home list. A caller the mesh cannot
// name is no one and changes nothing. Loopback is this machine: the list's
// session's tools and the CLI, which may name themselves.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vpavlin/foray/internal/store"
	"rsc.io/qr"
)

// Headers callers use.
const (
	// AsHeader names a caller on loopback: the list's session's tools say
	// which session they are. Ignored from the mesh.
	AsHeader = "X-Foray-As"
	// FromSessionHeader is the agent session a suggestion comes from, as its
	// agent says — the same header, and the same trust, as shrooms-agent's
	// file drop: the machine is the mesh's word, the session its agent's.
	FromSessionHeader = "X-Shrooms-From-Session"
)

// Ask is a sender refused because it is not allowed to suggest: the apps
// offer to allow it.
type Ask struct {
	From  string    `json:"from"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

type list struct {
	conf  ListConfig
	store *store.List
	asks  []Ask
}

// Server holds the lists.
type Server struct {
	mu      sync.Mutex
	cfg     *Config
	cfgPath string
	lists   map[string]*list
	log     *slog.Logger

	// Who names a caller's address, "phone.home"; "" for one the mesh does
	// not know.
	Who func(netip.Addr) string
	// Self is this machine's name on the mesh.
	Self func() string
	// Notify tells a list's session something; set by main to post to
	// shrooms-agent, and by tests to record.
	Notify func(l ListConfig, text string)
	// Addr is this machine's address on a mesh.
	Addr func(mesh string) (netip.Addr, bool)
}

// New opens every configured list.
func New(cfg *Config, cfgPath string, log *slog.Logger) (*Server, error) {
	s := &Server{cfg: cfg, cfgPath: cfgPath, lists: map[string]*list{}, log: log,
		Who: func(netip.Addr) string { return "" }, Self: func() string { return "" },
		Notify: func(ListConfig, string) {}, Addr: func(string) (netip.Addr, bool) { return netip.Addr{}, false }}
	for _, lc := range cfg.Lists {
		st, err := store.Load(cfg.Data + "/" + lc.Name + ".jsonl")
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", lc.Name, err)
		}
		s.lists[lc.Name] = &list{conf: lc, store: st}
	}
	return s, nil
}

type ctxKey struct{}

// where a request arrived: a mesh's address, or loopback.
type where struct {
	mesh  string
	local bool
}

// ConnContext marks connections accepted on a mesh's address with its label;
// the server for loopback marks them local.
func ConnContext(mesh string, local bool) func(context.Context, net.Conn) context.Context {
	return func(ctx context.Context, _ net.Conn) context.Context {
		return context.WithValue(ctx, ctxKey{}, where{mesh: mesh, local: local})
	}
}

func whereOf(r *http.Request) where {
	w, _ := r.Context().Value(ctxKey{}).(where)
	return w
}

// caller is who is asking: on loopback, this machine (or the session its
// tools say they are); from the mesh, the name the mesh gives the address.
func (s *Server) caller(r *http.Request) string {
	if whereOf(r).local {
		if as := strings.TrimSpace(r.Header.Get(AsHeader)); as != "" {
			return as
		}
		return s.Self()
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	return s.Who(a)
}

// visible is the list, if the request may see it.
func (s *Server) visible(r *http.Request, name string) (*list, bool) {
	s.mu.Lock()
	l, ok := s.lists[name]
	s.mu.Unlock()
	if !ok {
		return nil, false
	}
	w := whereOf(r)
	return l, w.local || w.mesh == l.conf.Mesh
}

// Handler is the API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/whoami", s.whoami)
	mux.HandleFunc("GET /v1/lists", s.getLists)
	mux.HandleFunc("GET /v1/lists/{list}", s.withList(s.getList))
	mux.HandleFunc("GET /v1/lists/{list}/changes", s.withList(s.changes))
	mux.HandleFunc("GET /v1/lists/{list}/pair", s.withList(s.pair))
	mux.HandleFunc("POST /v1/lists/{list}/items", s.withWriter(s.add))
	mux.HandleFunc("PATCH /v1/lists/{list}/items/{id}", s.withWriter(s.update))
	mux.HandleFunc("POST /v1/lists/{list}/items/{id}/comments", s.withWriter(s.comment))
	mux.HandleFunc("POST /v1/lists/{list}/items/{id}/ask", s.withWriter(s.ask))
	mux.HandleFunc("POST /v1/lists/{list}/items/{id}/help", s.withWriter(s.help))
	mux.HandleFunc("POST /v1/lists/{list}/suggest", s.withList(s.suggest))
	mux.HandleFunc("POST /v1/lists/{list}/allow", s.withWriter(s.allow))
	mux.HandleFunc("POST /v1/lists/{list}/ignore", s.withWriter(s.ignore))
	return mux
}

type listHandler func(w http.ResponseWriter, r *http.Request, l *list, by string)

func (s *Server) withList(h listHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l, ok := s.visible(r, r.PathValue("list"))
		if !ok {
			fail(w, http.StatusNotFound, errNoList)
			return
		}
		h(w, r, l, s.caller(r))
	}
}

// withWriter is withList for changes: from a caller the mesh names.
func (s *Server) withWriter(h listHandler) http.HandlerFunc {
	return s.withList(func(w http.ResponseWriter, r *http.Request, l *list, by string) {
		if by == "" {
			fail(w, http.StatusForbidden, errors.New("the mesh does not know who you are"))
			return
		}
		h(w, r, l, by)
	})
}

type listInfo struct {
	Name    string         `json:"name"`
	Title   string         `json:"title"`
	Mesh    string         `json:"mesh"`
	Session string         `json:"session"`
	Seq     int64          `json:"seq"`
	Counts  map[string]int `json:"counts"`
}

func info(l *list) listInfo {
	counts := map[string]int{}
	for _, it := range l.store.Items() {
		counts[it.State]++
	}
	title := l.conf.Title
	if title == "" {
		title = l.conf.Name
	}
	return listInfo{Name: l.conf.Name, Title: title, Mesh: l.conf.Mesh, Session: l.conf.Session,
		Seq: l.store.Seq(), Counts: counts}
}

// whoami is what this service calls the caller, and whether it may write:
// an app tells "you" from somebody else by it, and says up front that a
// device the mesh does not know can only read.
func (s *Server) whoami(w http.ResponseWriter, r *http.Request) {
	name := s.caller(r)
	wh := whereOf(r)
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "mesh": wh.mesh, "local": wh.local,
		"can_write": name != "", "machine": s.Self()})
}

func (s *Server) getLists(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	names := make([]string, 0, len(s.lists))
	for n := range s.lists {
		names = append(names, n)
	}
	s.mu.Unlock()
	out := []listInfo{}
	for _, n := range names {
		if l, ok := s.visible(r, n); ok {
			out = append(out, info(l))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"lists": out, "machine": s.Self()})
}

func (s *Server) getList(w http.ResponseWriter, r *http.Request, l *list, _ string) {
	var states []string
	if q := r.URL.Query().Get("states"); q != "" {
		states = strings.Split(q, ",")
	}
	s.mu.Lock()
	asks := append([]Ask(nil), l.asks...)
	allowed := append([]string(nil), l.conf.SuggestFrom...)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"list": info(l), "items": l.store.Items(states...),
		"asks": asks, "suggest_from": allowed})
}

// changes is what happened after since. When the caller's since is not one
// this list can continue from — ahead of it, as after the log was replaced,
// or behind what it still keeps, once old changes are compacted away — the
// answer is {"reset": true}: read the whole list again. A phone that was off
// for a week must never miss changes silently.
func (s *Server) changes(w http.ResponseWriter, r *http.Request, l *list, _ string) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	ev, seq := l.store.Since(since)
	if since > seq || (since > 0 && since < l.store.Oldest()-1) {
		writeJSON(w, http.StatusOK, map[string]any{"seq": seq, "reset": true, "events": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"seq": seq, "events": ev})
}

// pair is what a phone scans to find the list: a foray:// link with this
// machine's address on the list's mesh, and the QR of it as rows of 0 and 1
// for an app to draw. Nothing secret in it — the mesh is the access control,
// and a phone that is not on it can do nothing with the address.
func (s *Server) pair(w http.ResponseWriter, r *http.Request, l *list, _ string) {
	a, ok := s.Addr(l.conf.Mesh)
	if !ok {
		fail(w, http.StatusConflict, fmt.Errorf("this machine is not on the %s mesh", l.conf.Mesh))
		return
	}
	link := "foray://add?address=" + a.String() + "&list=" + l.conf.Name
	code, err := qr.Encode(link, qr.M)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]string, code.Size)
	for y := range rows {
		b := make([]byte, code.Size)
		for x := range b {
			b[x] = '0'
			if code.Black(x, y) {
				b[x] = '1'
			}
		}
		rows[y] = string(b)
	}
	writeJSON(w, http.StatusOK, map[string]any{"link": link, "address": a.String(), "qr": rows})
}

// isSession is whether by is the list's own session, which is not told about
// its own changes.
func isSession(l *list, by string) bool { return l.conf.Session != "" && by == l.conf.Session }

func (s *Server) tell(l *list, by, text string) {
	if l.conf.Session == "" || isSession(l, by) {
		return
	}
	go s.Notify(l.conf, text)
}

func (s *Server) add(w http.ResponseWriter, r *http.Request, l *list, by string) {
	var n store.New
	if !decode(w, r, &n) {
		return
	}
	// Only /suggest says who suggested something; from here, nobody did.
	n.SuggestedBy = ""
	it, err := l.store.Add(by, n)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.tell(l, by, fmt.Sprintf("[foray %s] %s added %q (item %s). Triage it.", l.conf.Name, by, it.Title, it.ID))
	writeJSON(w, http.StatusCreated, it)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request, l *list, by string) {
	var p store.Patch
	if !decode(w, r, &p) {
		return
	}
	it, err := l.store.Update(by, r.PathValue("id"), p)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) comment(w http.ResponseWriter, r *http.Request, l *list, by string) {
	var c struct{ Text string }
	if !decode(w, r, &c) {
		return
	}
	it, err := l.store.Comment(by, r.PathValue("id"), c.Text)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.tell(l, by, fmt.Sprintf("[foray %s] %s on %q (item %s): %s", l.conf.Name, by, it.Title, it.ID, strings.TrimSpace(c.Text)))
	writeJSON(w, http.StatusOK, it)
}

// ask is a person asking the session to get help with an item: it is
// recorded on the item and handed to the session, which picks the agent.
func (s *Server) ask(w http.ResponseWriter, r *http.Request, l *list, by string) {
	var a struct{ Kind, Text string }
	if !decode(w, r, &a) {
		return
	}
	if l.conf.Session == "" {
		fail(w, http.StatusConflict, errors.New("this list has no session to ask"))
		return
	}
	kind := strings.TrimSpace(a.Kind)
	if kind == "" {
		kind = "help"
	}
	text := "asks for " + kind
	if t := strings.TrimSpace(a.Text); t != "" {
		text += ": " + t
	}
	it, err := l.store.Comment(by, r.PathValue("id"), text)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.tell(l, by, fmt.Sprintf("[foray %s] %s %s — on %q (item %s). Get it done: ask the agent that fits, record it with record_help.",
		l.conf.Name, by, text, it.Title, it.ID))
	writeJSON(w, http.StatusOK, it)
}

// help records a help request's progress: the session's tools, on loopback.
func (s *Server) help(w http.ResponseWriter, r *http.Request, l *list, by string) {
	if !whereOf(r).local {
		fail(w, http.StatusForbidden, errors.New("help is recorded by the list's session"))
		return
	}
	var h store.Help
	if !decode(w, r, &h) {
		return
	}
	it, err := l.store.SetHelp(by, r.PathValue("id"), h)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// sender is who a suggestion comes from: MACHINE/SESSION, the machine as the
// mesh names it and the session as its agent says.
func (s *Server) sender(r *http.Request) string {
	machine := s.caller(r)
	if whereOf(r).local {
		machine = s.Self()
	}
	machine, _, _ = strings.Cut(machine, ".")
	session := strings.TrimSpace(r.Header.Get(FromSessionHeader))
	if machine == "" || session == "" {
		return ""
	}
	return machine + "/" + session
}

func (s *Server) suggest(w http.ResponseWriter, r *http.Request, l *list, _ string) {
	var n struct{ Title, Notes, Why string }
	if !decode(w, r, &n) {
		return
	}
	from := s.sender(r)
	if from == "" {
		fail(w, http.StatusBadRequest, errors.New("suggestions come from an agent's session: say which ("+FromSessionHeader+")"))
		return
	}
	s.mu.Lock()
	allowed := l.conf.allows(from)
	if !allowed {
		kept := []Ask{{From: from, Title: strings.TrimSpace(n.Title), At: time.Now().UTC()}}
		for _, a := range l.asks {
			if a.From != from && len(kept) < 10 {
				kept = append(kept, a)
			}
		}
		l.asks = kept
	}
	s.mu.Unlock()
	if !allowed {
		fail(w, http.StatusForbidden, fmt.Errorf("%s may not suggest items to %s: a person on its mesh can allow it in Foray",
			from, l.conf.Name))
		return
	}
	notes := strings.TrimSpace(n.Notes)
	if why := strings.TrimSpace(n.Why); why != "" {
		if notes != "" {
			notes += "\n\n"
		}
		notes += "Why: " + why
	}
	it, err := l.store.Add(from, store.New{Title: n.Title, Notes: notes, SuggestedBy: from})
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.tell(l, from, fmt.Sprintf("[foray %s] %s suggests %q (item %s, state suggested). Decide: accept it into the inbox, fold it into an item it repeats, or drop it.",
		l.conf.Name, from, it.Title, it.ID))
	writeJSON(w, http.StatusCreated, it)
}

func (s *Server) allow(w http.ResponseWriter, r *http.Request, l *list, by string) {
	var a struct{ From string }
	if !decode(w, r, &a) {
		return
	}
	from := strings.TrimSpace(a.From)
	if !validSender(from) {
		fail(w, http.StatusBadRequest, fmt.Errorf("%q: want MACHINE/SESSION or MACHINE/*", from))
		return
	}
	s.mu.Lock()
	if !l.conf.allows(from) {
		l.conf.SuggestFrom = append(l.conf.SuggestFrom, from)
	}
	s.dropAsk(l, from)
	for i := range s.cfg.Lists {
		if s.cfg.Lists[i].Name == l.conf.Name {
			s.cfg.Lists[i].SuggestFrom = append([]string(nil), l.conf.SuggestFrom...)
		}
	}
	err := s.cfg.Save(s.cfgPath)
	s.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("suggestions allowed", "list", l.conf.Name, "from", from, "by", by)
	writeJSON(w, http.StatusOK, map[string]any{"suggest_from": l.conf.SuggestFrom})
}

func (s *Server) ignore(w http.ResponseWriter, r *http.Request, l *list, _ string) {
	var a struct{ From string }
	if !decode(w, r, &a) {
		return
	}
	s.mu.Lock()
	s.dropAsk(l, strings.TrimSpace(a.From))
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// dropAsk forgets a refused sender's request. Caller holds s.mu.
func (s *Server) dropAsk(l *list, from string) {
	kept := l.asks[:0]
	for _, a := range l.asks {
		if a.From != from {
			kept = append(kept, a)
		}
	}
	l.asks = kept
}

// AgentNotifier posts to the list's session on shrooms-agent, at this
// machine's address on the list's mesh.
func AgentNotifier(addr func(mesh string) (netip.Addr, bool), port int, log *slog.Logger) func(ListConfig, string) {
	client := &http.Client{Timeout: 30 * time.Second}
	return func(l ListConfig, text string) {
		a, ok := addr(l.Mesh)
		if !ok {
			log.Warn("not on the list's mesh; its session was not told", "list", l.Name, "mesh", l.Mesh)
			return
		}
		body, _ := json.Marshal(map[string]string{"text": text})
		u := fmt.Sprintf("http://%s/v1/sessions/%s/messages", netip.AddrPortFrom(a, uint16(port)), l.Session)
		resp, err := client.Post(u, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Warn("could not tell the list's session", "list", l.Name, "session", l.Session, "err", err)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			log.Warn("the list's session refused a note", "list", l.Name, "session", l.Session, "status", resp.Status)
		}
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("unreadable request: %w", err))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
