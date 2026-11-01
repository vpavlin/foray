// Package assist sets up a list's assistant on this machine: what
// scripts/setup.sh does for one, done by the service when somebody asks for
// an assistant from an app (docs/design.md, "Lists and assistants from the apps").
//
// The folder (~/foray-<list>) gets the assistant's instructions, this binary
// as its tools, its browser, its model and pi's trust; then the session is
// made through this machine's shrooms-agent, caged, limited to the list's
// mesh. Safe to run again: what is there is replaced, the browser and the
// model are kept.
package assist

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Setup is what it needs from the machine.
type Setup struct {
	Home string // the user's home
	Exe  string // this binary, copied in as the assistant's tools
	// The assistant's files, from the binary (foray.AgentsMD and the browser).
	AgentsMD, BrowserPy, BrowserSh []byte
	// Agent reaches this machine's shrooms-agent (its socket).
	Agent *http.Client
	// Cage reports whether sessions can run caged here (podman).
	Cage func() bool
}

// Image is the cage the assistant runs in: the workbench and what its
// browser needs.
const Image = "localhost/shrooms-workbench:desktop"

// Dir and Session name a list's assistant.
func (s *Setup) Dir(list string) string { return filepath.Join(s.Home, "foray-"+list) }
func Session(list string) string        { return "foray-" + list }

// Socket is where the service serves the list for its caged assistant: in
// the folder the cage mounts.
func (s *Setup) Socket(list string) string { return filepath.Join(s.Dir(list), ".foray.sock") }

// Folder prepares the assistant's folder.
func (s *Setup) Folder(list string) error {
	dir := s.Dir(list)
	for _, d := range []string{"bin", ".pi", "browser"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			return err
		}
	}
	if err := put(filepath.Join(dir, "AGENTS.md"), s.AgentsMD, 0o644); err != nil {
		return err
	}
	exe, err := os.ReadFile(s.Exe)
	if err != nil {
		return fmt.Errorf("this binary: %w", err)
	}
	// Renamed into place: a running assistant's tools hold the old one open.
	if err := put(filepath.Join(dir, "bin", "foray"), exe, 0o755); err != nil {
		return err
	}
	if err := put(filepath.Join(dir, "bin", "foray-browser"), s.BrowserSh, 0o755); err != nil {
		return err
	}
	if err := put(filepath.Join(dir, "browser", "foray_browser.py"), s.BrowserPy, 0o644); err != nil {
		return err
	}
	if err := s.model(dir); err != nil {
		return err
	}
	if err := s.tools(list, dir); err != nil {
		return err
	}
	if err := s.trust(dir); err != nil {
		return err
	}
	// The browser is 2.8 GB; another list's, linked rather than copied,
	// costs none. Without one it installs itself on first use (foray-browser).
	s.shareBrowser(dir)
	return nil
}

// others are the folders of this machine's other assistants.
func (s *Setup) others(except string) []string {
	m, _ := filepath.Glob(filepath.Join(s.Home, "foray-*"))
	var out []string
	for _, d := range m {
		if d != except {
			if fi, err := os.Stat(filepath.Join(d, ".pi")); err == nil && fi.IsDir() {
				out = append(out, d)
			}
		}
	}
	return out
}

// model keeps the folder's model, else takes this machine's other
// assistants', else setup.sh's default.
func (s *Setup) model(dir string) error {
	p := filepath.Join(dir, ".pi", "settings.json")
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	b := []byte(`{"defaultProvider": "venice", "defaultModel": "deepseek-v4-1-flash"}` + "\n")
	for _, o := range s.others(dir) {
		if ob, err := os.ReadFile(filepath.Join(o, ".pi", "settings.json")); err == nil && json.Valid(ob) {
			b = ob
			break
		}
	}
	return put(p, b, 0o644)
}

var regionRe = regexp.MustCompile(`^[a-z]{2}-[a-z]{2}$`)

// tools writes .pi/mcp.json: the list's tools through its socket, and the
// browser, with the search region another assistant here uses.
func (s *Setup) tools(list, dir string) error {
	region := ""
	for _, o := range s.others(dir) {
		var c struct {
			McpServers map[string]struct{ Args []string } `json:"mcpServers"`
		}
		if b, err := os.ReadFile(filepath.Join(o, ".pi", "mcp.json")); err == nil && json.Unmarshal(b, &c) == nil {
			if a := c.McpServers["foray-browser"].Args; len(a) == 2 && a[0] == "--region" && regionRe.MatchString(a[1]) {
				region = a[1]
				break
			}
		}
	}
	browser := map[string]any{"command": filepath.Join(dir, "bin", "foray-browser"), "exposure": "direct",
		"description": "A browser for the list's session: web_search, read_page."}
	if region != "" {
		browser["args"] = []string{"--region", region}
	}
	c := map[string]any{"mcpServers": map[string]any{
		"foray-browser": browser,
		"foray-" + list: map[string]any{
			"command": filepath.Join(dir, "bin", "foray"),
			"args":    []string{"mcp", "--list", list, "--socket", s.Socket(list)},
			// Direct: ordinary tools, which a small model uses best.
			"exposure":    "direct",
			"description": "This list: only its own session may use these tools."},
	}}
	b, _ := json.MarshalIndent(c, "", "  ")
	return put(filepath.Join(dir, ".pi", "mcp.json"), append(b, '\n'), 0o644)
}

// trust: pi reads a folder's .pi settings only from folders it trusts, and
// in rpc mode never asks.
func (s *Setup) trust(dir string) error {
	p := filepath.Join(s.Home, ".pi", "agent", "trust.json")
	t := map[string]any{}
	if b, err := os.ReadFile(p); err == nil {
		if err := json.Unmarshal(b, &t); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if t[real] == true {
		return nil
	}
	t[real] = true
	b, _ := json.MarshalIndent(t, "", "  ")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return put(p, append(b, '\n'), 0o600)
}

// shareBrowser hard-links another assistant's installed browser into dir.
// Best effort: a browser that installs itself later is only slower.
func (s *Setup) shareBrowser(dir string) {
	dst := filepath.Join(dir, ".browser")
	if _, err := os.Stat(filepath.Join(dst, "ready")); err == nil {
		return
	}
	for _, o := range s.others(dir) {
		src := filepath.Join(o, ".browser")
		if _, err := os.Stat(filepath.Join(src, "ready")); err != nil {
			continue
		}
		os.RemoveAll(dst)
		if err := linkTree(src, dst); err != nil {
			os.RemoveAll(dst)
			continue
		}
		return
	}
}

func linkTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(to, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(l, to)
		case rel == ".lock":
			return nil
		default:
			return os.Link(p, to)
		}
	})
}

// Start makes the assistant's session, if it is not there, and limits it to
// the list's mesh.
func (s *Setup) Start(list, mesh string) error {
	name := Session(list)
	exists, err := s.exists(name)
	if err != nil {
		return err
	}
	if !exists {
		req := map[string]any{"name": name, "dir": s.Dir(list), "harness": "pi", "keep_running": true}
		if s.Cage != nil && s.Cage() {
			req["cage"] = map[string]string{"image": Image}
		}
		if err := s.call("POST", "/v1/sessions", req); err != nil {
			return fmt.Errorf("creating the session: %w", err)
		}
	}
	if err := s.call("PATCH", "/v1/sessions/"+name, map[string]any{"meshes": []string{mesh}}); err != nil {
		return fmt.Errorf("limiting it to mesh %s: %w", mesh, err)
	}
	return nil
}

func (s *Setup) exists(name string) (bool, error) {
	resp, err := s.Agent.Get("http://agent/v1/sessions")
	if err != nil {
		return false, fmt.Errorf("this machine's shrooms-agent: %w", err)
	}
	defer resp.Body.Close()
	var out struct{ Sessions []struct{ Name string } }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	for _, x := range out.Sessions {
		if x.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (s *Setup) call(method, path string, body any) error {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, "http://agent"+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.Agent.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return errors.New(resp.Status + ": " + strings.TrimSpace(string(msg)))
	}
	return nil
}

// put writes a file whole, renamed into place.
func put(path string, b []byte, mode os.FileMode) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// HavePodman is the usual Cage.
func HavePodman() bool { _, err := exec.LookPath("podman"); return err == nil }
