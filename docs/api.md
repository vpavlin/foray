# Foray API

`foray serve` listens on port **7389**, on this machine's address on each mesh
that has a list, and on `127.0.0.1`. JSON in and out; errors are
`{"error": "…"}` with a 4xx status.

**Who may do what.** A list exists only on its own mesh: from any other mesh
it is a 404, as if it were not there. Whoever the mesh names (the shrooms
daemon's roster) may read and write it. A caller the mesh cannot name may
read and gets **403** on every write. Loopback is this machine.

**Addresses.** The service listens on each mesh's **IPv6** overlay address
(and loopback), not on the 198.18/198.19 IPv4 aliases: use the overlay
address from the peer list.

**Finding lists.** An app probes `GET /v1/lists` on the peers of each mesh the
device is on (the overlay addresses in the shrooms status), the way the agents
apps find agents, and shows what it finds grouped by mesh.

## Reading

### `GET /v1/whoami`
What the service calls the caller — how an app tells "you" from somebody else
in `added_by` and comment `by`, and knows up front whether it can write:
```json
{"name": "phone.home", "mesh": "home", "local": false, "can_write": true, "machine": "server"}
```

### `GET /v1/lists`
```json
{"machine": "server",
 "lists": [{"name": "home", "title": "Home", "mesh": "home", "session": "foray-home",   // "" when the list has no assistant
            "seq": 41, "counts": {"open": 7, "waiting": 2, "done": 30, "suggested": 1}}]}
```

### `GET /v1/lists/{list}[?states=open,waiting]`
```json
{"list": { …as above… },
 "items": [ Item, … ],                        // newest change first
 "asks": [{"from": "k11/research", "title": "…", "at": "…"}],   // refused suggesters
 "suggest_from": ["laptop/shrooms"]}
```

### `GET /v1/lists/{list}/changes?since=SEQ`
What changed after `seq`, oldest first — poll this to stay current:
```json
{"seq": 43, "events": [{"seq": 42, "at": "…", "by": "phone.home", "op": "add", "item": Item}, …]}
```
`item` is the whole item after the change; the latest event for an id is its
state. `op` is `add`, `update`, `comment` or `help`.

When `since` is not one the list can continue from — ahead of it (the log was
replaced) or behind what it still keeps (old changes compacted away) — the
answer is `{"seq": N, "reset": true, "events": []}`: read the whole list again
and carry on from `N`.

### `GET /v1/lists/{list}/context`, `PUT /v1/lists/{list}/context`
What the household tells its session about itself, so triage and help fit
them:

```json
{"place": "Brno, Czechia", "language": "English", "currency": "CZK",
 "notes": "Car: 2018 Golf, tyres 205/55 R16.\nWe shop at the market on Saturdays.",
 "helpers": [{"agent": "k11/research", "for": "prices, shops"}],
 "by": "phone.home", "updated": "2026-10-11T14:02:00Z"}
```

`helpers` are the agents the session may ask for help (`MACHINE/SESSION`, up
to 20, each with what it is for).

`PUT` replaces it (place, language and currency a line each, notes up to
4000 characters) and tells the session. New helpers are set to take the
session's tasks even though it is caged; the answer says how that went, per
helper, in `trust`. Only people change it: the session
reads it with `get_context` and gets a 403 if it tries to write it. It is
also in `GET /v1/lists/{list}` as `context`. A change to it moves the list's
`seq` but is not one of the `changes`: those are items.

### `GET /v1/lists/{list}/items/{id}`
One item, with `list` (the list's info) and `api` (how to read, update and
comment on it, in words). This is what "copy reference" in the apps hands
over: `http://[address]:7389/v1/lists/{list}/items/{id}`, so an agent given it
can read the item and change it — as its machine, from the list's mesh.

### `GET /v1/lists/{list}/pair`
What a phone scans to find the list: `{"link": "foray://add?address=fd…&list=home",
"address": "fd…", "qr": ["1110…", …]}` — the QR as rows of 0 and 1 for the app
to draw. 409 when this machine is not on the list's mesh.

## Item

```json
{"id": "2xckcz", "title": "buy winter tires", "notes": "",
 "summary": "…", "summary_by": "foray-home",   // markdown, up to 8000 characters: what is known, with links
 "state": "open",                      // suggested | inbox | open | waiting | done | dropped
 "due": "2026-11-01",                  // or "2026-11-01T09:00:00+01:00", or absent
 "who": "Alex", "tags": ["car"],
 "added_by": "phone.home",             // a device, or an agent session
 "suggested_by": "laptop/shrooms",     // only on agents' suggestions
 "created": "…", "updated": "…",
 "comments": [{"by": "foray-home", "at": "…", "text": "Which car?"}],
 "help": [{"task": "k11/research:7", "worker": "k11/research", "kind": "find out",
           "state": "completed", "ask": "garages open Saturday", "answer": "…", "updated": "…"}],
 "calendar": ""}                       // the Scala event, phase 3
```

States, for the apps:
- **suggested** — an agent suggested it; the list's session decides. Show apart, or not at all.
- **inbox** — accepted, waiting for a person to take it on.
- **open**, **waiting** (on someone or something), **done**, **dropped**.

## Writing

| | Body | |
|---|---|---|
| `POST /v1/lists/{list}/items` | `{title, notes?, due?, who?, tags?, state?}` | 201, the item. State defaults to open. The session is told. |
| `PATCH /v1/lists/{list}/items/{id}` | any of `{title, notes, summary, state, due, who, tags}` | the item. A field left out is unchanged; `""` clears `notes`, `due` and `who`, `[]` clears `tags`; `title` cannot be emptied. |
| `POST /v1/lists/{list}/items/{id}/comments` | `{text}` | the item. The session is told (answers to its questions arrive this way). |
| `POST /v1/lists/{list}/items/{id}/ask` | `{kind, text?, to?}` — kind: `analyze`, `find out`, `compare`, `draft`, `plan`; `to`: one of the list's helpers | the item. Asks the list's session to get help (`analyze`: it decides what would help); recorded as a comment. 400 if `to` is not a helper, 409 if the list has no session. |
| `POST /v1/lists/{list}/allow` | `{from}` — `MACHINE/SESSION` or `MACHINE/*` | `{suggest_from}`. Lets a refused suggester suggest |
| `POST /v1/lists/{list}/ignore` | `{from}` | 204, forgets its request |

Agents use `POST /v1/lists/{list}/suggest` (`{title, notes?, why?}`, with
`X-Shrooms-From-Session`), through `foray suggest`. The list's session
records help through `POST …/items/{id}/help`, on loopback only.

## What an app should show

- Lists by mesh; in a list, open first (due soonest), then waiting, inbox,
  and done folded away. Suggested items only behind a count, if at all.
- An item: title, notes, due, who, tags; its comments as a conversation (the
  session's questions are comments); its help requests with state and
  answer; "ask for help" with its kinds.
- "Wants to suggest items" for each `asks` entry: allow, or ignore.
- Notifications: something due today, the session asked something (a comment
  by the list's session), help came back (a help entry turned completed).
