// Package auth handles Slack OAuth2 browser-based authentication.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// OAuthResult contains the tokens obtained from the OAuth flow.
type OAuthResult struct {
	BotToken  string // xoxb-...
	UserToken string // xoxp-...
	TeamName  string
	TeamID    string
	AppID     string
}

// OAuthConfig holds the credentials needed for the OAuth flow.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	// ExpectedTeamID, if set, is verified against the team ID returned by Slack
	// after the OAuth exchange. A mismatch aborts the flow, preventing a
	// malicious team config from routing tokens through an attacker's app.
	ExpectedTeamID string
	// Bot scopes requested for the bot token.
	BotScopes []string
	// User scopes requested for the user token.
	UserScopes []string
}

// slackOAuthResponse represents the response from oauth.v2.access.
type slackOAuthResponse struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error"`
	AppID       string `json:"app_id"`
	AccessToken string `json:"access_token"` // bot token
	Team        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"team"`
	AuthedUser struct {
		AccessToken string `json:"access_token"` // user token
	} `json:"authed_user"`
}

// DefaultBotScopes returns the standard bot scopes for Slackers.
func DefaultBotScopes() []string {
	return []string{
		"channels:read", "channels:history", "channels:join", "channels:manage",
		"groups:read", "groups:history", "groups:write",
		"im:read", "im:history", "im:write",
		"mpim:read", "mpim:history", "mpim:write",
		"chat:write", "chat:write.customize", "chat:write.public",
		"reactions:read", "reactions:write",
		"pins:read", "pins:write",
		"files:read", "files:write",
		"users:read", "users:read.email", "users.profile:read", "users:write",
		"bookmarks:read", "bookmarks:write",
		"usergroups:read", "usergroups:write",
		"team:read", "emoji:read", "commands",
	}
}

// DefaultUserScopes returns the standard user scopes for Slackers.
func DefaultUserScopes() []string {
	return []string{
		"channels:read", "channels:history", "channels:write",
		"groups:read", "groups:history",
		"im:read", "im:history", "mpim:read", "mpim:history",
		"chat:write",
		"reactions:read", "reactions:write",
		"files:read", "files:write",
		"search:read", "stars:read", "stars:write",
		"users:read", "users.profile:read", "users.profile:write",
		"dnd:read", "dnd:write",
		"reminders:read", "reminders:write",
		"identify", "emoji:read", "team:read", "pins:read",
	}
}

// RunOAuthFlow starts a local HTTP server, opens the browser to Slack's
// OAuth consent page, and waits for the callback with the authorization code.
// It then exchanges the code for tokens and returns the result.
func RunOAuthFlow(cfg OAuthConfig) (*OAuthResult, error) {
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("client_id is required for OAuth flow")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("client_secret is required for OAuth flow")
	}

	if len(cfg.BotScopes) == 0 {
		cfg.BotScopes = DefaultBotScopes()
	}
	if len(cfg.UserScopes) == 0 {
		cfg.UserScopes = DefaultUserScopes()
	}

	// Try a fixed set of ports so the redirect URL can be pre-registered with Slack.
	ports := []int{9876, 9877, 9878}
	var listener net.Listener
	var port int
	for _, p := range ports {
		l, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", p))
		if err == nil {
			listener = l
			port = p
			break
		}
	}
	if listener == nil {
		return nil, fmt.Errorf("failed to start local server: ports %v all in use", ports)
	}
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	// Channel to receive the auth code.
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		errParam := r.URL.Query().Get("error")

		if errParam != "" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, successPage("Authorization denied: "+errParam, false))
			errCh <- fmt.Errorf("oauth denied: %s", errParam)
			return
		}

		if code == "" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, successPage("No authorization code received.", false))
			errCh <- fmt.Errorf("no authorization code in callback")
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, successPage("Authorization successful! You can close this tab and return to your terminal.", true))
		codeCh <- code
	})

	server := &http.Server{Handler: mux}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("local server error: %w", err)
		}
	}()

	// Build the Slack OAuth URL.
	authURL := fmt.Sprintf(
		"https://slack.com/oauth/v2/authorize?client_id=%s&scope=%s&user_scope=%s&redirect_uri=%s",
		url.QueryEscape(cfg.ClientID),
		url.QueryEscape(strings.Join(cfg.BotScopes, ",")),
		url.QueryEscape(strings.Join(cfg.UserScopes, ",")),
		url.QueryEscape(redirectURI),
	)

	fmt.Println()
	fmt.Println("Opening your browser to authorize Slackers with Slack...")
	fmt.Println()
	fmt.Println("If the browser doesn't open, visit this URL manually:")
	fmt.Println(authURL)
	fmt.Println()
	fmt.Println("Waiting for authorization...")

	// Open browser.
	openBrowser(authURL)

	// Wait for the callback (with timeout).
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		shutdownServer(server)
		return nil, err
	case <-time.After(5 * time.Minute):
		shutdownServer(server)
		return nil, fmt.Errorf("timed out waiting for authorization (5 minutes)")
	}

	// Brief pause so the browser receives the success page before we close the socket.
	time.Sleep(500 * time.Millisecond)
	shutdownServer(server)

	// Exchange the code for tokens.
	fmt.Println("Exchanging authorization code for tokens...")
	return exchangeCode(cfg.ClientID, cfg.ClientSecret, code, redirectURI, cfg.ExpectedTeamID)
}

// shutdownServer gracefully shuts down the local callback server with a short deadline.
func shutdownServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

// exchangeCode exchanges an authorization code for OAuth tokens.
// If expectedTeamID is non-empty, the team ID in the response must match or the
// exchange is rejected — this prevents a malicious team config from silently
// routing tokens through a different Slack app.
func exchangeCode(clientID, clientSecret, code, redirectURI, expectedTeamID string) (*OAuthResult, error) {
	data := url.Values{
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
	}

	resp, err := http.PostForm("https://slack.com/api/oauth.v2.access", data)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	var oauthResp slackOAuthResponse
	if err := json.Unmarshal(body, &oauthResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	if !oauthResp.OK {
		return nil, fmt.Errorf("slack oauth error: %s", oauthResp.Error)
	}

	// Verify workspace identity when an expected team ID was provided.
	if expectedTeamID != "" && oauthResp.Team.ID != expectedTeamID {
		return nil, fmt.Errorf(
			"team ID mismatch: expected %s but got %s (%s) — aborting to prevent token theft",
			expectedTeamID, oauthResp.Team.ID, oauthResp.Team.Name,
		)
	}

	return &OAuthResult{
		BotToken: oauthResp.AccessToken,
		UserToken: oauthResp.AuthedUser.AccessToken,
		TeamName: oauthResp.Team.Name,
		TeamID:   oauthResp.Team.ID,
		AppID:    oauthResp.AppID,
	}, nil
}

// openBrowser opens a URL in the user's default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// successPage returns an HTML page shown after the OAuth callback.
func successPage(message string, success bool) string {
	color := "#e74c3c"
	icon := "&#10060;"
	if success {
		color = "#2ecc71"
		icon = "&#10004;"
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Slackers</title></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #1a1a2e; color: #eee;">
  <div style="text-align: center; padding: 2em;">
    <div style="font-size: 3em; color: %s;">%s</div>
    <h1 style="margin-top: 0.5em;">Slackers</h1>
    <p style="font-size: 1.2em; color: #ccc;">%s</p>
  </div>
</body>
</html>`, color, icon, message)
}
