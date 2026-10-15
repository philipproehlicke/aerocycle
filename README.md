# aerocycle

A small companion for [AeroSpace](https://nikitabobko.github.io/AeroSpace/)
that adds **per-application window switching**:

- Press **right-⌘ + &lt;key&gt;** once → the *most recently used* window of that
  app comes into focus.
- Press it again (within `cycle-timeout-ms`) → cycle through that app's other
  windows.
- Or bind **right-⌘ + &lt;key&gt;** to an arbitrary AeroSpace command (e.g.
  `move left`, `fullscreen`, `layout tiles horizontal vertical`).
- Works alongside AeroSpace's tiling: cycling picks a specific window, wherever
  it lives, and AeroSpace brings it forward (switching workspaces if needed).

It exists because AeroSpace cannot distinguish the left from the right Command
key, and has no "cycle windows of the same app" command.

## How it works

```
 right-⌘ + d
     │
     ▼
 CGEventTap (this app, cgo)          ← consumes the combo, so the focused app
     │                                  never sees ⌘d
     ▼
 cycler (per-app MRU, in memory)
     │
     ▼
 `aerospace focus --window-id N`     ← AeroSpace does the actual focusing
```

- Window knowledge: `aerospace list-windows --all --json`.
- Focus recency: `aerospace subscribe --all` (`focus-changed` events).
- AeroSpace has no "window closed" event, so the cache is reconciled against a
  live listing every 2 seconds.
- If AeroSpace is not reachable (quit or `aerospace enable off`), aerocycle
  stops consuming combos and lets them pass through to the app.

## Build & install

```sh
cd ~/Workspace/aerocycle
make build            # -> bin/aerocycle
make install          # -> ~/.local/bin/aerocycle (signed)
```

`make install` signs with the first available code-signing identity (e.g. your
Apple Development certificate) and falls back to ad-hoc signing. Use a real
identity: macOS ties the Accessibility grant to the code signature, so a stable
identity means the grant survives rebuilds.

## Permissions (required, one-off)

The event tap needs **Accessibility**, and macOS attributes that grant to the
*process that requests it*. Run aerocycle as its own process (via launchd — see
below) so the entry is **aerocycle**, not your terminal.

```sh
make agent-install       # installs + starts the launchd agent
# System Settings > Privacy & Security > Accessibility > enable "aerocycle"
make agent-restart       # pick up the grant
```

Check status:

```sh
~/.local/bin/aerocycle -check-permissions
```

## Configure — TUI

```sh
make tui               # or: aerocycle tui
```

- `a` add an app binding (choose a key, then pick from running apps)
- `A` add an action binding (choose a key, then type an AeroSpace command)
- `d` delete the highlighted binding
- `s` save — and hot-reload the running daemon (via SIGHUP)
- `r` reload from disk · `q` quit

Config lives at `~/.config/aerocycle/config.toml`:

```toml
cycle-timeout-ms = 1200

# right-<cmd>+<key> cycles that app's windows
[apps]
d = "com.vivaldi.Vivaldi"
t = "com.googlecode.iterm2"

# right-<cmd>+<key> runs an AeroSpace command (via `aerospace eval`)
[actions]
h = "move left"
j = "move down"
k = "move up"
l = "move right"
```

A key may appear in `[apps]` or `[actions]`, but not both. Saving keeps a
`config.toml.bak` backup.

Find bundle ids with `aerocycle -list-apps`.

## Always running

The launchd agent uses `RunAtLoad` + `KeepAlive`, so aerocycle starts at login
and is restarted if it ever exits. It reconnects to AeroSpace automatically, so
it does not need to be tied to AeroSpace's own lifecycle.

| Command | Effect |
| --- | --- |
| `make agent-install` | install + start at login |
| `make agent-restart` | restart now |
| `make agent-status` | show launchd state |
| `make agent-logs` | tail the log |
| `make agent-uninstall` | stop + remove |

## Flags / commands

| Command | Purpose |
| --- | --- |
| `aerocycle` | run the daemon |
| `aerocycle tui` | edit shortcuts in a terminal UI |
| `aerocycle -debug` | log detected combos and cycling decisions |
| `aerocycle -simulate` | cycle from stdin key names, no event tap |
| `aerocycle -list-apps` | print bundle ids of running apps |
| `aerocycle -check-permissions` | report Accessibility status |
| `aerocycle -request-permission` | trigger the macOS permission dialog |
| `aerocycle -config <path>` | use a different config file |

## Troubleshooting

- **Nothing happens on right-⌘+d** — run `aerocycle -check-permissions`; then
  `make agent-restart` and watch `make agent-logs`.
- **Log says "AeroSpace not reachable"** — AeroSpace is quit or disabled; the
  combos pass through to the app until it's back.
- **A window on another workspace isn't focused** — aerocycle switches to the
  window's workspace first; check the `-debug` log.
