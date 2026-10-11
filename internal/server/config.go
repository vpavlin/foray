package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config is ~/.config/foray/config.json.
type Config struct {
	// Port Foray serves on, on each mesh address and on loopback.
	Port int `json:"port"`
	// AgentPort is shrooms-agent's, for telling a list's session things.
	AgentPort int `json:"agent_port"`
	// Socket is the shrooms daemon's control socket.
	Socket string `json:"socket"`
	// Data is where the lists' logs are kept.
	Data  string       `json:"data"`
	Lists []ListConfig `json:"lists"`
}

// ListConfig is one list.
type ListConfig struct {
	Name  string `json:"name"`  // "home": its id, and its file
	Title string `json:"title"` // "Home"
	// Mesh is the one mesh the list exists on, by this machine's label.
	Mesh string `json:"mesh"`
	// Session is the shrooms-agent session that looks after it; empty for a
	// list with no assistant.
	Session string `json:"session,omitempty"`
	// SuggestFrom is who may suggest items: MACHINE/SESSION, or MACHINE/*.
	SuggestFrom []string `json:"suggest_from,omitempty"`
}

var listName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// LoadConfig reads the config, filling in what was left out.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.Port == 0 {
		c.Port = 7389
	}
	if c.AgentPort == 0 {
		c.AgentPort = 7387
	}
	if c.Socket == "" {
		c.Socket = "/run/shrooms/shrooms.sock"
	}
	if c.Data == "" {
		home, _ := os.UserHomeDir()
		c.Data = filepath.Join(home, ".local", "share", "foray")
	}
	seen := map[string]bool{}
	for _, l := range c.Lists {
		if !listName.MatchString(l.Name) {
			return nil, fmt.Errorf("list %q: a name is lowercase letters, digits, - and _", l.Name)
		}
		if seen[l.Name] {
			return nil, fmt.Errorf("list %q twice", l.Name)
		}
		seen[l.Name] = true
		if l.Mesh == "" {
			return nil, fmt.Errorf("list %q: which mesh is it on?", l.Name)
		}
		for _, f := range l.SuggestFrom {
			if !validSender(f) {
				return nil, fmt.Errorf("list %q: suggest_from %q: want MACHINE/SESSION or MACHINE/*", l.Name, f)
			}
		}
	}
	return &c, nil
}

// Save writes the config back, atomically.
func (c *Config) Save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func validSender(s string) bool {
	machine, session, ok := strings.Cut(strings.TrimSpace(s), "/")
	return ok && machine != "" && session != "" && !strings.ContainsAny(s, " \t\n")
}

// allows says whether sender, MACHINE/SESSION, may suggest to the list.
func (l ListConfig) allows(sender string) bool {
	machine, _, _ := strings.Cut(sender, "/")
	for _, a := range l.SuggestFrom {
		if a == sender || a == machine+"/*" {
			return true
		}
	}
	return false
}

var errNoList = errors.New("no such list")
