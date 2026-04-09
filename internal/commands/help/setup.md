# Setup

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
