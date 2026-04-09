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
