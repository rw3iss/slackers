package slack

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rw3iss/slackers/internal/types"
	"github.com/slack-go/slack"
)

// SlackService defines the interface for Slack Web API operations.
type SlackService interface {
	AuthTest() (string, error)
	ListChannels() ([]types.Channel, error)
	ListUsers() (map[string]types.User, error)
	FetchHistory(channelID string, limit int) ([]types.Message, error)
	SendMessage(channelID, text string) error
	ResolveUserName(userID string) string
	// Warnings returns and clears any accumulated fallback warnings.
	Warnings() []string
}

// slackClient implements SlackService using the slack-go library.
type slackClient struct {
	primary   *slack.Client // user token (preferred) or bot token
	fallback  *slack.Client // bot token (fallback), nil if no user token
	hasUser   bool
	mu        sync.RWMutex
	users     map[string]types.User
	warnMu    sync.Mutex
	warnings  []string
}

// NewSlackClient creates a new SlackService backed by the Slack Web API.
// If userToken is provided, it becomes the primary client and all API calls
// go through it first. The bot token is kept as a fallback.
func NewSlackClient(botToken, userToken string) SlackService {
	c := &slackClient{
		users: make(map[string]types.User),
	}
	if userToken != "" {
		c.primary = slack.New(userToken)
		c.fallback = slack.New(botToken)
		c.hasUser = true
	} else {
		c.primary = slack.New(botToken)
		c.fallback = nil
		c.hasUser = false
	}
	return c
}

// Warnings returns and clears any accumulated fallback warnings.
func (c *slackClient) Warnings() []string {
	c.warnMu.Lock()
	defer c.warnMu.Unlock()
	w := c.warnings
	c.warnings = nil
	return w
}

func (c *slackClient) addWarning(msg string) {
	c.warnMu.Lock()
	defer c.warnMu.Unlock()
	c.warnings = append(c.warnings, msg)
}

// tryWithFallback runs fn against the primary client. If it fails and a
// fallback client exists, it retries with the fallback and logs a warning.
func (c *slackClient) tryWithFallback(operation string, fn func(api *slack.Client) error) error {
	err := fn(c.primary)
	if err == nil {
		return nil
	}
	if c.fallback != nil {
		fallbackErr := fn(c.fallback)
		if fallbackErr == nil {
			c.addWarning(fmt.Sprintf("%s: used bot token (user token: %s)", operation, shortenErr(err)))
			return nil
		}
	}
	return err
}

// AuthTest validates the token and returns the team name.
func (c *slackClient) AuthTest() (string, error) {
	resp, err := c.primary.AuthTest()
	if err != nil && c.fallback != nil {
		resp, err = c.fallback.AuthTest()
	}
	if err != nil {
		return "", fmt.Errorf("slack auth test: %w", err)
	}
	return resp.Team, nil
}

// ListChannels retrieves all conversations the user can see, sorted by type.
func (c *slackClient) ListChannels() ([]types.Channel, error) {
	var channels []types.Channel

	params := &slack.GetConversationsParameters{
		Types:           []string{"public_channel", "private_channel", "im", "mpim"},
		Limit:           200,
		ExcludeArchived: true,
	}

	var fetchErr error
	for {
		convs, nextCursor, err := c.primary.GetConversations(params)
		if err != nil {
			// Try fallback for the whole listing.
			if c.fallback != nil {
				convs, nextCursor, err = c.fallback.GetConversations(params)
				if err == nil {
					c.addWarning("list channels: used bot token")
				}
			}
			if err != nil {
				fetchErr = fmt.Errorf("slack list channels: %w", err)
				break
			}
		}

		for _, conv := range convs {
			ch := types.Channel{
				ID:        conv.ID,
				Name:      conv.Name,
				IsDM:      conv.IsIM,
				IsPrivate: conv.IsPrivate,
				IsGroup:   conv.IsMpIM,
			}

			if conv.IsIM {
				ch.UserID = conv.User
				ch.Name = c.ResolveUserName(conv.User)
			}

			channels = append(channels, ch)
		}

		if nextCursor == "" {
			break
		}
		params.Cursor = nextCursor
	}

	if fetchErr != nil {
		return nil, fetchErr
	}

	sort.SliceStable(channels, func(i, j int) bool {
		return channelSortOrder(channels[i]) < channelSortOrder(channels[j])
	})

	return channels, nil
}

func channelSortOrder(ch types.Channel) int {
	switch {
	case !ch.IsPrivate && !ch.IsDM && !ch.IsGroup:
		return 0
	case ch.IsPrivate && !ch.IsDM && !ch.IsGroup:
		return 1
	case ch.IsDM:
		return 2
	default:
		return 3
	}
}

// ListUsers fetches all workspace users and caches them.
func (c *slackClient) ListUsers() (map[string]types.User, error) {
	var slackUsers []slack.User
	err := c.tryWithFallback("list users", func(api *slack.Client) error {
		var e error
		slackUsers, e = api.GetUsers()
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("slack list users: %w", err)
	}

	result := make(map[string]types.User, len(slackUsers))
	for _, u := range slackUsers {
		user := types.User{
			ID:          u.ID,
			DisplayName: u.Profile.DisplayName,
			RealName:    u.RealName,
		}
		result[u.ID] = user
	}

	c.mu.Lock()
	c.users = result
	c.mu.Unlock()

	return result, nil
}

// FetchHistory retrieves recent messages from a channel in chronological order.
func (c *slackClient) FetchHistory(channelID string, limit int) ([]types.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	}

	var resp *slack.GetConversationHistoryResponse
	err := c.tryWithFallback("fetch history", func(api *slack.Client) error {
		var e error
		resp, e = api.GetConversationHistory(params)
		if e != nil && isNotInChannel(e) && api == c.fallback {
			// Bot not in channel -- try joining (works for public channels).
			_, _, _, joinErr := api.JoinConversation(channelID)
			if joinErr == nil {
				resp, e = api.GetConversationHistory(params)
			}
		}
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("slack fetch history for %s: %w", channelID, err)
	}

	messages := make([]types.Message, 0, len(resp.Messages))
	for _, msg := range resp.Messages {
		messages = append(messages, types.Message{
			UserID:    msg.User,
			UserName:  c.ResolveUserName(msg.User),
			Text:      msg.Text,
			Timestamp: parseSlackTimestamp(msg.Timestamp),
			ChannelID: channelID,
		})
	}

	// Slack returns newest first; reverse to chronological order.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// SendMessage posts a text message to the specified channel.
func (c *slackClient) SendMessage(channelID, text string) error {
	err := c.tryWithFallback("send message", func(api *slack.Client) error {
		_, _, e := api.PostMessage(channelID, slack.MsgOptionText(text, false))
		return e
	})
	if err != nil {
		return fmt.Errorf("slack send message to %s: %w", channelID, err)
	}
	return nil
}

// ResolveUserName returns a human-readable name for a user ID.
func (c *slackClient) ResolveUserName(userID string) string {
	c.mu.RLock()
	user, ok := c.users[userID]
	c.mu.RUnlock()

	if !ok {
		return userID
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	if user.RealName != "" {
		return user.RealName
	}
	return userID
}

// parseSlackTimestamp converts a Slack message timestamp string to time.Time.
func parseSlackTimestamp(ts string) (t time.Time) {
	parts := splitTimestamp(ts)
	if len(parts) == 0 {
		return t
	}

	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return t
	}

	var nsec int64
	if len(parts) == 2 {
		micro := parts[1]
		for len(micro) < 9 {
			micro += "0"
		}
		nsec, _ = strconv.ParseInt(micro[:9], 10, 64)
	}

	return time.Unix(sec, nsec)
}

func isNotInChannel(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not_in_channel") ||
		strings.Contains(msg, "channel_not_found")
}

func shortenErr(err error) string {
	s := err.Error()
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

func splitTimestamp(ts string) []string {
	idx := -1
	for i, c := range ts {
		if c == '.' {
			idx = i
			break
		}
	}
	if idx < 0 {
		if ts == "" {
			return nil
		}
		return []string{ts}
	}
	return []string{ts[:idx], ts[idx+1:]}
}
