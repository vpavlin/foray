# Setting up Foray

## What you need

- **A Shrooms mesh** with the people's phones and computers on it, and one
  Linux machine with systemd that is always on, to keep the lists.
- **On that machine:** [Shrooms Agents](https://shrooms.vpavlin.xyz/agents-start.html)
  (it runs the list's session, on port 7387), pi working for your user, Go
  (to build `foray`), `python3` and `curl`. Podman, for the session's cage, is
  recommended.

A list works with no assistant at all: `--no-session` sets up only the list.

## On the machine that keeps the lists

Prerequisites, once:

1. **The machine is on the list's mesh.** If it is not yet: `sudo shrooms invite`
   on the mesh's admin machine, `sudo shrooms join TOKEN --mesh home` on this
   one, then a restart of its shrooms daemon. `--mesh` in setup.sh is this
   machine's label for the mesh (`default` for the first one it joined).
2. **Foray can read the daemon's socket.** The daemon sets the socket's group
   from `socket_group` in its config. Where the daemon runs in a container,
   that is a numeric gid (`groupadd --system shrooms`, `socket_group = "987"`,
   `usermod -aG shrooms $USER`, restart the daemon). Then restart your user's
   systemd (`sudo systemctl restart user@$(id -u)`) so shrooms-agent has the
   group too — without it, it cannot tell its meshes apart, and a session
   limited to one is shown on none. The Foray service takes the group itself
   (`sg`). Without the socket, Foray serves on loopback only.
3. **The firewall lets the port in on the mesh address**, as it does
   shrooms-agent's 7387 (setup.sh prints the rule when firewalld is on).
4. **pi has the model.** `~/.pi/agent/models.json` has the provider of the
   list's model, with its key. A new list uses `venice/deepseek-v4-1-flash`
   unless you give `--model PROVIDER/MODEL`; setup.sh says when pi has no such
   provider.
5. **A cage for the session** (recommended): podman installed and
   shrooms-agent offering cages (the default), so `foray-home` runs in a
   rootless container that has the list and nothing of the home directory;
   setup.sh asks for one whenever podman is there. The first start builds the
   cage image, which takes a few minutes. Without a cage the session runs as
   the user, and anyone on the list's mesh can drive it.

Then, from a checkout:

```
scripts/setup.sh --list home --mesh home --title Home
```

It installs the binary and the service, writes the list's config, prepares
`~/foray-home` (the session's `AGENTS.md`, its pi model, the list's socket),
gives pi the list's tools, and creates the `foray-home` session limited to the
list's mesh. Run it again any time; it changes only what differs.

The other flags: `--model PROVIDER/MODEL`, `--suggest-from MACHINE/SESSION`
(agents that may suggest items; comma-separated), `--no-session`, `--no-cage`.

## More lists

A machine keeps as many lists as you like: run setup again with another name,
for each its own session, its own context ("about us") and its own browser
(about 2.8 GB):

```
scripts/setup.sh --list office --mesh office --title Office
```

A list is on one mesh, and everybody on that mesh sees it — so two lists for
two groups of people go on two meshes; two lists for the same people can
share one.

## Before anybody else joins the list's mesh

Every agent session on a machine that is on the mesh is visible from it
unless limited. On each such machine, give every session that is not the
list's a `meshes` setting without that mesh (Shrooms Agents → a session →
meshes). Otherwise a phone on the mesh could open them, and drive them.

## The apps

They are not in public repositories yet; build them from this repository.

- **Basecamp**: `basecamp/core` (`foray_core`) and `basecamp/view` (`foray`)
  are Nix flakes on logos-module-builder. Install both, beside Shrooms;
  restart Basecamp after `foray_core`. It finds lists on the machines of your
  meshes by itself.
- **Android**: `mobile/` is an Expo app (`npm install`, `npm run prebuild`,
  then build `android/`). The phone has to be on the mesh, through the Shrooms
  app. The first time, scan the list's QR or type the machine's address.

## For agents

An agent session on a machine allowed to suggest (`--suggest-from`, or
"allow" in the apps):

```
foray suggest --list home --to SERVER "renew the domain" --why "it lapses on the 20th"
```

`SERVER` is the machine that keeps the list, by its mesh name or address. It
lands as a suggestion; the list's session decides on it.
