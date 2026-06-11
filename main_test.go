package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPercentEncode(t *testing.T) {
	tests := []struct{ in, want string }{
		{"abc", "abc"},
		{"ABC", "ABC"},
		{"123", "123"},
		{"-_.~", "-_.~"},
		{"hello world", "hello%20world"},
		{"!", "%21"},
		{"'", "%27"},
		{"(", "%28"},
		{")", "%29"},
		{"*", "%2A"},
		{"twitter.com/test", "twitter.com%2Ftest"},
		{":", "%3A"},
		{"@", "%40"},
		{"hello%20world", "hello%2520world"},
	}
	for _, tt := range tests {
		got := percentEncode(tt.in)
		if got != tt.want {
			t.Errorf("percentEncode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatTweet(t *testing.T) {
	e := feedEntry{Title: "Hello World", Link: "https://example.com"}
	tests := []struct{ tmpl, want string }{
		{"{title} {link}", "Hello World https://example.com"},
		{"[{title}]({link})", "[Hello World](https://example.com)"},
		{"{title}", "Hello World"},
		{"{link}", "https://example.com"},
		{"{title}: {link}", "Hello World: https://example.com"},
	}
	for _, tt := range tests {
		got := formatTweet(tt.tmpl, e)
		if got != tt.want {
			t.Errorf("formatTweet(%q) = %q, want %q", tt.tmpl, got, tt.want)
		}
	}
}

func TestSHA256URL(t *testing.T) {
	got := sha256URL("test")
	if len(got) != 43 {
		t.Errorf("sha256URL length = %d, want 43", len(got))
	}
	got2 := sha256URL("test")
	if got != got2 {
		t.Error("sha256URL not deterministic")
	}
}

func TestRandomBase64URL(t *testing.T) {
	got := randomBase64URL(32)
	if len(got) != 43 {
		t.Errorf("randomBase64URL(32) length = %d, want 43", len(got))
	}
	got2 := randomBase64URL(32)
	if got == got2 {
		t.Error("randomBase64URL should return different values")
	}
}

func TestGenerateNonce(t *testing.T) {
	got := generateNonce()
	if len(got) != 32 {
		t.Errorf("generateNonce length = %d, want 32", len(got))
	}
	for _, c := range got {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("generateNonce contains non-hex char: %c", c)
		}
	}
}

func TestIsUnauthorized(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("twitter API error (status 401): Unauthorized"), true},
		{fmt.Errorf("status 401: invalid token"), true},
		{fmt.Errorf("status 403: forbidden"), false},
		{fmt.Errorf("network error"), false},
		{nil, false},
	}
	for _, tt := range tests {
		got := isUnauthorized(tt.err)
		if got != tt.want {
			t.Errorf("isUnauthorized(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestReadWriteLastTimestamp(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "ts.txt")

	ts := readLastTimestamp(fp)
	if !ts.IsZero() {
		t.Error("expected zero time for missing file")
	}

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	if err := writeLastTimestamp(fp, now); err != nil {
		t.Fatalf("writeLastTimestamp: %v", err)
	}

	read := readLastTimestamp(fp)
	if !read.Equal(now) {
		t.Errorf("read %v, want %v", read, now)
	}

	os.WriteFile(fp, []byte("not-a-date"), 0644)
	read = readLastTimestamp(fp)
	if !read.IsZero() {
		t.Error("expected zero time for invalid data")
	}
}

func TestLoadSaveTokenState(t *testing.T) {
	dir := t.TempDir()
	origFile := tokenStateFile
	tokenStateFile = filepath.Join(dir, "token.json")
	defer func() { tokenStateFile = origFile }()

	_, err := loadTokenState()
	if err == nil {
		t.Error("expected error for missing file")
	}

	ts := tokenState{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		ExpiresAt:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := saveTokenState(ts); err != nil {
		t.Fatalf("saveTokenState: %v", err)
	}

	loaded, err := loadTokenState()
	if err != nil {
		t.Fatalf("loadTokenState: %v", err)
	}
	if loaded.AccessToken != ts.AccessToken || loaded.RefreshToken != ts.RefreshToken || !loaded.ExpiresAt.Equal(ts.ExpiresAt) {
		t.Errorf("loaded %+v, want %+v", loaded, ts)
	}
}

func TestEnsureValidToken(t *testing.T) {
	origFile := tokenStateFile
	dir := t.TempDir()
	tokenStateFile = filepath.Join(dir, "token.json")
	defer func() { tokenStateFile = origFile }()

	ts := tokenState{AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Now().Add(2 * time.Hour)}
	tok, err := ensureValidToken(&ts)
	if err != nil {
		t.Fatalf("ensureValidToken valid: %v", err)
	}
	if tok != "at" {
		t.Errorf("got %q, want %q", tok, "at")
	}

	ts = tokenState{AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Now().Add(30 * time.Second)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: "new-at", RefreshToken: "new-rt", ExpiresIn: 7200})
	}))
	defer srv.Close()
	origTokenURL := twitterTokenURL
	twitterTokenURL = srv.URL
	defer func() { twitterTokenURL = origTokenURL }()
	os.Setenv("TWITTER_CLIENT_ID", "cid")
	os.Setenv("TWITTER_CLIENT_SECRET", "csec")
	defer os.Unsetenv("TWITTER_CLIENT_ID")
	defer os.Unsetenv("TWITTER_CLIENT_SECRET")

	tok, err = ensureValidToken(&ts)
	if err != nil {
		t.Fatalf("ensureValidToken refresh: %v", err)
	}
	if tok != "new-at" {
		t.Errorf("got %q, want %q", tok, "new-at")
	}
	if ts.AccessToken != "new-at" || ts.RefreshToken != "new-rt" {
		t.Errorf("token state not updated: %+v", ts)
	}
}

func TestFetchFeedRSS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>First Post</title><link>https://example.com/1</link><pubDate>Mon, 01 Jan 2024 12:00:00 GMT</pubDate></item>
<item><title>Second Post</title><link>https://example.com/2</link><pubDate>Wed, 03 Jan 2024 15:00:00 GMT</pubDate></item>
</channel></rss>`)
	}))
	defer srv.Close()

	entries, err := fetchFeed(srv.URL)
	if err != nil {
		t.Fatalf("fetchFeed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Title != "First Post" {
		t.Errorf("title = %q", entries[0].Title)
	}
	if entries[0].Link != "https://example.com/1" {
		t.Errorf("link = %q", entries[0].Link)
	}
	if entries[1].Title != "Second Post" {
		t.Errorf("title = %q", entries[1].Title)
	}
}

func TestFetchFeedAtom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
<entry><title>Atom Entry</title><link href="https://example.com/a"/><published>2024-06-15T10:00:00Z</published></entry>
</feed>`)
	}))
	defer srv.Close()

	entries, err := fetchFeed(srv.URL)
	if err != nil {
		t.Fatalf("fetchFeed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Title != "Atom Entry" {
		t.Errorf("title = %q", entries[0].Title)
	}
	if entries[0].Link != "https://example.com/a" {
		t.Errorf("link = %q", entries[0].Link)
	}
}

func TestFetchFeedInvalidURL(t *testing.T) {
	_, err := fetchFeed("http://127.0.0.1:1/nonexistent")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestPostTweetOAuth2(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(401)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{"id": "12345", "text": "Hello World"},
		})
	}))
	defer srv.Close()

	origURL := twitterAPIV2
	twitterAPIV2 = srv.URL
	defer func() { twitterAPIV2 = origURL }()

	resp, err := postTweetOAuth2("test-token", "Hello World")
	if err != nil {
		t.Fatalf("postTweetOAuth2: %v", err)
	}
	if resp.Data.ID != "12345" {
		t.Errorf("id = %q, want %q", resp.Data.ID, "12345")
	}
	if resp.Data.Text != "Hello World" {
		t.Errorf("text = %q, want %q", resp.Data.Text, "Hello World")
	}
}

func TestPostTweetOAuth2Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	origURL := twitterAPIV2
	twitterAPIV2 = srv.URL
	defer func() { twitterAPIV2 = origURL }()

	_, err := postTweetOAuth2("bad-token", "test")
	if err == nil {
		t.Error("expected error for 401")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

func TestExchangeCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "cid" || pass != "csec" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "access-abc", RefreshToken: "refresh-xyz", ExpiresIn: 7200, Scope: "tweet.write",
		})
	}))
	defer srv.Close()

	origURL := twitterTokenURL
	twitterTokenURL = srv.URL
	defer func() { twitterTokenURL = origURL }()

	resp, err := exchangeCode("cid", "csec", "code123", "verifier", "http://localhost/callback")
	if err != nil {
		t.Fatalf("exchangeCode: %v", err)
	}
	if resp.AccessToken != "access-abc" {
		t.Errorf("access_token = %q", resp.AccessToken)
	}
	if resp.RefreshToken != "refresh-xyz" {
		t.Errorf("refresh_token = %q", resp.RefreshToken)
	}
	if resp.ExpiresIn != 7200 {
		t.Errorf("expires_in = %d", resp.ExpiresIn)
	}
}

func TestExchangeCodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
	}))
	defer srv.Close()

	origURL := twitterTokenURL
	twitterTokenURL = srv.URL
	defer func() { twitterTokenURL = origURL }()

	_, err := exchangeCode("cid", "csec", "bad", "v", "http://x")
	if err == nil {
		t.Error("expected error for bad code")
	}
}

func TestRefreshAccessToken(t *testing.T) {
	dir := t.TempDir()
	origFile := tokenStateFile
	tokenStateFile = filepath.Join(dir, "token.json")
	defer func() { tokenStateFile = origFile }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600,
		})
	}))
	defer srv.Close()

	origURL := twitterTokenURL
	twitterTokenURL = srv.URL
	defer func() { twitterTokenURL = origURL }()

	os.Setenv("TWITTER_CLIENT_ID", "cid")
	os.Setenv("TWITTER_CLIENT_SECRET", "csec")
	defer os.Unsetenv("TWITTER_CLIENT_ID")
	defer os.Unsetenv("TWITTER_CLIENT_SECRET")

	ts := &tokenState{AccessToken: "old", RefreshToken: "old-refresh", ExpiresAt: time.Now()}
	tok, err := refreshAccessToken(ts)
	if err != nil {
		t.Fatalf("refreshAccessToken: %v", err)
	}
	if tok != "new-access" {
		t.Errorf("got %q, want %q", tok, "new-access")
	}
	if ts.RefreshToken != "new-refresh" {
		t.Errorf("refresh token = %q", ts.RefreshToken)
	}

	loaded, _ := loadTokenState()
	if loaded.AccessToken != "new-access" {
		t.Errorf("saved token = %q", loaded.AccessToken)
	}
}

func TestRefreshAccessTokenNoCredentials(t *testing.T) {
	os.Unsetenv("TWITTER_CLIENT_ID")
	os.Unsetenv("TWITTER_CLIENT_SECRET")
	ts := &tokenState{RefreshToken: "rt"}
	_, err := refreshAccessToken(ts)
	if err == nil {
		t.Error("expected error without credentials")
	}
}

func TestRefreshAccessTokenServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	origURL := twitterTokenURL
	twitterTokenURL = srv.URL
	defer func() { twitterTokenURL = origURL }()

	os.Setenv("TWITTER_CLIENT_ID", "cid")
	os.Setenv("TWITTER_CLIENT_SECRET", "csec")
	defer os.Unsetenv("TWITTER_CLIENT_ID")
	defer os.Unsetenv("TWITTER_CLIENT_SECRET")

	ts := &tokenState{RefreshToken: "rt"}
	_, err := refreshAccessToken(ts)
	if err == nil {
		t.Error("expected error from server error")
	}
}

func TestPostTweetOAuth1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{"id": "67890", "text": "oauth1-test"},
		})
	}))
	defer srv.Close()

	origURL := twitterAPIV2
	twitterAPIV2 = srv.URL
	defer func() { twitterAPIV2 = origURL }()

	resp, err := postTweetOAuth1("k", "ks", "at", "ats", "hello")
	if err != nil {
		t.Fatalf("postTweetOAuth1: %v", err)
	}
	if resp.Data.ID != "67890" {
		t.Errorf("id = %q", resp.Data.ID)
	}
}

func TestCmdPostDryRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>T1</title><link>https://x.com/1</link><pubDate>Mon, 01 Jan 2025 12:00:00 GMT</pubDate></item>
</channel></rss>`)
	}))
	defer srv.Close()

	cmdPost([]string{"--dry-run", "--feed-url", srv.URL})
}

func TestCmdPostBearer(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>Bearer Test</title><link>https://x.com/b</link><pubDate>Mon, 01 Jan 2025 12:00:00 GMT</pubDate></item>
</channel></rss>`)
	}))
	defer feedSrv.Close()

	tweetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-bearer-env" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{"id": "999", "text": "Bearer Test"},
		})
	}))
	defer tweetSrv.Close()

	origAPI := twitterAPIV2
	twitterAPIV2 = tweetSrv.URL
	defer func() { twitterAPIV2 = origAPI }()

	os.Setenv("TWITTER_BEARER_TOKEN", "test-bearer-env")
	defer os.Unsetenv("TWITTER_BEARER_TOKEN")

	tmpDir := t.TempDir()
	cmdPost([]string{"--feed-url", feedSrv.URL, "--timestamp-file", filepath.Join(tmpDir, "ts.txt")})
}

func TestCmdPostDryRunCustomTemplate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>Custom</title><link>https://x.com/c</link><pubDate>Mon, 01 Jan 2025 12:00:00 GMT</pubDate></item>
</channel></rss>`)
	}))
	defer srv.Close()

	cmdPost([]string{"--dry-run", "--feed-url", srv.URL, "--template", "[{title}]({link})"})
}

func TestOpenBrowser(t *testing.T) {
	openBrowser("http://example.com")
}

func TestExchangeCodeBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	origURL := twitterTokenURL
	twitterTokenURL = srv.URL
	defer func() { twitterTokenURL = origURL }()

	_, err := exchangeCode("cid", "csec", "code", "verifier", "http://localhost/callback")
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

func TestRefreshAccessTokenBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{{{"))
	}))
	defer srv.Close()

	origURL := twitterTokenURL
	twitterTokenURL = srv.URL
	defer func() { twitterTokenURL = origURL }()

	os.Setenv("TWITTER_CLIENT_ID", "cid")
	os.Setenv("TWITTER_CLIENT_SECRET", "csec")
	defer os.Unsetenv("TWITTER_CLIENT_ID")
	defer os.Unsetenv("TWITTER_CLIENT_SECRET")

	ts := &tokenState{RefreshToken: "rt"}
	_, err := refreshAccessToken(ts)
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

func TestFetchFeedNoDateFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>No Date</title><link>https://x.com/nd</link></item>
</channel></rss>`)
	}))
	defer srv.Close()

	entries, err := fetchFeed(srv.URL)
	if err != nil {
		t.Fatalf("fetchFeed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].Link != "https://x.com/nd" {
		t.Errorf("link = %q", entries[0].Link)
	}
}

func TestFetchFeedNoLinkFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>GUID Link</title><guid>https://x.com/guid</guid><pubDate>Mon, 01 Jan 2025 12:00:00 GMT</pubDate></item>
</channel></rss>`)
	}))
	defer srv.Close()

	entries, err := fetchFeed(srv.URL)
	if err != nil {
		t.Fatalf("fetchFeed: %v", err)
	}
	if entries[0].Link != "https://x.com/guid" {
		t.Errorf("link = %q", entries[0].Link)
	}
}

func TestCmdPostWithTokenState(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>State Test</title><link>https://x.com/s</link><pubDate>Mon, 01 Jan 2025 12:00:00 GMT</pubDate></item>
</channel></rss>`)
	}))
	defer feedSrv.Close()

	tweetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{"id": "111", "text": "State Test"},
		})
	}))
	defer tweetSrv.Close()

	origAPI := twitterAPIV2
	twitterAPIV2 = tweetSrv.URL
	defer func() { twitterAPIV2 = origAPI }()

	tmpDir := t.TempDir()
	origFile := tokenStateFile
	tokenStateFile = filepath.Join(tmpDir, "token.json")
	defer func() { tokenStateFile = origFile }()

	ts := tokenState{AccessToken: "state-tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(2 * time.Hour)}
	saveTokenState(ts)

	cmdPost([]string{"--feed-url", feedSrv.URL, "--timestamp-file", filepath.Join(tmpDir, "ts.txt")})
}

func TestCmdPostAllOld(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>Old</title><link>https://x.com/o</link><pubDate>Mon, 01 Jan 2020 12:00:00 GMT</pubDate></item>
</channel></rss>`)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	tsFile := filepath.Join(tmpDir, "ts.txt")
	writeLastTimestamp(tsFile, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	cmdPost([]string{"--dry-run", "--feed-url", srv.URL, "--timestamp-file", tsFile})
}

func TestPostTweetOAuth2BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	origURL := twitterAPIV2
	twitterAPIV2 = srv.URL
	defer func() { twitterAPIV2 = origURL }()

	_, err := postTweetOAuth2("test-token", "hello")
	if err == nil {
		t.Error("expected error for bad JSON response")
	}
}

func TestCmdPostOAuth1(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>OAuth1 Test</title><link>https://x.com/o1</link><pubDate>Mon, 01 Jan 2025 12:00:00 GMT</pubDate></item>
</channel></rss>`)
	}))
	defer feedSrv.Close()

	tweetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{"id": "222", "text": "OAuth1 Test"},
		})
	}))
	defer tweetSrv.Close()

	origAPI := twitterAPIV2
	twitterAPIV2 = tweetSrv.URL
	defer func() { twitterAPIV2 = origAPI }()

	os.Setenv("TWITTER_CONSUMER_KEY", "ck")
	os.Setenv("TWITTER_CONSUMER_SECRET", "cs")
	os.Setenv("TWITTER_ACCESS_TOKEN", "at")
	os.Setenv("TWITTER_ACCESS_TOKEN_SECRET", "ats")
	defer os.Unsetenv("TWITTER_CONSUMER_KEY")
	defer os.Unsetenv("TWITTER_CONSUMER_SECRET")
	defer os.Unsetenv("TWITTER_ACCESS_TOKEN")
	defer os.Unsetenv("TWITTER_ACCESS_TOKEN_SECRET")

	os.Unsetenv("TWITTER_BEARER_TOKEN")

	tmpDir := t.TempDir()
	tokenStateFile = filepath.Join(tmpDir, "nonexistent.json")
	cmdPost([]string{"--feed-url", feedSrv.URL, "--timestamp-file", filepath.Join(tmpDir, "ts.txt")})
}

func TestPostTweetOAuth2NetworkError(t *testing.T) {
	origURL := twitterAPIV2
	twitterAPIV2 = "http://127.0.0.1:1/tweets"
	defer func() { twitterAPIV2 = origURL }()

	_, err := postTweetOAuth2("tok", "hello")
	if err == nil {
		t.Error("expected network error")
	}
}

func TestPostTweetOAuth1BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer srv.Close()

	origURL := twitterAPIV2
	twitterAPIV2 = srv.URL
	defer func() { twitterAPIV2 = origURL }()

	_, err := postTweetOAuth1("k", "ks", "at", "ats", "hello")
	if err == nil {
		t.Error("expected error for 403")
	}
	if !strings.Contains(err.Error(), "status 403") {
		t.Errorf("expected 403 error, got %v", err)
	}
}

func TestPostTweetOAuth1NetworkError(t *testing.T) {
	origURL := twitterAPIV2
	twitterAPIV2 = "http://127.0.0.1:1/tweets"
	defer func() { twitterAPIV2 = origURL }()

	_, err := postTweetOAuth1("k", "ks", "at", "ats", "hello")
	if err == nil {
		t.Error("expected network error")
	}
}

func TestExchangeCodeNetworkError(t *testing.T) {
	origURL := twitterTokenURL
	twitterTokenURL = "http://127.0.0.1:1/token"
	defer func() { twitterTokenURL = origURL }()

	_, err := exchangeCode("cid", "csec", "code", "verifier", "http://localhost/callback")
	if err == nil {
		t.Error("expected network error")
	}
}

func TestRefreshAccessTokenNetworkError(t *testing.T) {
	origURL := twitterTokenURL
	twitterTokenURL = "http://127.0.0.1:1/token"
	defer func() { twitterTokenURL = origURL }()

	os.Setenv("TWITTER_CLIENT_ID", "cid")
	os.Setenv("TWITTER_CLIENT_SECRET", "csec")
	defer os.Unsetenv("TWITTER_CLIENT_ID")
	defer os.Unsetenv("TWITTER_CLIENT_SECRET")

	ts := &tokenState{RefreshToken: "rt"}
	_, err := refreshAccessToken(ts)
	if err == nil {
		t.Error("expected network error")
	}
}

func TestMainDefaultCmd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>Main Test</title><link>https://x.com/m</link><pubDate>Mon, 01 Jan 2020 12:00:00 GMT</pubDate></item>
</channel></rss>`)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	tsFile := filepath.Join(tmpDir, "ts.txt")
	writeLastTimestamp(tsFile, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	oldArgs := os.Args
	os.Args = []string{"twitter-poster", "--dry-run", "--feed-url", srv.URL, "--timestamp-file", tsFile}
	defer func() { os.Args = oldArgs }()

	main()
}

func TestMainPostSubcommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>Main Post</title><link>https://x.com/mp</link><pubDate>Mon, 01 Jan 2020 12:00:00 GMT</pubDate></item>
</channel></rss>`)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	tsFile := filepath.Join(tmpDir, "ts.txt")
	writeLastTimestamp(tsFile, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	oldArgs := os.Args
	os.Args = []string{"twitter-poster", "post", "--dry-run", "--feed-url", srv.URL, "--timestamp-file", tsFile}
	defer func() { os.Args = oldArgs }()

	main()
}

func TestRunGetTokenStateMismatch(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "tok", RefreshToken: "rt", ExpiresIn: 7200,
		})
	}))
	defer tokenSrv.Close()

	origTokenURL := twitterTokenURL
	twitterTokenURL = tokenSrv.URL
	defer func() { twitterTokenURL = origTokenURL }()

	tmpDir := t.TempDir()
	origFile := tokenStateFile
	tokenStateFile = filepath.Join(tmpDir, "tok.json")
	defer func() { tokenStateFile = origFile }()

	os.Setenv("TWITTER_CLIENT_ID", "cid")
	os.Setenv("TWITTER_CLIENT_SECRET", "csec")
	defer os.Unsetenv("TWITTER_CLIENT_ID")
	defer os.Unsetenv("TWITTER_CLIENT_SECRET")

	done := make(chan error, 1)
	go func() {
		done <- runGetToken("cid", "csec", "http://localhost:17890/callback", true, 17890)
	}()

	time.Sleep(200 * time.Millisecond)

	resp, err := http.DefaultClient.Get("http://localhost:17890/callback?code=x&state=wrong")
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	resp.Body.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error for state mismatch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runGetToken did not complete")
	}
}

func TestRunGetTokenOAuthError(t *testing.T) {
	os.Setenv("TWITTER_CLIENT_ID", "cid")
	os.Setenv("TWITTER_CLIENT_SECRET", "csec")
	defer os.Unsetenv("TWITTER_CLIENT_ID")
	defer os.Unsetenv("TWITTER_CLIENT_SECRET")

	done := make(chan error, 1)
	go func() {
		done <- runGetToken("cid", "csec", "http://localhost:17891/callback", true, 17891)
	}()

	time.Sleep(200 * time.Millisecond)

	resp, err := http.DefaultClient.Get("http://localhost:17891/callback?error=access_denied&error_description=User+denied")
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	resp.Body.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error for oauth denial")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runGetToken did not complete")
	}
}

func TestRunServe(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		done <- runServe("cid", "csec", "http://localhost:17892/callback", 17892)
	}()

	time.Sleep(200 * time.Millisecond)

	_, err := http.DefaultClient.Get("http://localhost:17892/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
}

func TestLoadTokenStateBadJSON(t *testing.T) {
	dir := t.TempDir()
	origFile := tokenStateFile
	tokenStateFile = filepath.Join(dir, "bad.json")
	defer func() { tokenStateFile = origFile }()

	os.WriteFile(tokenStateFile, []byte("not json"), 0644)
	_, err := loadTokenState()
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

func TestSaveTokenStateWriteError(t *testing.T) {
	origFile := tokenStateFile
	tokenStateFile = "/nonexistent/path/.twitter_token.json"
	defer func() { tokenStateFile = origFile }()

	ts := tokenState{AccessToken: "at"}
	err := saveTokenState(ts)
	if err == nil {
		t.Error("expected error for invalid path")
	}
}
