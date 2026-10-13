# Foray

A shared to-do list on your own machines, with an assistant that does the
legwork. The people on a [Shrooms](https://github.com/vpavlin/shrooms) mesh
share it — a household, or a small team — and each list has an agent session
that triages new items, researches the ones that need it (dates, prices, the
links to act on, the sources), and asks other agents for bigger jobs.

No account and no cloud: a list lives on one machine on your mesh, and the
mesh is the access control. A device on the mesh can open the list; any other
cannot see it at all.

## What is in it

| Part | Where | What |
|---|---|---|
| The service | `cmd/foray`, `internal/` | Go. Serves each list on its machine's mesh address (port 7389), keeps it as an append-only log, tells the list's session what happened, and gates what agents may do |
| The session's tools | `internal/mcp`, `browser/` | An MCP server for the list (`foray mcp`) and a browser on Camoufox (`web_search`, `read_page`) |
| The session | `session/AGENTS.md` | Its instructions: research rather than ask, summaries with links and sources, never buy or send anything |
| Basecamp | `basecamp/core`, `basecamp/view` | `foray_core` (reaches the lists; opens links) and the `foray` view |
| Android | `mobile/` | React Native (Expo) app, `xyz.vpavlin.foray` |

## What it does

- **Items** with due dates, who, tags, notes, and a **summary** the assistant
  keeps: the current state in markdown, with links to act on and sources;
  people can edit it.
- **Help**: analyze, find out, compare, draft, plan — the assistant looks it
  up with its own browser, or asks one of the list's **helpers** (agents on
  the mesh the people named), and the answer lands on the item.
- **About us**: where the list's people live, the language, the currency,
  anything worth knowing, and the helpers — so answers fit them.
- **Needs you**: the assistant's open questions come first; sort and group
  by due date, tag or person.
- **Several lists** per machine (home, office), each on one mesh, with its
  own assistant and model.
- **References**: every item has an address on the mesh
  (`/v1/lists/{list}/items/{id}`) an agent can be handed to read and change it.

The assistant runs in a cage (a container that sees its list and nothing
else), only people change its context and helpers, and it never buys, books
or sends anything: it prepares, a person acts.

## Running it

You need a [Shrooms](https://github.com/vpavlin/shrooms) mesh, and on it a
Linux machine (systemd) that is always on, with
[Shrooms Agents](https://shrooms.vpavlin.xyz/agents-start.html), pi with a
model provider, Go, `python3` and `curl`; podman for the assistant's cage is
recommended. Then, from a checkout on that machine:

```
scripts/setup.sh --list home --mesh home --title Home
```

`--mesh` is the machine's label for the mesh. The details, and the apps, are
in [docs/setup.md](docs/setup.md).

The API is in [docs/api.md](docs/api.md), the design and its reasons in
[docs/design.md](docs/design.md).

## Status

Young, and used daily by its author for a home list and an office list. The
Basecamp modules and the Android app are published to the author's own
repositories, not yet to public ones: build them from `basecamp/` and
`mobile/`.

## License

Dual-licensed under [MIT](LICENSE-MIT) or [Apache 2.0](LICENSE-APACHE-v2), at
your option.
