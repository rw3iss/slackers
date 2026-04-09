# Commands

Slackers supports a slash-command interface in the input bar. Type `/`
followed by a command name. As you type, a suggestion popup appears
above the input showing the top fuzzy matches.

## Navigation

- **Type** `/` to begin
- **Up / Down** to highlight a suggestion
- **Tab** to complete the highlighted suggestion into the input
- **Enter** to run the command (or send the message if it doesn't match)
- **Esc** to dismiss the suggestion popup

## Built-in commands

### General

- `/commands` — open the full Command List view
- `/help [topic]` — open this help, or a specific topic
- `/version` — show the running version
- `/quit` — exit slackers
- `/me` — show your own contact info

### Channels & messages

- `/channels` — list every channel and friend chat
- `/clear-history` — wipe the current channel's chat history (with prompt)
- `/search <query>` — search messages in the current channel
- `/settings` — open the settings overlay

### Workspace setup

- `/setup <json|hash|--flags>` — import workspace credentials
  (client id, client secret, app token, optionally user token).
  All three formats are auto-detected; the same flow runs from
  the CLI via `slackers setup <arg>`. If existing credentials are
  set, a y/Enter confirmation prompt appears before overwriting.
- `/setup share [hash|json]` — print the current workspace
  credentials in a sharable form. Opens the Output view with
  each import command as a selectable code-snippet sub-item —
  press right-arrow to select a snippet, `c` to copy it. The
  user OAuth token (`xoxp-`) is excluded from shared output.

### Sharing (sends directly to the current chat)

- `/share me` / `/share profile` — send your own contact card
  (a `[FRIEND:me]` marker, expanded to JSON or SLF2 hash by
  the send pipeline based on Share Format)
- `/share setup` — send your workspace setup credentials as a
  one-line `slackers setup <hash>` snippet (no user token)
- `/share friend <name|id>` — send a specific friend's contact
  card (resolved by display name, email, user id, or slacker id)
- `/share theme <name>` — send a theme's JSON in a code block

If the target is missing or invalid (or requires a second arg
that wasn't supplied), `/share` surfaces an error in the status
bar and does **not** send anything.

## Intellisense / argument look-ahead

When you type a slash command and then a space, the suggestion
popup switches from command completion to argument completion.
It knows which arg the command expects based on the command's
declared `ArgSpec.Kind`:

| Arg kind | Pool |
|---|---|
| help topic | the 9 embedded help topics |
| theme name | every installed theme (cached so typing is instant) |
| friend id | your friend list with online/offline status |
| channel id | every channel + friend chat |
| share target | me, profile, setup, friend, theme |

Some commands have **context-aware** second args — `/share
friend <name>` pulls the friend list for its second slot,
`/share theme <name>` pulls the theme list. Typing the first arg
then a space automatically switches the popup to the right
pool for the NEXT slot.

**Up / Down arrow** keys navigate the popup whenever the input
starts with `/`, without ever falling through to the input
history scroll. **Tab** completes the highlighted suggestion
into the input bar, replacing only the current partial token
so earlier args stay intact. **Enter** runs the command with
the selected completion applied.

### Friends & P2P

- `/friends` — list friends in the Output view
- `/add-friend <hash | json | [FRIEND:marker]>` — import a contact card
- `/remove-friend <id|name>` — delete a friend (with prompt)

### Appearance

- `/theme [name]` — switch to a theme, or list installed themes
- `/themes` — list every installed theme

### Diagnostics

- `/config` — show current configuration in the Output view
- `/shortcuts` — open the keyboard shortcuts editor

## Argument syntax

Most commands take simple positional arguments separated by spaces.
Tokens enclosed in double quotes (`"two words"`) are passed as a
single argument with the surrounding quotes stripped.

For commands that accept structured input (e.g. `/add-friend` taking
a JSON contact card with embedded spaces), the entire string after
the command name is also available as a single raw blob — you can
paste a multi-word JSON value without quoting it and it'll be
parsed correctly.

## Custom commands & emotes

The dictionary supports user-defined emotes (saved to
`~/.config/slackers/emotes.json`) and custom commands
(`~/.config/slackers/commands.json`). Both are merged into the
global lookup at startup. See `/help friends` for more on the
emote variable syntax once it lands.
