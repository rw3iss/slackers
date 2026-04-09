# Setup

## Workspace credential sharing (`setup share` / `setup <arg>`)

If your team already has a working slackers instance somewhere,
you can pipe their credentials directly into your own copy:

```bash
# On the teammate's machine
slackers setup share          # default: hash form
slackers setup share json     # explicit JSON form
```

That prints an intro blurb followed by two ready-to-run import
commands in both JSON and hash formats. They'll look like:

```bash
slackers setup '{"client_id":"...","client_secret":"...","app_token":"xapp-..."}'
slackers setup H4sIAAAAAAAA_...
```

**The user OAuth token (`xoxp-`) is intentionally excluded from
shared output.** Even a leaked setup hash can only be used to
bootstrap a fresh OAuth login via `slackers login`, not to
impersonate the sharer.

You can also run `/setup share` from inside a running slackers
instance — the output opens in the Output view with each command
rendered as a selectable code-snippet sub-item so you can press
right-arrow to select the snippet and `c` to copy it to the
clipboard.

Import supports three input formats, auto-detected:

```bash
# 1. JSON
slackers setup '{"client_id":"...","client_secret":"..."}'

# 2. Hash (compact single-line)
slackers setup H4sIAAAAAAAA_...

# 3. Flags
slackers setup --client-id X --client-secret Y --app-token xapp-...
```

The same three forms work inside a running client via the
`/setup <arg>` command. If your existing config already has
Slack credentials, the import flow prompts for confirmation
before overwriting.

## First-time login

Get a Client ID, Client Secret, and App-Level Token from your team's
Slack app (or have an admin create one with the manifest in
`configs/slack-app-manifest.json`).

Then run:

```bash
slackers login --client-id CLIENT_ID --client-secret CLIENT_SECRET --app-token xapp-...
```

A browser window opens for OAuth. Once authorised, your tokens are
saved to `~/.config/slackers/config.json` and you can launch with
just `slackers`.

## One-command onboarding

Admins can publish a JSON file with the credentials and teammates
join with one command:

```bash
slackers join https://your-url.com/team.json
```

The JSON should contain `client_id`, `client_secret`, and
`app_token` fields.

## Friends-only mode

Slackers also runs without any Slack workspace if you have friends
in your store. Skip the login step and slackers will boot in
friends-only mode the next time you launch (if `friends.json` has
entries).

## Backup & restore

- `slackers export` — write the entire `~/.config/slackers/` directory
  to a single zip in `~/Downloads`
- `slackers import <zip>` — restore from a zip with `replace` or `merge`
  mode

The same import / export controls live under
**Settings → Backup**.
