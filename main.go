package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/mmcdole/gofeed"
)

var (
	twitterAPIV2    = "https://api.twitter.com/2/tweets"
	twitterTokenURL = "https://api.twitter.com/2/oauth2/token"
	httpClient      = &http.Client{Timeout: 30 * time.Second}
	tokenStateFile  = ".twitter_token.json"
)

const (
	twitterAuthURL = "https://twitter.com/i/oauth2/authorize"
)

type tweetResponse struct {
	Data struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"data"`
}

type feedEntry struct {
	Title     string
	Link      string
	Published time.Time
}

type tokenState struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func main() {
	_ = godotenv.Load()

	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "get-token":
			cmdGetToken(args[1:])
			return
		case "serve":
			cmdServe(args[1:])
			return
		case "post":
			cmdPost(args[1:])
			return
		case "help", "-h", "--help":
			printUsage()
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", args[0])
			printUsage()
			os.Exit(1)
		}
	}
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		printUsage()
		return
	}
	cmdPost(args)
}

func printUsage() {
	fmt.Println(`twitter-poster — post new RSS/Atom feed items to Twitter

Usage:
  twitter-poster [command] [flags]

Commands:
  post        Fetch RSS/Atom feed and post new items (default)
  get-token   OAuth 2.0 PKCE flow — obtain access token
  serve       Persistent OAuth 2.0 auth server
  help        Show this help

Run 'twitter-poster <command> --help' for command-specific flags.`)
}

func cmdPost(args []string) {
	fs := flag.NewFlagSet("post", flag.ExitOnError)
	feedURL := fs.String("feed-url", "https://www.blognone.com/node/feed", "URL of RSS/Atom feed to fetch")
	template := fs.String("template", "{title} {link}", "Tweet format. Available: {title}, {link}")
	timestampFile := fs.String("timestamp-file", "last_timestamp.txt", "Path to last timestamp file")
	tokenFile := fs.String("token-file", ".twitter_token.json", "Path to token state file")
	dryRun := fs.Bool("dry-run", false, "Print what would be posted without actually posting")
	maxPosts := fs.Int("max-posts", 0, "Max tweets to post per run (0 = unlimited)")
	fs.Parse(args)

	tokenStateFile = *tokenFile

	log.Printf("Fetching feed: %s", *feedURL)

	entries, err := fetchFeed(*feedURL)
	if err != nil {
		log.Fatalf("Failed to fetch feed: %v", err)
	}

	lastTS := readLastTimestamp(*timestampFile)
	if lastTS.IsZero() {
		log.Println("No previous timestamp found — will consider all feed items as new")
	} else {
		log.Printf("Last posted: %s", lastTS.Format(time.RFC3339))
	}

	var newEntries []feedEntry
	for _, e := range entries {
		if e.Published.After(lastTS) {
			newEntries = append(newEntries, e)
		}
	}

	if len(newEntries) == 0 {
		log.Println("No new items to post")
		return
	}

	log.Printf("Found %d new item(s) out of %d total", len(newEntries), len(entries))

	sort.Slice(newEntries, func(i, j int) bool {
		return newEntries[i].Published.Before(newEntries[j].Published)
	})

	if *dryRun {
		log.Println("=== DRY RUN ===")
		shown := 0
		for _, e := range newEntries {
			log.Printf("Would post: %q (published: %s)", formatTweet(*template, e), e.Published.Format(time.RFC3339))
			shown++
			if *maxPosts > 0 && shown >= *maxPosts {
				log.Printf("(stopping at limit: %d of %d items)", *maxPosts, len(newEntries))
				break
			}
		}
		return
	}

	ts, err := loadTokenState()
	if err == nil {
		bearerToken, err := ensureValidToken(ts)
		if err != nil {
			log.Fatalf("Token refresh failed: %v. Run 'twitter-poster get-token' to re-authenticate.", err)
		}
		posted := 0
		for _, e := range newEntries {
			text := formatTweet(*template, e)
			resp, postErr := postTweetOAuth2(bearerToken, text)
			if postErr != nil {
				if isUnauthorized(postErr) {
					var refreshErr error
					bearerToken, refreshErr = refreshAccessToken(ts)
					if refreshErr != nil {
						log.Printf("Failed to refresh token: %v", refreshErr)
						break
					}
					resp, postErr = postTweetOAuth2(bearerToken, text)
				}
				if postErr != nil {
					if isDuplicateContent(postErr) {
						log.Printf("Skipping duplicate: %q", text)
						if err := writeLastTimestamp(*timestampFile, e.Published); err != nil {
							log.Printf("Warning: could not save timestamp: %v", err)
						}
						continue
					}
					log.Printf("Failed to post tweet: %v", postErr)
					break
				}
			}
			log.Printf("Posted: %q (ID: %s)", resp.Data.Text, resp.Data.ID)
			if err := writeLastTimestamp(*timestampFile, e.Published); err != nil {
				log.Printf("Warning: could not save timestamp: %v", err)
			}
			posted++
			if *maxPosts > 0 && posted >= *maxPosts {
				log.Printf("Reached limit of %d posts, %d remaining for next run", *maxPosts, len(newEntries)-posted)
				break
			}
		}
		log.Printf("Done. Posted %d tweet(s)", posted)
		return
	}
	log.Println("No token state found, checking env for bearer token...")

	bearerToken := os.Getenv("TWITTER_BEARER_TOKEN")
	if bearerToken != "" {
		posted := 0
		for _, e := range newEntries {
			text := formatTweet(*template, e)
			_, err := postTweetOAuth2(bearerToken, text)
			if err != nil {
				if isDuplicateContent(err) {
					log.Printf("Skipping duplicate: %q", text)
					if err := writeLastTimestamp(*timestampFile, e.Published); err != nil {
						log.Printf("Warning: could not save timestamp: %v", err)
					}
					continue
				}
				log.Printf("Failed to post tweet: %v", err)
				break
			}
			if err := writeLastTimestamp(*timestampFile, e.Published); err != nil {
				log.Printf("Warning: could not save timestamp: %v", err)
			}
			posted++
			if *maxPosts > 0 && posted >= *maxPosts {
				log.Printf("Reached limit of %d posts, %d remaining for next run", *maxPosts, len(newEntries)-posted)
				break
			}
		}
		log.Printf("Done. Posted %d tweet(s)", posted)
		return
	}

	consumerKey := os.Getenv("TWITTER_CONSUMER_KEY")
	consumerSecret := os.Getenv("TWITTER_CONSUMER_SECRET")
	accessToken := os.Getenv("TWITTER_ACCESS_TOKEN")
	accessTokenSecret := os.Getenv("TWITTER_ACCESS_TOKEN_SECRET")

	if consumerKey == "" || consumerSecret == "" || accessToken == "" || accessTokenSecret == "" {
		log.Fatal("Missing credentials. Set TWITTER_BEARER_TOKEN (OAuth 2.0) or TWITTER_CONSUMER_KEY, TWITTER_CONSUMER_SECRET, TWITTER_ACCESS_TOKEN, TWITTER_ACCESS_TOKEN_SECRET (OAuth 1.0a) in .env")
	}

	posted := 0
	for _, e := range newEntries {
		text := formatTweet(*template, e)
		resp, err := postTweetOAuth1(consumerKey, consumerSecret, accessToken, accessTokenSecret, text)
		if err != nil {
			if isDuplicateContent(err) {
				log.Printf("Skipping duplicate: %q", text)
				if err := writeLastTimestamp(*timestampFile, e.Published); err != nil {
					log.Printf("Warning: could not save timestamp: %v", err)
				}
				continue
			}
			log.Printf("Failed to post tweet: %v", err)
			log.Println("Stopping to preserve chronological order. Already posted items are tracked.")
			break
		}
		log.Printf("Posted: %q (ID: %s)", resp.Data.Text, resp.Data.ID)
		if err := writeLastTimestamp(*timestampFile, e.Published); err != nil {
			log.Printf("Warning: could not save timestamp: %v", err)
		}
		posted++
		if *maxPosts > 0 && posted >= *maxPosts {
			log.Printf("Reached limit of %d posts, %d remaining for next run", *maxPosts, len(newEntries)-posted)
			break
		}
	}
	log.Printf("Done. Posted %d tweet(s)", posted)
}

func fetchFeed(urlStr string) ([]feedEntry, error) {
	parser := gofeed.NewParser()
	feed, err := parser.ParseURL(urlStr)
	if err != nil {
		return nil, fmt.Errorf("parsing feed: %w", err)
	}

	var entries []feedEntry
	for _, item := range feed.Items {
		published := item.PublishedParsed
		if published == nil {
			published = item.UpdatedParsed
		}
		if published == nil {
			now := time.Now()
			published = &now
			log.Printf("No publish date for %q, using current time", item.Title)
		}

		link := item.Link
		if link == "" {
			link = item.GUID
		}

		entries = append(entries, feedEntry{
			Title:     item.Title,
			Link:      link,
			Published: *published,
		})
	}
	return entries, nil
}

func formatTweet(tmpl string, e feedEntry) string {
	s := tmpl
	s = strings.ReplaceAll(s, "{title}", e.Title)
	s = strings.ReplaceAll(s, "{link}", e.Link)
	return s
}

func cmdGetToken(args []string) {
	fs := flag.NewFlagSet("get-token", flag.ExitOnError)
	port := fs.Int("port", 8080, "Local port for OAuth callback")
	noBrowser := fs.Bool("no-browser", false, "Don't open browser automatically")
	fs.Parse(args)

	clientID := os.Getenv("TWITTER_CLIENT_ID")
	clientSecret := os.Getenv("TWITTER_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		log.Fatal("Missing TWITTER_CLIENT_ID and/or TWITTER_CLIENT_SECRET in .env or environment")
	}

	if err := runGetToken(clientID, clientSecret, fmt.Sprintf("http://localhost:%d/callback", *port), *noBrowser, *port); err != nil {
		log.Fatal(err)
	}
}

func runGetToken(clientID, clientSecret, redirectURI string, noBrowser bool, port int) error {
	codeVerifier := randomBase64URL(32)
	codeChallenge := sha256URL(codeVerifier)
	state := randomBase64URL(16)

	authURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&code_challenge=%s&code_challenge_method=S256&prompt=login",
		twitterAuthURL,
		clientID,
		url.QueryEscape(redirectURI),
		url.QueryEscape("tweet.read tweet.write users.read offline.access"),
		state,
		codeChallenge,
	)

	done := make(chan struct {
		code  string
		state string
		err   error
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := q.Get("code")
		recvState := q.Get("state")
		errStr := q.Get("error")

		if errStr != "" {
			desc := q.Get("error_description")
			fmt.Fprintf(w, "Authorization failed: %s - %s", errStr, desc)
			done <- struct {
				code  string
				state string
				err   error
			}{err: fmt.Errorf("%s: %s", errStr, desc)}
			return
		}

		if recvState != state {
			fmt.Fprint(w, "State mismatch. Possible CSRF attack.")
			done <- struct {
				code  string
				state string
				err   error
			}{err: fmt.Errorf("state mismatch")}
			return
		}

		fmt.Fprint(w, "Authorization successful. You can close this window.")
		done <- struct {
			code  string
			state string
			err   error
		}{code: code, state: recvState}
	})

	server := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	log.Printf("Opening browser to authorize...")
	log.Printf("URL: %s", authURL)

	if !noBrowser {
		openBrowser(authURL)
	} else {
		log.Println("Visit the URL above to authorize.")
	}

	result := <-done
	server.Close()

	if result.err != nil {
		return fmt.Errorf("authorization failed: %w", result.err)
	}

	tokenResp, err := exchangeCode(clientID, clientSecret, result.code, codeVerifier, redirectURI)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	ts := tokenState{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	if err := saveTokenState(ts); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	fmt.Println()
	fmt.Println("=== Token acquired ===")
	fmt.Printf("Access Token:  %s\n", tokenResp.AccessToken)
	if tokenResp.RefreshToken != "" {
		fmt.Printf("Refresh Token: %s\n", tokenResp.RefreshToken)
	}
	fmt.Printf("Expires In:    %d seconds\n", tokenResp.ExpiresIn)
	fmt.Printf("Scope:         %s\n", tokenResp.Scope)
	fmt.Println()
	fmt.Println("Token saved to .twitter_token.json — post command will auto-refresh when expired.")
	return nil
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8000, "Server port")
	fs.Parse(args)

	clientID := os.Getenv("TWITTER_CLIENT_ID")
	clientSecret := os.Getenv("TWITTER_CLIENT_SECRET")
	redirectURI := os.Getenv("TWITTER_REDIRECT_URI")

	if clientID == "" || clientSecret == "" || redirectURI == "" {
		log.Fatal("Missing TWITTER_CLIENT_ID, TWITTER_CLIENT_SECRET, or TWITTER_REDIRECT_URI in .env")
	}

	log.Fatal(runServe(clientID, clientSecret, redirectURI, *port))
}

func runServe(clientID, clientSecret, redirectURI string, port int) error {
	states := make(map[string]string)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		codeVerifier := randomBase64URL(32)
		codeChallenge := sha256URL(codeVerifier)
		state := randomBase64URL(16)
		states[state] = codeVerifier

		authURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&code_challenge=%s&code_challenge_method=S256&prompt=login",
			twitterAuthURL,
			clientID,
			url.QueryEscape(redirectURI),
			url.QueryEscape("tweet.read tweet.write users.read offline.access"),
			state,
			codeChallenge,
		)
		http.Redirect(w, r, authURL, http.StatusFound)
	})

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := q.Get("code")
		state := q.Get("state")
		errStr := q.Get("error")

		if errStr != "" {
			http.Error(w, fmt.Sprintf("Authorization failed: %s - %s", errStr, q.Get("error_description")), http.StatusBadRequest)
			return
		}

		verifier, ok := states[state]
		if !ok {
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}
		delete(states, state)

		tokenResp, err := exchangeCode(clientID, clientSecret, code, verifier, redirectURI)
		if err != nil {
			http.Error(w, fmt.Sprintf("Token exchange failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":       "Token acquired successfully!",
			"access_token":  tokenResp.AccessToken,
			"refresh_token": tokenResp.RefreshToken,
			"expires_in":    tokenResp.ExpiresIn,
			"scope":         tokenResp.Scope,
			"instruction":   "Add TWITTER_BEARER_TOKEN=<access_token> to your .env file.",
		})
	})

	addr := fmt.Sprintf(":%d", port)
	return http.ListenAndServe(addr, mux)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

func exchangeCode(clientID, clientSecret, code, codeVerifier, redirectURI string) (*tokenResponse, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {codeVerifier},
	}

	req, err := http.NewRequest("POST", twitterTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

func loadTokenState() (*tokenState, error) {
	data, err := os.ReadFile(tokenStateFile)
	if err != nil {
		return nil, err
	}
	var ts tokenState
	if err := json.Unmarshal(data, &ts); err != nil {
		return nil, err
	}
	return &ts, nil
}

func saveTokenState(ts tokenState) error {
	data, err := json.MarshalIndent(ts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tokenStateFile, data, 0600)
}

func ensureValidToken(ts *tokenState) (string, error) {
	if time.Now().Before(ts.ExpiresAt.Add(-60 * time.Second)) {
		return ts.AccessToken, nil
	}
	return refreshAccessToken(ts)
}

func refreshAccessToken(ts *tokenState) (string, error) {
	clientID := os.Getenv("TWITTER_CLIENT_ID")
	clientSecret := os.Getenv("TWITTER_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("TWITTER_CLIENT_ID and TWITTER_CLIENT_SECRET required for token refresh")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {ts.RefreshToken},
	}

	req, err := http.NewRequest("POST", twitterTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", err
	}

	ts.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		ts.RefreshToken = tr.RefreshToken
	}
	ts.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)

	if err := saveTokenState(*ts); err != nil {
		log.Printf("Warning: could not save refreshed token: %v", err)
	}

	log.Println("Access token refreshed successfully")
	return ts.AccessToken, nil
}

func isUnauthorized(err error) bool {
	return err != nil && strings.Contains(err.Error(), "status 401")
}

func isDuplicateContent(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate content")
}

func randomBase64URL(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha256URL(s string) string {
	h := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func openBrowser(urlStr string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", urlStr)
	case "linux":
		cmd = exec.Command("xdg-open", urlStr)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", urlStr)
	default:
		log.Printf("Cannot open browser automatically. Visit: %s", urlStr)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to open browser: %v. Visit: %s", err, urlStr)
	}
}

func readLastTimestamp(file string) time.Time {
	data, err := os.ReadFile(file)
	if err != nil {
		return time.Time{}
	}
	ts := strings.TrimSpace(string(data))
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}
	}
	return t
}

func writeLastTimestamp(file string, t time.Time) error {
	return os.WriteFile(file, []byte(t.Format(time.RFC3339)+"\n"), 0644)
}

func percentEncode(s string) string {
	var buf strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '~' {
			buf.WriteRune(r)
			continue
		}
		for _, b := range []byte(string(r)) {
			buf.WriteString(fmt.Sprintf("%%%02X", b))
		}
	}
	return buf.String()
}

func generateNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func postTweetOAuth2(bearerToken, text string) (*tweetResponse, error) {
	body := map[string]string{"text": text}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling body: %w", err)
	}

	req, err := http.NewRequest("POST", twitterAPIV2, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != 201 {
		return nil, fmt.Errorf("twitter API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var tweetResp tweetResponse
	if err := json.Unmarshal(respBody, &tweetResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return &tweetResp, nil
}

func postTweetOAuth1(consumerKey, consumerSecret, accessToken, accessTokenSecret, text string) (*tweetResponse, error) {
	oauthParams := map[string]string{
		"oauth_consumer_key":     consumerKey,
		"oauth_nonce":            generateNonce(),
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        fmt.Sprintf("%d", time.Now().Unix()),
		"oauth_token":            accessToken,
		"oauth_version":          "1.0",
	}

	keys := make([]string, 0, len(oauthParams))
	for k := range oauthParams {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var paramParts []string
	for _, k := range keys {
		paramParts = append(paramParts, fmt.Sprintf("%s=%s", percentEncode(k), percentEncode(oauthParams[k])))
	}
	paramString := strings.Join(paramParts, "&")

	sigBase := fmt.Sprintf("POST&%s&%s", percentEncode(twitterAPIV2), percentEncode(paramString))
	signingKey := fmt.Sprintf("%s&%s", percentEncode(consumerSecret), percentEncode(accessTokenSecret))

	mac := hmac.New(sha1.New, []byte(signingKey))
	mac.Write([]byte(sigBase))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	oauthParams["oauth_signature"] = signature

	keys = keys[:0]
	for k := range oauthParams {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var authParts []string
	for _, k := range keys {
		authParts = append(authParts, fmt.Sprintf(`%s="%s"`, percentEncode(k), percentEncode(oauthParams[k])))
	}
	authHeader := "OAuth " + strings.Join(authParts, ", ")

	body := map[string]string{"text": text}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling body: %w", err)
	}

	req, err := http.NewRequest("POST", twitterAPIV2, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != 201 {
		return nil, fmt.Errorf("twitter API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var tweetResp tweetResponse
	if err := json.Unmarshal(respBody, &tweetResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return &tweetResp, nil
}
