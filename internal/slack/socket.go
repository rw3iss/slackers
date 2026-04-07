package slack

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/rw3iss/slackers/internal/debug"
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
	SlackTS string // raw Slack timestamp for lastSeen tracking

	// Reaction fields (for reaction_added/reaction_removed events).
	ChannelID    string
	ReactionUser string
	TargetTS     string
	EmojiName    string
}

// SocketService defines the interface for real-time Slack event listening.
type SocketService interface {
	Connect(ctx context.Context, eventCh chan<- SocketEvent) error
}

// socketClient implements SocketService using Slack's Socket Mode.
type socketClient struct {
	botToken   string
	appToken   string
	lastStatus types.ConnectionStatus
}

// NewSocketClient creates a new SocketService for real-time event listening.
func NewSocketClient(botToken, appToken string) SocketService {
	return &socketClient{
		botToken: botToken,
		appToken: appToken,
	}
}

// Connect establishes a Socket Mode connection with automatic reconnection.
// It blocks until the context is cancelled, reconnecting on any failure.
func (s *socketClient) Connect(ctx context.Context, eventCh chan<- SocketEvent) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		_ = s.connectOnce(ctx, eventCh)

		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Connection dropped — notify and retry.
		if s.lastStatus != types.StatusDisconnected {
			s.lastStatus = types.StatusDisconnected
			eventCh <- SocketEvent{Type: "status", Status: types.StatusDisconnected}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			s.lastStatus = types.StatusConnecting
			eventCh <- SocketEvent{Type: "status", Status: types.StatusConnecting}
		}
	}
}

func (s *socketClient) connectOnce(ctx context.Context, eventCh chan<- SocketEvent) error {
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
				return nil // channel closed, trigger reconnect
			}

			s.handleEvent(client, evt, eventCh)
		}
	}
}

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
		if s.lastStatus != types.StatusConnected {
			debug.Log("[socket] connected")
			s.lastStatus = types.StatusConnected
			eventCh <- SocketEvent{Type: "status", Status: types.StatusConnected}
		}

	case socketmode.EventTypeConnecting:
		if s.lastStatus == types.StatusDisconnected || s.lastStatus == 0 {
			debug.Log("[socket] connecting...")
			s.lastStatus = types.StatusConnecting
			eventCh <- SocketEvent{Type: "status", Status: types.StatusConnecting}
		}

	case socketmode.EventTypeDisconnect:
		debug.Log("[socket] disconnected")
		s.lastStatus = types.StatusDisconnected
		eventCh <- SocketEvent{Type: "status", Status: types.StatusDisconnected}

	case socketmode.EventTypeConnectionError:
		debug.Log("[socket] connection error")
		s.lastStatus = types.StatusError
		eventCh <- SocketEvent{Type: "status", Status: types.StatusError}

	default:
		if evt.Request != nil {
			client.Ack(*evt.Request)
		}
	}
}

func (s *socketClient) handleEventsAPI(event slackevents.EventsAPIEvent, eventCh chan<- SocketEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		innerEvent := event.InnerEvent

		switch ev := innerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			if ev.SubType != "" {
				debug.Log("[socket] ignoring message subtype=%s channel=%s", ev.SubType, ev.Channel)
				return
			}
			debug.Log("[socket] message channel=%s user=%s ts=%s", ev.Channel, ev.User, ev.TimeStamp)

			socketEvt := SocketEvent{
				Type: "message",
				Message: types.Message{
					UserID:    ev.User,
					Text:      ev.Text,
					ChannelID: ev.Channel,
					Timestamp: parseSlackTimestamp(ev.TimeStamp),
				},
				SlackTS: ev.TimeStamp,
			}

			eventCh <- socketEvt

		case *slackevents.ReactionAddedEvent:
			debug.Log("[socket] reaction_added user=%s emoji=%s ts=%s", ev.User, ev.Reaction, ev.Item.Timestamp)
			eventCh <- SocketEvent{
				Type:         "reaction_added",
				ChannelID:    ev.Item.Channel,
				ReactionUser: ev.User,
				TargetTS:     ev.Item.Timestamp,
				EmojiName:    ev.Reaction,
			}

		case *slackevents.ReactionRemovedEvent:
			debug.Log("[socket] reaction_removed user=%s emoji=%s ts=%s", ev.User, ev.Reaction, ev.Item.Timestamp)
			eventCh <- SocketEvent{
				Type:         "reaction_removed",
				ChannelID:    ev.Item.Channel,
				ReactionUser: ev.User,
				TargetTS:     ev.Item.Timestamp,
				EmojiName:    ev.Reaction,
			}
		}
	}
}
