# Setup

## Quick Start

If someone already has slackers configured, they can share their setup with you. Run one of these on their machine:

`slackers setup share`

or from inside slackers:

`/setup share`

This outputs a compact hash you can import:

`slackers setup H4sIAAAAAAAA_...`

That's it — you're connected.

## Setup Methods

### From a shared hash or JSON

`slackers setup H4sIAAAAAAAA_...`

`slackers setup '{"client_id":"...","client_secret":"...","app_token":"xapp-..."}'`

`slackers setup --client-id X --client-secret Y --app-token xapp-...`

All three formats are auto-detected. They also work inside a running client via `/setup <arg>`.

### From OAuth login

If you have a Slack app's Client ID, Client Secret, and App Token:

`slackers login --client-id CLIENT_ID --client-secret CLIENT_SECRET --app-token xapp-...`

A browser window opens for OAuth. Once authorized, your tokens are saved and you can launch with just `slackers`.

### From a URL

If your team publishes credentials at a URL:

`slackers join https://your-team.com/team.json`

The JSON file should contain `client_id`, `client_secret`, and `app_token` fields.

### Interactive setup

`slackers setup`

Walks you through entering credentials step by step.

## Multiple Workspaces

You can connect to multiple Slack workspaces. Each `slackers setup` with a different workspace's credentials adds a new workspace automatically. Press `Alt+W` to manage workspaces:

- `Enter` — switch to the selected workspace
- `a` — add a new workspace
- `e` — edit workspace settings
- `s` — sign in or out
- `d` — remove a workspace

Each workspace's data is stored separately under `~/.config/slackers/workspaces/<team-id>/`.

## Friends-Only Mode

slackers works without any Slack workspace for P2P-only chat. Skip the login step — if `friends.json` has entries, slackers boots in friends-only mode.

## Sharing Your Setup

### From the CLI

`slackers setup share`

`slackers setup share json`

### From inside slackers

`/setup share`

`/setup share json`

The output includes ready-to-run import commands. The user OAuth token (`xoxp-`) is intentionally excluded — a leaked hash can only bootstrap a fresh OAuth login, not impersonate you.

In the Output view, use arrow keys to select code snippets and `c` to copy them.

## Backup & Restore

Export your entire config (settings, themes, friends, workspaces):

`slackers export`

Restore from a backup zip:

`slackers import ~/Downloads/slackers-backup.zip`

These are also available under **Settings → Backup**.

## Config Location

All data lives under `~/.config/slackers/`:

```
config.json          — global settings (theme, shortcuts, P2P)
secure.key           — P2P encryption key
friends/             — friend list and chat history
workspaces/          — per-workspace tokens and channel data
themes/              — custom themes
```

To run a second instance (e.g. for P2P testing):

`XDG_CONFIG_HOME=/tmp/slackers-test slackers`
