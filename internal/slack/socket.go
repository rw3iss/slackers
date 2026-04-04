package slack

import (
	"context"
	"fmt"
	"log"
	"io"

	"github.com/rw3iss/slackers/internal/types"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// SocketEvent represents a real-time event received over the socket connection.
type SocketEvent struct {
	Type    string
	Message types.Message
	Status  types.ConnectionStatus
}

// SocketService defines the interface for real-time Slack event listening.
type SocketService interface {
	Connect(ctx context.Context, eventCh chan<- SocketEvent) error
}

// socketClient implements SocketService using Slack's Socket Mode.
type socketClient struct {
	botToken string
	appToken string
}

// NewSocketClient creates a new SocketService for real-time event listening.
func NewSocketClient(botToken, appToken string) SocketService {
	return &socketClient{
		botToken: botToken,
		appToken: appToken,
	}
}

// Connect establishes a Socket Mode connection and forwards message events
// to the provided channel. It blocks until the context is cancelled.
func (s *socketClient) Connect(ctx context.Context, eventCh chan<- SocketEvent) error {
	// Suppress the slack-go library's internal logging which corrupts the TUI.
	api := slack.New(
		s.botToken,
		slack.OptionAppLevelToken(s.appToken),
		slack.OptionLog(log.New(io.Discard, "", 0)),
	)

	client := socketmode.New(
		api,
		socketmode.OptionLog(log.New(io.Discard, "", 0)),
	)

	go func() {
		_ = client.RunContext(ctx)
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case evt, ok := <-client.Events:
			if !ok {
				return fmt.Errorf("socket mode events channel closed")
			}

			s.handleEvent(client, evt, eventCh)
		}
	}
}

// handleEvent processes a single socket mode event.
func (s *socketClient) handleEvent(client *socketmode.Client, evt socketmode.Event, eventCh chan<- SocketEvent) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		client.Ack(*evt.Request)
		s.handleEventsAPI(eventsAPIEvent, eventCh)

	case socketmode.EventTypeConnected:
		eventCh <- SocketEvent{Type: "status", Status: types.StatusConnected}

	case socketmode.EventTypeConnecting:
		eventCh <- SocketEvent{Type: "status", Status: types.StatusConnecting}

	case socketmode.EventTypeDisconnect:
		eventCh <- SocketEvent{Type: "status", Status: types.StatusDisconnected}

	case socketmode.EventTypeConnectionError:
		eventCh <- SocketEvent{Type: "status", Status: types.StatusError}

	default:
		if evt.Request != nil {
			client.Ack(*evt.Request)
		}
	}
}

// handleEventsAPI extracts message events and sends them to the event channel.
func (s *socketClient) handleEventsAPI(event slackevents.EventsAPIEvent, eventCh chan<- SocketEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		innerEvent := event.InnerEvent

		switch ev := innerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			if ev.SubType != "" {
				return
			}

			socketEvt := SocketEvent{
				Type: "message",
				Message: types.Message{
					UserID:    ev.User,
					Text:      ev.Text,
					ChannelID: ev.Channel,
				},
			}

			eventCh <- socketEvt
		}
	}
}
