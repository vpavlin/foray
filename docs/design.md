# Shrooms Todo — design

**Status:** draft for review, 2026-10-11. Nothing built. Working name.

A todo list for a household — "buy XYZ", "schedule the tire change" — that two
people share from their phones and desktops, with an agent session that knows
the whole list, triages it, and asks other agents for help when an item wants
it: find out, compare, draft, plan. Most items will never involve an agent; the
list has to be good with none.

It is its own project. It uses what already exists and changes none of it:
the shrooms mesh for reach and access control, shrooms-agent for the sessions,
A2A tasks for asking agents, and Scala for the calendar.

## 1. Pieces

```
   phone (Todo app) ─┐                    ┌─ home-todo  (an agent session on the same machine)
   wife's phone ─────┼── mesh ── todo ────┤     triage, help, conversation
   Basecamp (Todo) ──┘          service   └─ other sessions (research, drafts) via A2A
                                  │
                                  └── Scala calendar (phase 2)
```

| Piece | What it is |
|---|---|
| **todo service** | A small Go daemon on one machine: holds the lists, serves them on the machine's mesh addresses. The data. |
| **Todo session** | One shrooms-agent session per list (`home-todo`), with the list's tools. The assistant. |
| **todo MCP** | `todo mcp --list home`: the tools the session works the list with, wired into the session by its own `.mcp.json`. |
| **Todo apps** | A Basecamp module (view + core) and an Android app. Find every list on every mesh the device is on. |
| **Scala bridge** | Phase 2: timed items as events in a calendar the household shares; free/busy read back. |

## 2. Lists, one per mesh

- A list belongs to one mesh: the home list exists on the home mesh, a work
  list on the office mesh. From any other mesh it does not exist — the same
  rule shrooms-agent applies to sessions (shrooms ADR-049), decided the same
  way: the caller's mesh is the name the shrooms daemon gives its address.
- One machine hosts them (decided). A machine can host several lists, one per
  mesh it is on.
- **Access is the mesh**, as for the agents: whoever is on the home mesh can
  read and edit the home list. That is the point — your wife's phone joins the
  home mesh and sees exactly the home list.
- The apps discover lists the way the agents apps discover agents: they probe
  the peers of each mesh the device is on, and show what they find grouped by
  mesh.

## 3. Items

| Field | |
|---|---|
| `id`, `title`, `notes` | |
| `state` | `inbox` → `open` → `waiting` (on somebody or something) → `done` / `dropped` |
| `due` | a date, or a date and time; optional |
| `who` | free text: "Václav", "both"; optional |
| `tags` | free |
| `added_by` | the device or agent that added it ("phone.home", "laptop/shrooms") |
| `help` | requests made for it: the kind, the worker, the A2A task id, its state, the answer and any files |
| `calendar` | the Scala event it is mirrored to, if any (phase 2) |
| `history` | every change, by whom |

Stored by the service in one file per list (SQLite, or an append-only JSON log
folded into state — the second keeps a later move to multi-writer sync
honest). Backed up like any file.

## 4. The Todo session

A normal shrooms-agent session, created once per list:

- **Limited to the list's mesh** (`meshes: [home]`), so the home mesh sees it
  and nothing else of that machine. This matters: **anyone who can see a
  session can drive it**, and the home mesh has people on it who are not you.
- **In a cage**, not sealed: it needs the list and the mesh, not anybody's
  home directory. Its tools reach the todo service over loopback through the
  cage's proxy.
- **pi on a local model** is a good default for the triage: it is called on
  every item, and the paid model is better spent on the help it asks for.
- Its standing instructions (`CLAUDE.md` / pi's system prompt in its
  directory): what the list is for, who uses it, how to triage, when to ask
  for help, when to ask the people instead.

**Its tools** (`todo mcp`): `list_items`, `get_item`, `add_item`,
`update_item`, `set_state`, `attach` (a file or an answer to an item),
`request_help` (records a help request on the item and sends the A2A task).
Plus the shrooms tools every session has: `list_agents`, `ask_agent`,
`task_status`.

## 5. Adding, triaging, helping

**People add directly.** The app writes the item to the service at once — no
model in the path, works when the session is busy or asleep. The service then
sends the session a note ("added by phone.home: buy winter tires"), and the
session triages when it gets to it: due date, tags, a question back to
whoever added it, or an offer of help. Its question and its offer appear on
the item in the app.

**Agents add through the session.** An agent that thinks something belongs on
the list sends `home-todo` a task ("the domain renews on the 20th"). The
session decides whether and how to add it — new item, a note on an existing
one, or nothing. Items it adds land in `inbox`, marked who suggested them;
only a person moves them on.

**Help is a task.** From an item, a person or the session asks for one of:

| Kind | Example | What comes back |
|---|---|---|
| find out | which tyres fit, which garages are open Saturday | an answer on the item |
| compare | three options with prices | a short table, a recommendation |
| draft | the email to the garage | text to send, or a file |
| plan | "move the office" | sub-items, proposed |
| remind | "chase the plumber on Thursday" | phase 2, with the scheduler |

The task goes to a session the session picks or the person names, carries the
item id, and its answer (and files, through the agents' file drop) comes back
onto the item. **Nothing acts in the world on its own**: an agent prepares,
a person sends, buys, books.

## 6. Scala — phase 2

*Pending answers from Scala's author (jimmy-crib/vpavlin); filled in from
them.*

1. **Out:** an item with a time becomes an event in a calendar the household
   shares, so it shows in Scala on both phones with its reminder.
2. **In:** the session reads the calendar to propose times ("Thursday after
   5 looks free").
3. **Round trip:** an edit in Scala (moved to Friday) is matched back to the
   item through a key on the event.
4. **Clock:** the agent scheduler from shrooms' calendar analysis — a turn
   sent to the session at a time — gives `home-todo` its own follow-ups.

The cost is a headless Scala hub on the list's machine.

## 7. The apps

Basecamp (view + a small core module, since a view cannot reach the network)
and Android, doing the same things:

- lists by mesh; inbox / open / waiting / done;
- add in one line; voice capture on the phone (transcribed on the machine,
  as the agents' voice notes are);
- an item: notes, due, who, its help requests and their answers;
- a conversation with the list's session ("what's left for the weekend?");
- notifications: something due, the session asked you something, help came
  back.

Neither needs Shrooms Agents. Both need shrooms, for the mesh.

## 8. Phases

1. **The list.** Service, per-mesh lists, Basecamp and Android apps, direct
   add. Useful with no agent at all.
2. **The session.** `todo mcp`, the session's set-up, triage notes, help as
   tasks, agents adding through the session.
3. **The calendar.** The Scala bridge, free/busy, the scheduler.
4. **Offline both ways**, if it is wanted: the list as a multi-writer log
   over Logos Delivery, the way Scala and Kith sync.

## 9. Open questions

1. **Name.** "Shrooms Todo" is a working name.
2. **Where the service runs.** The machine that is always on at home — pi5?
3. **Storage:** SQLite, or an event log from the start (cheaper move to
   phase 4)?
4. **Who triages:** pi on a local model by default, Claude when it asks for
   help?
5. **Notifications on the wife's phone:** the Todo app's own, or through
   Scala reminders only?
6. **Agents adding:** every agent on the mesh, or only sessions you list?
