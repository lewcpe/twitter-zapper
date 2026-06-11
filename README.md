# Twitter Feed Poster

Post new items from an RSS/Atom feed to Twitter (X) API v2. Written in Go.

## Quick Start

```bash
# Get OAuth 2.0 access token (opens browser for authorization)
./twitter-poster get-token

# Preview what would be posted from a feed
./twitter-poster post --feed-url "https://example.com/feed.xml" --dry-run

# Post new items to Twitter using default format: {title} {link}
./twitter-poster post --feed-url "https://example.com/feed.xml"

# Custom tweet format
./twitter-poster post --feed-url "https://example.com/feed.xml" --template "[{title}] {link}"
```

## Setup

### 1. Twitter Developer Portal

1. Go to [Twitter Developer Portal](https://developer.twitter.com/en/portal/dashboard) → your App → Settings
2. Under **User authentication settings**, enable **OAuth 2.0** (or OAuth 1.0a)
3. Set App permissions to **Read and Write**
4. Set App type to **Web App**
5. Copy your **Client ID** and **Client Secret**

### 2. Get Access Token

**Option A: OAuth 2.0 PKCE (recommended)**

```bash
./twitter-poster get-token
```

Starts a local server on `:8080`, opens your browser to authorize with Twitter, and prints the token.

> Ensure your Twitter app has `http://localhost:8080/callback` as a valid Callback URI.

**Option B: OAuth 1.0a (from Developer Portal)**

In the Developer Portal, under **Keys and tokens**, generate Access Token & Secret. Then set in `.env`:

```
TWITTER_CONSUMER_KEY=...
TWITTER_CONSUMER_SECRET=...
TWITTER_ACCESS_TOKEN=...
TWITTER_ACCESS_TOKEN_SECRET=...
```

**Option C: Persistent auth server (for Cloudflare tunnel)**

```bash
./twitter-poster serve --port 8000
# In another terminal:
cloudflared tunnel --url http://localhost:8000
```

### 3. Configure `.env`

```bash
cp .env.example .env
```

Fill in the relevant credentials:

```
# OAuth 2.0 (from get-token or serve)
TWITTER_CLIENT_ID=...
TWITTER_CLIENT_SECRET=...
TWITTER_BEARER_TOKEN=...        # paste access_token from get-token

# OAuth 1.0a (from Developer Portal)
TWITTER_CONSUMER_KEY=...
TWITTER_CONSUMER_SECRET=...
TWITTER_ACCESS_TOKEN=...
TWITTER_ACCESS_TOKEN_SECRET=...

# Optional: redirect URI for serve command
TWITTER_REDIRECT_URI=https://your-tunnel.trycloudflare.com/callback
```

### 4. Post

```bash
# Build
go build -o twitter-poster .

# Dry run (no credentials needed)
./twitter-poster post --feed-url "https://your-feed-url" --dry-run

# Post to Twitter
./twitter-poster post --feed-url "https://your-feed-url"
```

## How It Works

- The program fetches an RSS or Atom feed from the given URL (format auto-detected)
- Compares each item's publish date against the last posted timestamp in `last_timestamp.txt`
- Only **newer** items are posted — no duplicates
- The timestamp is saved **immediately** after each successful post, so interrupted runs won't re-post
- If `TWITTER_BEARER_TOKEN` is set, OAuth 2.0 is used; otherwise OAuth 1.0a
- Default tweet format is `{title} {link}`; customizable via `--template`

## Commands

| Command | Description |
|---------|-------------|
| `post` (default) | Fetch RSS/Atom feed and post new items to Twitter |
| `get-token` | OAuth 2.0 PKCE flow — opens browser, obtains access token |
| `serve` | Persistent OAuth 2.0 auth server (for Cloudflare tunnel setups) |

### Post flags

| Flag | Default | Description |
|------|---------|-------------|
| `--feed-url` | `https://www.blognone.com/node/feed` | URL of RSS/Atom feed to fetch |
| `--template` | `{title} {link}` | Tweet format. Placeholders: `{title}`, `{link}` |
| `--timestamp-file` | `last_timestamp.txt` | Path to last timestamp file |
| `--dry-run` | `false` | Print what would be posted without posting |

### get-token flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8080` | Local port for OAuth callback |
| `--no-browser` | `false` | Don't open browser automatically |

### serve flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8000` | Server port |

## Troubleshooting

- **403 Forbidden:** App permissions must be "Read and Write"
- **401 Unauthorized:** Check credentials in `.env`
- **Duplicate Tweet:** Twitter rejects identical consecutive tweets; the timestamp tracking prevents this from the source side
- **Callback error:** Ensure your Twitter app's Callback URI matches `http://localhost:{port}/callback` (for get-token) or your tunnel URL (for serve)
- **Feed parse error:** Verify the feed URL is publicly accessible and returns valid RSS or Atom XML

## Legacy

The original Python/JavaScript implementations are in [`legacy/`](legacy/).
