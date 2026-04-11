# Plugins

slackers supports a plugin system for extending functionality
with custom commands, games, tools, and integrations.

## Commands

  /plugins          Open the Plugin Manager
  /plugin enable <name>    Enable a plugin
  /plugin disable <name>   Disable a plugin
  /plugin uninstall <name> Remove a plugin
  /plugin info <name>      Show plugin details
  /plugin list             List all plugins

## Built-in Plugins

  games    Mini games (snake, tetris) — /games
  weather  Weather forecast viewer — /weather [city]

## Plugin Manager

Open with /plugins or its keyboard shortcut. The manager
shows all installed plugins in a table with:

  - Name, version, author, and status
  - Enter: open plugin config
  - e: toggle enable/disable
  - d: uninstall (with confirmation)

## Game Controls

  /games snake     Start snake game
  /games tetris    Start tetris game
  /games quit      Quit the running game

In-game:
  Ctrl+S   Open game settings (board size, speed, etc.)
  Ctrl+Q   Hide game to background (paused)
  P/Space  Toggle pause
  R        Restart (after game over)

## Weather

  /weather          Show forecast (last city or default)
  /weather London   Show forecast for London

Configure your default city in the weather plugin config
(Plugin Manager → weather → Enter).
