# Setting up Foray

## On the machine that keeps the lists (proteus)

Prerequisites, once:

1. **The machine is on the list's mesh.** For the home list, proteus joins
   the home mesh: `sudo shrooms invite` on the laptop (the home mesh's admin),
   `sudo shrooms join TOKEN --mesh home` on proteus, then a restart of its
   shrooms daemon. Until then the list is on the office mesh (`--mesh default`,
   proteus's label for it).
2. **Foray can read the daemon's socket.** The daemon sets the socket's group
   from `socket_group` in its config; on proteus, whose daemon runs in podman,
   that is a numeric gid (`groupadd --system shrooms`, `socket_group = "987"`,
   `usermod -aG shrooms vpavlin`, restart the daemon). Then restart the user's
   systemd (`sudo systemctl restart user@1000`) so shrooms-agent has the group
   too — without it, it cannot tell its meshes apart, and a session limited to
   one is shown on none. The Foray service takes the group itself (`sg`).
3. **The firewall lets the port in on the mesh address**, as it does
   shrooms-agent's 7387 (setup.sh prints the rule when firewalld is on).
4. **pi has the model.** `~/.pi/agent/models.json` gets a `venice` provider
   with its key (proteus has it), with `deepseek-v4-1-flash`.
5. **A cage for the session** (recommended): podman installed and
   shrooms-agent offering cages (the default), so `foray-home` runs in a
   rootless container that has the list and nothing of the home directory;
   setup.sh asks for one whenever podman is there. The first start builds the
   cage image, which takes a few minutes. Without a cage the session runs as
   the user, and anyone on the list's mesh can drive it.

Then, from a checkout:

```
scripts/setup.sh --list home --mesh default --title Home --suggest-from laptop/shrooms
```

It installs the binary and the service, writes the list's config, prepares
`~/foray-home` (the session's `AGENTS.md`, its pi model, the list's socket),
gives pi the list's tools, and creates the `foray-home` session limited to the
home mesh. Run it again any time; it changes only what differs.

## Before anybody else joins the home mesh

Every agent session on a machine that is on the home mesh is visible from it
unless limited. On each such machine — the laptop today, proteus once it
joins — give every session that is not the household's `meshes: [office]`
(Shrooms Agents → a session → meshes). Otherwise a phone on the home mesh
could open them.

## The apps

- **Basecamp**: install `foray_core` and `foray` from the repository; restart
  Basecamp after `foray_core`. It finds lists on the machines of your meshes
  by itself.
- **Android**: Foray from the LAN F-Droid repository; the first time, scan the
  list's QR or type the machine's address.

## For agents

An agent session on a machine allowed to suggest:

```
foray suggest --list home --to proteus.office.mesh "renew the domain" --why "it lapses on the 20th"
```

It lands as a suggestion; the list's session decides on it.
