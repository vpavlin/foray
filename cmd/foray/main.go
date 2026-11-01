// Command foray is a household's todo list on its own machines.
//
//	foray serve                  the service: lists on this machine's mesh addresses
//	foray mcp --list home        the list's tools, for its session (MCP on stdio)
//	foray suggest --list home --to MACHINE "title" [--why …]
//	                             an agent's suggestion, from inside its session
//	foray add --list home "title"
//	                             add an item from this machine
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vpavlin/foray/internal/mcp"
	"github.com/vpavlin/foray/internal/mesh"
	"github.com/vpavlin/foray/internal/server"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "mcp":
		err = mcpMain(os.Args[2:])
	case "suggest":
		err = suggest(os.Args[2:])
	case "add":
		err = add(os.Args[2:])
	case "version", "--version":
		fmt.Println("foray", version)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "foray:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  foray serve [--config PATH]
  foray mcp --list NAME [--session NAME] [--port 7389]
  foray suggest --list NAME --to MACHINE TITLE [--notes …] [--why …]
  foray add --list NAME TITLE [--due 2006-01-02] [--who …]`)
	os.Exit(2)
}

func defaultConfig() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "foray", "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "foray", "config.json")
}

// flagsFirst lets flags follow the positional argument, as people type them.
func flagsFirst(fs *flag.FlagSet, args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if !strings.Contains(name, "=") {
				if f := fs.Lookup(name); f != nil {
					if _, isBool := f.Value.(interface{ IsBoolFlag() bool }); !isBool && i+1 < len(args) {
						flags = append(flags, args[i+1])
						i++
					}
				}
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(flags, rest...)
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfig(), "the config file")
	fs.Parse(args)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := server.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	// Without the shrooms daemon the lists are served on loopback only, and
	// said so: a daemon still starting, or a development machine. The mesh
	// addresses are picked up below once it answers.
	st, err := mesh.Fetch(cfg.Socket)
	if err != nil {
		log.Warn("no shrooms daemon; serving on loopback only", "err", err)
	}
	names := &mesh.Names{}
	names.Update(st)
	addrs := st.Addresses()
	var addrMu sync.Mutex

	srv, err := server.New(cfg, *cfgPath, log)
	if err != nil {
		return err
	}
	srv.Who = names.Who
	// This machine by its mesh name, or its hostname until the daemon says.
	srv.Self = func() string {
		if n := names.Self(); n != "" {
			return n
		}
		h, _ := os.Hostname()
		return h
	}
	srv.Addr = func(m string) (netip.Addr, bool) {
		addrMu.Lock()
		defer addrMu.Unlock()
		a, ok := addrs[m]
		return a, ok
	}
	srv.Notify = server.AgentNotifier(srv.Addr, cfg.AgentPort, log)
	srv.Trust = server.AgentTrust(names.Find, srv.Addr, cfg.AgentPort)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The meshes the lists are on, and loopback for this machine.
	want := map[string]bool{}
	for _, l := range cfg.Lists {
		want[l.Mesh] = true
	}
	h := srv.Handler()
	var srvMu sync.Mutex
	var servers []*http.Server
	start := func(ln net.Listener, mesh string, local bool) *http.Server {
		hs := &http.Server{Handler: h, ConnContext: server.ConnContext(mesh, local), ReadHeaderTimeout: 10 * time.Second}
		srvMu.Lock()
		servers = append(servers, hs)
		srvMu.Unlock()
		log.Info("serving", "on", ln.Addr().String(), "mesh", mesh)
		go hs.Serve(ln)
		return hs
	}

	// Each list's mesh, on this machine's address there, opened as soon as the
	// daemon reports it and moved if the address changes. Opened only at start
	// before: proteus's nightly reboot on 2026-11-01 started Foray before the
	// shrooms daemon answered, every list was served on loopback all morning,
	// and a phone's new items stayed "only on this device".
	type onMesh struct {
		addr netip.Addr
		hs   *http.Server
	}
	meshSrv := map[string]onMesh{}
	warned := map[string]bool{}
	listen := func(addrs map[string]netip.Addr) (missing bool) {
		// The lists' meshes as they are now: one made through the API may
		// be on a mesh not served at start.
		want = srv.Meshes()
		for m := range want {
			a, ok := addrs[m]
			cur, have := meshSrv[m]
			if have && (!ok || cur.addr != a) {
				log.Info("this machine's address on a list's mesh changed; moving", "mesh", m, "was", cur.addr)
				cur.hs.Close()
				delete(meshSrv, m)
				have = false
			}
			if !ok {
				if !warned[m] {
					log.Warn("this machine is not on a list's mesh yet; that list is served on loopback until it is", "mesh", m)
					warned[m] = true
				}
				missing = true
				continue
			}
			if have {
				continue
			}
			ln, err := net.Listen("tcp", netip.AddrPortFrom(a, uint16(cfg.Port)).String())
			if err != nil {
				log.Warn("cannot listen on a list's mesh yet", "mesh", m, "err", err)
				missing = true
				continue
			}
			meshSrv[m] = onMesh{a, start(ln, m, false)}
			warned[m] = false
		}
		return missing
	}
	missing := listen(addrs)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
	if err != nil {
		return err
	}
	start(ln, "", true)
	// Each list's own socket, in its session's directory, for a caged session.
	for _, l := range cfg.Lists {
		if l.Socket == "" {
			continue
		}
		os.MkdirAll(filepath.Dir(l.Socket), 0o700)
		os.Remove(l.Socket)
		ul, err := net.Listen("unix", l.Socket)
		if err != nil {
			return fmt.Errorf("list %s: socket: %w", l.Name, err)
		}
		os.Chmod(l.Socket, 0o600)
		start(ul, "", true)
	}

	// The roster changes as devices come and go; names follow it, and so do
	// the mesh listeners. Every 10 seconds while a list's mesh is missing,
	// every minute after.
	go func() {
		for {
			wait := time.Minute
			if missing {
				wait = 10 * time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
				if st, err := mesh.Fetch(cfg.Socket); err == nil {
					names.Update(st)
					now := st.Addresses()
					addrMu.Lock()
					addrs = now
					addrMu.Unlock()
					missing = listen(now)
				}
			}
		}
	}()

	<-ctx.Done()
	srvMu.Lock()
	defer srvMu.Unlock()
	for _, hs := range servers {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		hs.Shutdown(sctx)
		cancel()
	}
	return nil
}

func mcpMain(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	list := fs.String("list", "", "the list")
	session := fs.String("session", os.Getenv("SHROOMS_AGENT_SESSION"), "the session the tools work for")
	port := fs.Int("port", 7389, "the Foray service's port")
	sock := fs.String("socket", "", "the list's socket, instead of loopback (a caged session)")
	fs.Parse(args)
	if *list == "" {
		return fmt.Errorf("--list is needed")
	}
	if *session == "" {
		*session = "foray-" + *list
	}
	c := mcp.Client{Base: fmt.Sprintf("http://127.0.0.1:%d", *port), List: *list, Session: *session}
	if *sock != "" {
		c.Base = "http://foray"
		c.HTTP = &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", *sock)
			}}}
	}
	return mcp.Serve(os.Stdin, os.Stdout, c)
}

// suggest is an agent's suggestion, sent to the machine that keeps the list.
// The machine is named by the mesh; the session by its agent, which sets
// SHROOMS_AGENT_SESSION for every session it runs.
func suggest(args []string) error {
	fs := flag.NewFlagSet("suggest", flag.ExitOnError)
	list := fs.String("list", "", "the list")
	to := fs.String("to", "", "the machine that keeps it: a mesh name or address")
	notes := fs.String("notes", "", "")
	why := fs.String("why", "", "why it belongs on the list")
	port := fs.Int("port", 7389, "")
	fs.Parse(flagsFirst(fs, args))
	session := os.Getenv("SHROOMS_AGENT_SESSION")
	if *list == "" || *to == "" || fs.NArg() == 0 {
		return fmt.Errorf("usage: foray suggest --list NAME --to MACHINE TITLE")
	}
	if session == "" {
		return fmt.Errorf("suggestions come from an agent's session (SHROOMS_AGENT_SESSION is not set)")
	}
	host := *to
	if a, err := netip.ParseAddr(host); err == nil && a.Is6() {
		host = "[" + host + "]"
	}
	body, _ := json.Marshal(map[string]string{"title": strings.Join(fs.Args(), " "), "notes": *notes, "why": *why})
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://%s:%d/v1/lists/%s/suggest", host, *port, *list), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(server.FromSessionHeader, session)
	return send(req)
}

func add(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	list := fs.String("list", "", "the list")
	due := fs.String("due", "", "")
	who := fs.String("who", "", "")
	port := fs.Int("port", 7389, "")
	fs.Parse(flagsFirst(fs, args))
	if *list == "" || fs.NArg() == 0 {
		return fmt.Errorf("usage: foray add --list NAME TITLE")
	}
	body, _ := json.Marshal(map[string]string{"title": strings.Join(fs.Args(), " "), "due": *due, "who": *who})
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/v1/lists/%s/items", *port, *list), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return send(req)
}

func send(req *http.Request) error {
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var e struct{ Error string }
		json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("%s", e.Error)
	}
	var it struct{ ID, Title, State string }
	json.Unmarshal(b, &it)
	fmt.Printf("%s · %s · %s\n", it.ID, it.State, it.Title)
	return nil
}
