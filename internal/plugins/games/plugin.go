package games

import (
	"github.com/rw3iss/slackers/internal/api"
	"github.com/rw3iss/slackers/internal/commands"
	"github.com/rw3iss/slackers/internal/plugins"
)

// GameQuitRequest is returned when the user types /games quit.
type GameQuitRequest struct{}

// GameStartRequest is returned as a Result.Cmd by the /games
// command when the user picks a specific game. The Model checks
// for this type and opens the game overlay.
type GameStartRequest struct {
	Name string
}

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
				{Name: "game", Kind: commands.ArgGameName, Optional: true, Help: "snake or tetris"},
			},
			Run: func(ctx *commands.Context) commands.Result {
				gameName := ""
				if len(ctx.Args) > 0 {
					gameName = ctx.Args[0]
				}
				switch gameName {
				case "snake", "tetris":
					return commands.Result{
						Status: commands.StatusOK,
						Cmd:    GameStartRequest{Name: gameName},
					}
				case "quit", "exit", "stop":
					return commands.Result{
						Status: commands.StatusOK,
						Cmd:    GameQuitRequest{},
					}
				case "", "menu":
					return commands.Result{
						Status: commands.StatusOK,
						Title:  "Mini Games",
						Sections: []commands.Section{
							{Text: "snake  — Classic snake game", Selectable: true, Title: "snake"},
							{Text: "tetris — Block stacking puzzle (coming soon)", Selectable: true, Title: "tetris"},
						},
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
