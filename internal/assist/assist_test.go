package assist

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// The folder gets what setup.sh gives one, takes the machine's model and
// search region from an assistant already here, and links its browser
// rather than copying 2.8 GB.
func TestFolderIsAsSetupMakesIt(t *testing.T) {
	home := t.TempDir()
	home, _ = filepath.EvalSymlinks(home)
	old := filepath.Join(home, "foray-home")
	os.MkdirAll(filepath.Join(old, ".pi"), 0o700)
	os.MkdirAll(filepath.Join(old, ".browser", "venv"), 0o700)
	os.WriteFile(filepath.Join(old, ".pi", "settings.json"), []byte(`{"defaultProvider":"venice","defaultModel":"claude-haiku-5-5"}`), 0o644)
	os.WriteFile(filepath.Join(old, ".pi", "mcp.json"), []byte(`{"mcpServers":{"foray-browser":{"args":["--region","cz-cs"]}}}`), 0o644)
	os.WriteFile(filepath.Join(old, ".browser", "ready"), nil, 0o644)
	os.WriteFile(filepath.Join(old, ".browser", "venv", "big"), []byte("camoufox"), 0o644)
	exe := filepath.Join(home, "foray-bin")
	os.WriteFile(exe, []byte("#!binary"), 0o755)

	s := &Setup{Home: home, Exe: exe, AgentsMD: []byte("# assistant"), BrowserPy: []byte("py"), BrowserSh: []byte("#!sh")}
	if err := s.Folder("house"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "foray-house")
	for p, want := range map[string]string{"AGENTS.md": "# assistant", "bin/foray": "#!binary", "bin/foray-browser": "#!sh",
		"browser/foray_browser.py": "py", ".pi/settings.json": `"claude-haiku-5-5"`} {
		b, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil || !strings.Contains(string(b), want) {
			t.Errorf("%s: %q %v", p, b, err)
		}
	}
	if fi, _ := os.Stat(filepath.Join(dir, "bin", "foray")); fi == nil || fi.Mode()&0o100 == 0 {
		t.Error("the tools are not executable")
	}
	var mcp struct {
		McpServers map[string]struct {
			Command string
			Args    []string
		} `json:"mcpServers"`
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".pi", "mcp.json"))
	json.Unmarshal(b, &mcp)
	if a := mcp.McpServers["foray-house"].Args; strings.Join(a, " ") != "mcp --list house --socket "+filepath.Join(dir, ".foray.sock") {
		t.Errorf("tools: %v", a)
	}
	if a := mcp.McpServers["foray-browser"].Args; strings.Join(a, " ") != "--region cz-cs" {
		t.Errorf("browser region: %v", a)
	}
	var trust map[string]bool
	b, _ = os.ReadFile(filepath.Join(home, ".pi", "agent", "trust.json"))
	if json.Unmarshal(b, &trust); !trust[dir] {
		t.Errorf("not trusted: %s", b)
	}
	a, _ := os.Stat(filepath.Join(old, ".browser", "venv", "big"))
	c, err := os.Stat(filepath.Join(dir, ".browser", "venv", "big"))
	if err != nil || a.Sys().(*syscall.Stat_t).Ino != c.Sys().(*syscall.Stat_t).Ino {
		t.Errorf("the browser was not linked: %v", err)
	}
	// Again: nothing breaks, the model stays.
	if err := s.Folder("house"); err != nil {
		t.Fatal(err)
	}
}

// Start makes the session caged and limits it; a session already there is
// only limited.
func TestStartMakesTheSession(t *testing.T) {
	var calls []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(b))
		if r.Method == "GET" {
			w.Write([]byte(`{"sessions":[{"name":"foray-home"}]}`))
		}
	}))
	defer ts.Close()
	c := &http.Client{Transport: &http.Transport{Dial: func(_, _ string) (net.Conn, error) {
		return net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	}}}
	s := &Setup{Home: "/h", Agent: c, Cage: func() bool { return true }}
	if err := s.Start("house", "office"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || !strings.Contains(calls[1], `"cage":{"image":"localhost/shrooms-workbench:desktop"}`) ||
		!strings.Contains(calls[1], `"harness":"pi"`) || !strings.Contains(calls[2], `PATCH /v1/sessions/foray-house {"meshes":["office"]}`) {
		t.Errorf("calls: %v", calls)
	}
	calls = nil
	if err := s.Start("home", "office"); err != nil || len(calls) != 2 {
		t.Errorf("an existing session: %v %v", err, calls)
	}
}
