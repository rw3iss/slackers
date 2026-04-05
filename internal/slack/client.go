package slack

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	// FetchHistoryAround fetches messages around a specific timestamp for context.
	// Returns messages, the index of the target message (or -1), and any error.
	FetchHistoryAround(channelID string, timestamp string, contextSize int) ([]types.Message, int, error)
	// SearchMessages searches for messages matching query. If channelID is non-empty, scopes to that channel.
	SearchMessages(query, channelID string, limit int) ([]types.SearchResult, error)
	// ListFiles returns files visible to the user. If channelID is non-empty, scopes to that channel.
	ListFiles(channelID string, count int) ([]types.FileInfo, error)
	// UploadFile uploads a file to a channel.
	UploadFile(channelID, filePath string) error
	// DownloadFile downloads a file from Slack to the local path. Pass a context for cancellation.
	DownloadFile(ctx context.Context, url, destPath string) error
	// CheckNewMessages returns channel IDs with new messages and a map of all latest timestamps.
	CheckNewMessages(lastSeen map[string]string) ([]string, map[string]string, error)
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
		registerClientToken(c.primary, userToken)
		registerClientToken(c.fallback, botToken)
	} else {
		c.primary = slack.New(botToken)
		c.fallback = nil
		c.hasUser = false
		registerClientToken(c.primary, botToken)
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
			Files:     extractFiles(msg.Files),
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

// FetchHistoryAround fetches messages before and after a specific timestamp.
func (c *slackClient) FetchHistoryAround(channelID string, timestamp string, contextSize int) ([]types.Message, int, error) {
	if contextSize < 5 {
		contextSize = 25
	}
	half := contextSize / 2

	// Fetch messages before (and including) the target.
	beforeParams := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Latest:    timestamp,
		Limit:     half + 1,
		Inclusive: true,
	}
	var beforeResp *slack.GetConversationHistoryResponse
	err := c.tryWithFallback("fetch context before", func(api *slack.Client) error {
		var e error
		beforeResp, e = api.GetConversationHistory(beforeParams)
		return e
	})
	if err != nil {
		return nil, -1, fmt.Errorf("fetch context for %s: %w", channelID, err)
	}

	// Fetch messages after the target.
	afterParams := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Oldest:    timestamp,
		Limit:     half,
		Inclusive: false,
	}
	var afterResp *slack.GetConversationHistoryResponse
	err = c.tryWithFallback("fetch context after", func(api *slack.Client) error {
		var e error
		afterResp, e = api.GetConversationHistory(afterParams)
		return e
	})
	if err != nil {
		// If after-fetch fails, just use before messages.
		afterResp = &slack.GetConversationHistoryResponse{}
	}

	// Before messages come newest-first, reverse them.
	var messages []types.Message
	for i := len(beforeResp.Messages) - 1; i >= 0; i-- {
		msg := beforeResp.Messages[i]
		messages = append(messages, types.Message{
			UserID:    msg.User,
			UserName:  c.ResolveUserName(msg.User),
			Text:      msg.Text,
			Timestamp: parseSlackTimestamp(msg.Timestamp),
			ChannelID: channelID,
			Files:     extractFiles(msg.Files),
		})
	}

	targetIdx := len(messages) - 1

	// After messages also come newest-first, reverse them.
	for i := len(afterResp.Messages) - 1; i >= 0; i-- {
		msg := afterResp.Messages[i]
		messages = append(messages, types.Message{
			UserID:    msg.User,
			UserName:  c.ResolveUserName(msg.User),
			Text:      msg.Text,
			Timestamp: parseSlackTimestamp(msg.Timestamp),
			ChannelID: channelID,
			Files:     extractFiles(msg.Files),
		})
	}

	if targetIdx < 0 {
		targetIdx = 0
	}

	return messages, targetIdx, nil
}

// SearchMessages searches for messages using Slack's search.messages API.
// Requires a user token (search:read scope). If channelID is provided, the
// query is scoped to that channel via "in:<channel>" modifier.
func (c *slackClient) SearchMessages(query, channelID string, limit int) ([]types.SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	searchQuery := query
	if channelID != "" {
		searchQuery = fmt.Sprintf("in:<#%s> %s", channelID, query)
	}

	params := slack.SearchParameters{
		Sort:          "timestamp",
		SortDirection: "desc",
		Count:         limit,
	}

	var msgs *slack.SearchMessages
	err := c.tryWithFallback("search messages", func(api *slack.Client) error {
		var e error
		msgs, e = api.SearchMessages(searchQuery, params)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("slack search: %w", err)
	}

	results := make([]types.SearchResult, 0, len(msgs.Matches))
	for _, m := range msgs.Matches {
		results = append(results, types.SearchResult{
			Message: types.Message{
				UserID:    m.User,
				UserName:  c.ResolveUserName(m.User),
				Text:      m.Text,
				Timestamp: parseSlackTimestamp(m.Timestamp),
				ChannelID: m.Channel.ID,
			},
			ChannelID:   m.Channel.ID,
			ChannelName: m.Channel.Name,
			Permalink:   m.Permalink,
		})
	}

	return results, nil
}

// ListFiles returns all files visible to the user across all channels.
func (c *slackClient) ListFiles(channelID string, count int) ([]types.FileInfo, error) {
	if count <= 0 {
		count = 100
	}

	params := slack.ListFilesParameters{
		Limit:   count,
		Channel: channelID,
	}

	var files []types.FileInfo
	err := c.tryWithFallback("list files", func(api *slack.Client) error {
		slackFiles, _, e := api.ListFiles(params)
		if e != nil {
			return e
		}
		for _, f := range slackFiles {
			channelName := ""
			if len(f.Channels) > 0 {
				channelName = f.Channels[0]
			} else if len(f.Groups) > 0 {
				channelName = f.Groups[0]
			} else if len(f.IMs) > 0 {
				channelName = f.IMs[0]
			}

			files = append(files, types.FileInfo{
				ID:          f.ID,
				Name:        f.Name,
				Size:        int64(f.Size),
				MimeType:    f.Mimetype,
				URL:         f.URLPrivateDownload,
				ChannelName: channelName,
				UserName:    c.ResolveUserName(f.User),
				Timestamp:   parseSlackTimestamp(fmt.Sprintf("%d", int64(f.Timestamp))),
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return files, nil
}

// UploadFile uploads a local file to a Slack channel using the v2 API.
func (c *slackClient) UploadFile(channelID, filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("cannot stat file %s: %w", filePath, err)
	}

	params := slack.UploadFileV2Parameters{
		Channel:  channelID,
		File:     filePath,
		FileSize: int(info.Size()),
		Filename: filepath.Base(filePath),
	}

	uploadErr := c.tryWithFallback("upload file", func(api *slack.Client) error {
		_, e := api.UploadFileV2(params)
		return e
	})
	if uploadErr != nil {
		return fmt.Errorf("slack upload file %s: %w", filePath, uploadErr)
	}
	return nil
}

// CheckNewMessages checks each channel in lastSeen for new messages by
// fetching the latest message via conversations.history (limit=1).
// Returns channel IDs with new messages and a map of all latest timestamps.
func (c *slackClient) CheckNewMessages(lastSeen map[string]string) ([]string, map[string]string, error) {

	api := c.primary
	if api == nil {
		api = c.fallback
	}

	var updated []string
	allLatest := make(map[string]string)

	for channelID, seenTS := range lastSeen {
		params := &slack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Limit:     1,
		}

		resp, err := api.GetConversationHistory(params)
		if err != nil {
			// Try fallback
			if c.fallback != nil && api != c.fallback {
				resp, err = c.fallback.GetConversationHistory(params)
			}
			if err != nil {
				// Skip this channel, don't fail the whole poll.
				continue
			}
		}

		if len(resp.Messages) == 0 {
			continue
		}

		latestTS := resp.Messages[0].Timestamp
		allLatest[channelID] = latestTS

		if latestTS > seenTS {
			updated = append(updated, channelID)
		}
	}

	return updated, allLatest, nil
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

// DownloadFile downloads a file from Slack's private URL to a local path.
// The context can be cancelled to abort the download.
func (c *slackClient) DownloadFile(ctx context.Context, url, destPath string) error {
	token := ""
	_ = c.tryWithFallback("download file", func(api *slack.Client) error {
		token = getClientToken(api)
		return nil
	})
	if token == "" {
		return fmt.Errorf("cannot get auth token for download")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", destPath, err)
	}
	defer out.Close()

	buf := make([]byte, 32*1024)
	for {
		// Check context before each read.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("writing file: %w", writeErr)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("reading download: %w", readErr)
		}
	}

	return nil
}

// getClientToken extracts the token from a slack.Client using reflection-free approach.
// We store tokens ourselves since slack-go doesn't expose them.
var clientTokens = make(map[*slack.Client]string)

func registerClientToken(client *slack.Client, token string) {
	clientTokens[client] = token
}

func getClientToken(client *slack.Client) string {
	return clientTokens[client]
}

// extractFiles converts slack.File attachments to types.FileInfo.
func extractFiles(files []slack.File) []types.FileInfo {
	if len(files) == 0 {
		return nil
	}
	result := make([]types.FileInfo, 0, len(files))
	for _, f := range files {
		result = append(result, types.FileInfo{
			ID:       f.ID,
			Name:     f.Name,
			Size:     int64(f.Size),
			MimeType: f.Mimetype,
			URL:      f.URLPrivateDownload,
		})
	}
	return result
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
