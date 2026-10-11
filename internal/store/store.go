// Package store keeps one list as an append-only log of changes.
//
// Every change is a line of JSON carrying the item as it is after the change,
// so the state is the last line written for each item: no merge logic, nothing
// to get wrong when reading back. The log is also what a later sync between
// devices would replicate (docs/design.md, phase 4) — so it is the format from
// the start rather than a table that would have to become one.
package store

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// States an item moves through.
const (
	// Suggested is an agent's suggestion the list's session has not yet
	// decided on: it accepts it into the inbox, folds it into another item,
	// or drops it. Who suggested it is recorded by the service, never by the
	// session, so a model cannot be talked into misattributing one.
	Suggested = "suggested"
	Inbox     = "inbox"   // accepted, waiting for a person to take it on
	Open      = "open"    // to do
	Waiting   = "waiting" // on somebody or something
	Done      = "done"
	Dropped   = "dropped"
)

var states = map[string]bool{Suggested: true, Inbox: true, Open: true, Waiting: true, Done: true, Dropped: true}

// ValidState says whether s is a state an item can be in.
func ValidState(s string) bool { return states[s] }

// Item is one thing to do.
type Item struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Notes       string    `json:"notes,omitempty"`
	State       string    `json:"state"`
	Due         string    `json:"due,omitempty"` // 2006-01-02, or RFC 3339 with a time
	Who         string    `json:"who,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	AddedBy     string    `json:"added_by,omitempty"`
	SuggestedBy string    `json:"suggested_by,omitempty"` // an agent, MACHINE/SESSION
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
	Comments    []Comment `json:"comments,omitempty"`
	Help        []Help    `json:"help,omitempty"`
	Calendar    string    `json:"calendar,omitempty"` // the Scala event it is mirrored to
}

// Comment is a line of conversation on an item: a question the session asks,
// a person's answer, a note.
type Comment struct {
	By   string    `json:"by"`
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// Help is one request to an agent for an item: find out, compare, draft, plan.
type Help struct {
	Task    string    `json:"task"`   // the A2A task id, MACHINE/SESSION:ID
	Worker  string    `json:"worker"` // MACHINE/SESSION
	Kind    string    `json:"kind"`
	State   string    `json:"state"` // working, input-required, completed, failed, …
	Ask     string    `json:"ask,omitempty"`
	Answer  string    `json:"answer,omitempty"`
	Updated time.Time `json:"updated"`
}

// Event is one line of the log.
type Event struct {
	Seq  int64     `json:"seq"`
	At   time.Time `json:"at"`
	By   string    `json:"by"`
	Op   string    `json:"op"` // add, update, comment, help
	Item Item      `json:"item"`
}

// List is one list: its log on disk and its state in memory.
type List struct {
	mu     sync.Mutex
	path   string
	items  map[string]*Item
	events []Event
	seq    int64
	now    func() time.Time
}

// Load reads a list's log, making it if it does not exist.
func Load(path string) (*List, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	l := &List{path: path, items: map[string]*Item{}, now: time.Now}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	good := 0 // bytes up to the end of the last whole line read
	for line, rest := 1, data; len(rest) > 0; line++ {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			// No newline: a write cut short by a crash, the one damage an
			// append-only file can take. Cut off, or the next write would be
			// appended to it and turn a torn line into a corrupt one.
			break
		}
		raw := rest[:i]
		rest = rest[i+1:]
		if len(bytes.TrimSpace(raw)) > 0 {
			var e Event
			if err := json.Unmarshal(raw, &e); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, line, err)
			}
			l.fold(e)
		}
		good = len(data) - len(rest)
	}
	if good < len(data) {
		if err := os.Truncate(path, int64(good)); err != nil {
			return nil, fmt.Errorf("cutting off a torn last line: %w", err)
		}
	}
	return l, nil
}

func (l *List) fold(e Event) {
	it := e.Item
	l.items[it.ID] = &it
	l.events = append(l.events, e)
	if e.Seq > l.seq {
		l.seq = e.Seq
	}
}

// write appends an event and applies it. Caller holds l.mu.
func (l *List) write(by, op string, it Item) (Item, error) {
	it.Updated = l.now().UTC()
	e := Event{Seq: l.seq + 1, At: it.Updated, By: by, Op: op, Item: it}
	b, err := json.Marshal(e)
	if err != nil {
		return Item{}, err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Item{}, err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return Item{}, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return Item{}, err
	}
	if err := f.Close(); err != nil {
		return Item{}, err
	}
	l.fold(e)
	return it, nil
}

// New is what can be given when adding an item.
type New struct {
	Title       string   `json:"title"`
	Notes       string   `json:"notes"`
	Due         string   `json:"due"`
	Who         string   `json:"who"`
	Tags        []string `json:"tags"`
	State       string   `json:"state"`
	SuggestedBy string   `json:"suggested_by"`
}

// Add adds an item. A person's item is open; one an agent suggested is
// Suggested until the list's session decides on it.
func (l *List) Add(by string, n New) (Item, error) {
	title := strings.TrimSpace(n.Title)
	if title == "" {
		return Item{}, errors.New("an item needs a title")
	}
	if err := checkDue(n.Due); err != nil {
		return Item{}, err
	}
	state := n.State
	switch {
	case n.SuggestedBy != "":
		state = Suggested
	case state == "":
		state = Open
	case !ValidState(state):
		return Item{}, fmt.Errorf("no state called %q", state)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	it := Item{ID: l.newID(), Title: title, Notes: strings.TrimSpace(n.Notes), State: state,
		Due: strings.TrimSpace(n.Due), Who: strings.TrimSpace(n.Who), Tags: cleanTags(n.Tags),
		AddedBy: by, SuggestedBy: n.SuggestedBy, Created: l.now().UTC()}
	return l.write(by, "add", it)
}

// Patch is a change to an item's fields; nil leaves a field as it is.
type Patch struct {
	Title    *string   `json:"title"`
	Notes    *string   `json:"notes"`
	State    *string   `json:"state"`
	Due      *string   `json:"due"`
	Who      *string   `json:"who"`
	Tags     *[]string `json:"tags"`
	Calendar *string   `json:"calendar"`
}

// Update changes an item.
func (l *List) Update(by, id string, p Patch) (Item, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, ok := l.items[id]
	if !ok {
		return Item{}, fmt.Errorf("no item %q", id)
	}
	it := clone(*cur)
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if t == "" {
			return Item{}, errors.New("an item needs a title")
		}
		it.Title = t
	}
	if p.Notes != nil {
		it.Notes = strings.TrimSpace(*p.Notes)
	}
	if p.State != nil {
		if !ValidState(*p.State) {
			return Item{}, fmt.Errorf("no state called %q", *p.State)
		}
		it.State = *p.State
	}
	if p.Due != nil {
		if err := checkDue(*p.Due); err != nil {
			return Item{}, err
		}
		it.Due = strings.TrimSpace(*p.Due)
	}
	if p.Who != nil {
		it.Who = strings.TrimSpace(*p.Who)
	}
	if p.Tags != nil {
		it.Tags = cleanTags(*p.Tags)
	}
	if p.Calendar != nil {
		it.Calendar = *p.Calendar
	}
	return l.write(by, "update", it)
}

// Comment adds a line of conversation to an item.
func (l *List) Comment(by, id, text string) (Item, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Item{}, errors.New("a comment needs text")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, ok := l.items[id]
	if !ok {
		return Item{}, fmt.Errorf("no item %q", id)
	}
	it := clone(*cur)
	it.Comments = append(it.Comments, Comment{By: by, At: l.now().UTC(), Text: text})
	return l.write(by, "comment", it)
}

// SetHelp records a help request, or its progress: one entry per task.
func (l *List) SetHelp(by, id string, h Help) (Item, error) {
	if h.Task == "" {
		return Item{}, errors.New("help needs the task it is")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, ok := l.items[id]
	if !ok {
		return Item{}, fmt.Errorf("no item %q", id)
	}
	it := clone(*cur)
	h.Updated = l.now().UTC()
	found := false
	for i := range it.Help {
		if it.Help[i].Task == h.Task {
			old := it.Help[i]
			if h.Worker == "" {
				h.Worker = old.Worker
			}
			if h.Kind == "" {
				h.Kind = old.Kind
			}
			if h.Ask == "" {
				h.Ask = old.Ask
			}
			if h.Answer == "" {
				h.Answer = old.Answer
			}
			it.Help[i] = h
			found = true
		}
	}
	if !found {
		it.Help = append(it.Help, h)
	}
	return l.write(by, "help", it)
}

// Get is one item.
func (l *List) Get(id string) (Item, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	it, ok := l.items[id]
	if !ok {
		return Item{}, false
	}
	return clone(*it), true
}

// Items is every item, newest change first; states filters when given.
func (l *List) Items(states ...string) []Item {
	want := map[string]bool{}
	for _, s := range states {
		want[s] = true
	}
	l.mu.Lock()
	out := make([]Item, 0, len(l.items))
	for _, it := range l.items {
		if len(want) == 0 || want[it.State] {
			out = append(out, clone(*it))
		}
	}
	l.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// Since is every change after seq, oldest first, and the newest seq: what an
// app polls to stay current without reading the whole list.
func (l *List) Since(seq int64) ([]Event, int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := sort.Search(len(l.events), func(i int) bool { return l.events[i].Seq > seq })
	return append([]Event(nil), l.events[i:]...), l.seq
}

// Oldest is the oldest change still kept: 1 while nothing has been compacted.
func (l *List) Oldest() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) == 0 {
		return l.seq + 1
	}
	return l.events[0].Seq
}

// Seq is the newest change's number.
func (l *List) Seq() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq
}

func (l *List) newID() string {
	const abc = "abcdefghjkmnpqrstuvwxyz23456789"
	for {
		b := make([]byte, 6)
		rand.Read(b)
		for i := range b {
			b[i] = abc[int(b[i])%len(abc)]
		}
		if _, taken := l.items[string(b)]; !taken {
			return string(b)
		}
	}
}

func checkDue(d string) error {
	d = strings.TrimSpace(d)
	if d == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", d); err == nil {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, d); err == nil {
		return nil
	}
	return fmt.Errorf("due %q: want 2006-01-02, or a date and time like 2006-01-02T15:04:05+02:00", d)
}

func cleanTags(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func clone(it Item) Item {
	it.Tags = append([]string(nil), it.Tags...)
	it.Comments = append([]Comment(nil), it.Comments...)
	it.Help = append([]Help(nil), it.Help...)
	return it
}
