# Foray API

`foray serve` listens on port **7389**, on this machine's address on each mesh
that has a list, and on `127.0.0.1`. JSON in and out; errors are
`{"error": "…"}` with a 4xx status.

**Who may do what.** A list exists only on its own mesh: from any other mesh
it is a 404, as if it were not there. Whoever the mesh names (the shrooms
daemon's roster) may read and write it. A caller the mesh cannot name may
read and gets **403** on every write. Loopback is this machine.

**Finding lists.** An app probes `GET /v1/lists` on the peers of each mesh the
device is on (the overlay addresses in the shrooms status), the way the agents
apps find agents, and shows what it finds grouped by mesh.

## Reading

### `GET /v1/lists`
```json
{"machine": "jimmy-crib",
 "lists": [{"name": "home", "title": "Home", "mesh": "home", "session": "foray-home",
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

## Item

```json
{"id": "2xckcz", "title": "buy winter tires", "notes": "",
 "state": "open",                      // suggested | inbox | open | waiting | done | dropped
 "due": "2026-11-01",                  // or "2026-11-01T09:00:00+01:00", or absent
 "who": "Václav", "tags": ["car"],
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
| `PATCH /v1/lists/{list}/items/{id}` | any of `{title, notes, state, due, who, tags}` | the item |
| `POST /v1/lists/{list}/items/{id}/comments` | `{text}` | the item. The session is told (answers to its questions arrive this way). |
| `POST /v1/lists/{list}/items/{id}/ask` | `{kind, text?}` — kind: `find out`, `compare`, `draft`, `plan` | the item. Asks the list's session to get an agent's help; recorded as a comment. 409 if the list has no session. |
| `POST /v1/lists/{list}/allow` | `{from}` — `MACHINE/SESSION` or `MACHINE/*` | lets a refused suggester suggest |
| `POST /v1/lists/{list}/ignore` | `{from}` | 204, forgets its request |

Agents use `POST /v1/lists/{list}/suggest` (`{title, notes?, why?}`, with
`X-Shrooms-From-Session`), through `foray suggest`. The list's session
records help through `POST …/items/{id}/help`, on loopback only.

## What an app should show

- Lists by mesh; in a list, open first (due soonest), then waiting, inbox,
  and done folded away. Suggested items only behind a count, if at all.
- An item: title, notes, due, who, tags; its comments as a conversation (the
  session's questions are comments); its help requests with state and
  answer; "ask for help" with the four kinds.
- "Wants to suggest items" for each `asks` entry: allow, or ignore.
- Notifications: something due today, the session asked something (a comment
  by the list's session), help came back (a help entry turned completed).
