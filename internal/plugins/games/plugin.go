package games

import (
	"github.com/rw3iss/slackers/internal/api"
	"github.com/rw3iss/slackers/internal/commands"
	"github.com/rw3iss/slackers/internal/plugins"
)

// GamesPlugin provides mini-games accessible via /games.
type GamesPlugin struct {
	appAPI api.API
}

func New() *GamesPlugin {
	return &GamesPlugin{}
}

func (p *GamesPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		Name:        "games",
		Version:     "1.0.0",
		Author:      "slackers",
		Description: "Mini games: snake, tetris, and more",
	}
}

func (p *GamesPlugin) Init(appAPI api.API) error {
	p.appAPI = appAPI
	return nil
}

func (p *GamesPlugin) Start() error   { return nil }
func (p *GamesPlugin) Stop() error    { return nil }
func (p *GamesPlugin) Destroy() error { return nil }

func (p *GamesPlugin) Commands() []*commands.Command {
	return []*commands.Command{
		{
			Name:        "games",
			Aliases:     []string{"game", "play"},
			Kind:        commands.KindCommand,
			Description: "Play mini games (snake, tetris)",
			Usage:       "/games [game-name]",
			Args: []commands.ArgSpec{
				{Name: "game", Kind: commands.ArgString, Optional: true, Help: "snake or tetris"},
			},
			Run: func(ctx *commands.Context) commands.Result {
				gameName := "menu"
				if len(ctx.Args) > 0 {
					gameName = ctx.Args[0]
				}
				switch gameName {
				case "snake":
					return commands.Result{
						Status:    commands.StatusOK,
						Title:     "Snake",
						Body:      NewSnakeGame().RenderFrame(),
						StatusBar: "Snake game loaded! (Note: Full interactive mode coming in next update)",
					}
				case "menu", "":
					return commands.Result{
						Status: commands.StatusOK,
						Title:  "Mini Games",
						Body:   "Available games:\n\n  snake   — Classic snake game\n  tetris  — Block stacking puzzle\n\nUsage: /games <name>",
					}
				default:
					return commands.Result{
						Status:    commands.StatusError,
						StatusBar: "Unknown game: " + gameName + " (try: snake, tetris)",
					}
				}
			},
		},
	}
}

func (p *GamesPlugin) Shortcuts() map[string][]string        { return nil }
func (p *GamesPlugin) MessageFilter(senderID, data string) bool { return false }
