package weather

import (
	"github.com/rw3iss/slackers/internal/api"
	"github.com/rw3iss/slackers/internal/commands"
	"github.com/rw3iss/slackers/internal/plugins"
)

// WeatherPlugin provides weather forecasts via /weather.
type WeatherPlugin struct {
	appAPI api.API
	city   string
}

func New() *WeatherPlugin {
	return &WeatherPlugin{city: "New York"}
}

func (p *WeatherPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		Name:        "weather",
		Version:     "1.0.0",
		Author:      "slackers",
		Description: "Weather forecast viewer using wttr.in",
	}
}

func (p *WeatherPlugin) Init(appAPI api.API) error {
	p.appAPI = appAPI
	return nil
}

func (p *WeatherPlugin) Start() error   { return nil }
func (p *WeatherPlugin) Stop() error    { return nil }
func (p *WeatherPlugin) Destroy() error { return nil }

func (p *WeatherPlugin) Commands() []*commands.Command {
	return []*commands.Command{
		{
			Name:        "weather",
			Aliases:     []string{"wttr"},
			Kind:        commands.KindCommand,
			Description: "Show weather forecast",
			Usage:       "/weather [city]",
			Args: []commands.ArgSpec{
				{Name: "city", Kind: commands.ArgString, Optional: true, Help: "city name (default: New York)"},
			},
			Run: func(ctx *commands.Context) commands.Result {
				city := p.city
				if len(ctx.Args) > 0 {
					city = ctx.Raw
					p.city = city
				}
				forecast, err := FetchWeather(city)
				if err != nil {
					return commands.Result{
						Status:    commands.StatusError,
						StatusBar: "Weather fetch failed: " + err.Error(),
					}
				}
				return commands.Result{
					Status: commands.StatusOK,
					Title:  "Weather — " + city,
					Body:   forecast,
				}
			},
		},
	}
}

func (p *WeatherPlugin) Shortcuts() map[string][]string {
	return map[string][]string{
		"show_weather": {"ctrl+w"},
	}
}

func (p *WeatherPlugin) MessageFilter(senderID, data string) bool { return false }

func (p *WeatherPlugin) ConfigFields() []plugins.ConfigField {
	return []plugins.ConfigField{
		{
			Key:         "city",
			Label:       "City / Zipcode",
			Value:       p.city,
			Description: "City name or zipcode for weather lookups (e.g. 'London', '10001')",
		},
	}
}

func (p *WeatherPlugin) SetConfig(key, value string) {
	switch key {
	case "city":
		if value != "" {
			p.city = value
		}
	}
}
