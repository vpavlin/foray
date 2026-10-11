# Setting up Foray

## On the machine that keeps the lists (jimmy-crib)

Prerequisites, once:

1. **The machine is on the list's mesh.** For the home list, jimmy-crib joins
   the home mesh: `sudo shrooms invite` on the laptop (the home mesh's admin),
   `sudo shrooms join TOKEN --mesh home` on jimmy-crib, then a restart of its
   shrooms daemon — which carries Jimmy and the publisher, so at a quiet time.
2. **pi has the model.** `~/.pi/agent/models.json` gets a `venice` provider
   with its key (as on pi5, proteus and scribe), with `deepseek-v4-1-flash`.
3. **A cage for the session** (recommended): podman installed and
   shrooms-agent started with cages, so `foray-home` runs in a rootless
   container that has the list and nothing of the home directory. Without it,
   the session runs as the user, and anyone on the home mesh can drive it.

Then, from a checkout:

```
scripts/setup.sh --list home --mesh home --title Home --suggest-from laptop/shrooms
```

It installs the binary and the service, writes the list's config, prepares
`~/foray-home` (the session's `AGENTS.md`, its pi model, the list's socket),
gives pi the list's tools, and creates the `foray-home` session limited to the
home mesh. Run it again any time; it changes only what differs.

## Before anybody else joins the home mesh

Every agent session on a machine that is on the home mesh is visible from it
unless limited. On each such machine — the laptop today, jimmy-crib once it
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
foray suggest --list home --to jimmy-crib.home.mesh "renew the domain" --why "it lapses on the 20th"
```

It lands as a suggestion; the list's session decides on it.
