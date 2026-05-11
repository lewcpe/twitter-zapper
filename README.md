# Twitter Zapier Post

This repository contains tools to post tweets via the Twitter (X) API v2 using "Code by Zapier".

## 1. OAuth 2.0 Token Acquisition Tool (FastAPI)

Since posting tweets requires **User Context**, you can use the included FastAPI server to perform the OAuth 2.0 PKCE flow and get a valid `access_token`.

### A. Setup Twitter Developer Portal
1. Go to your App's **User authentication settings**.
2. Enable **OAuth 2.0**.
3. Set **App Type** to **Web App**.
4. Set **Callback URI** to `https://<your-tunnel-subdomain>.trycloudflare.com/callback`.
5. Copy your **Client ID** and **Client Secret**.

### B. Run the Auth Server
1. Install dependencies:
   ```bash
   pip install -r requirements.txt
   ```
2. Run the server with your credentials:
   ```bash
   export TWITTER_CLIENT_ID="your_client_id"
   export TWITTER_CLIENT_SECRET="your_client_secret"
   export TWITTER_REDIRECT_URI="https://<your-tunnel-subdomain>.trycloudflare.com/callback"
   python auth_server.py
   ```
3. Start a Cloudflare Tunnel (in another terminal):
   ```bash
   cloudflared tunnel --url http://localhost:8000
   ```
4. Visit the URL provided by Cloudflare. It will redirect you to Twitter to authorize.
5. After authorization, the server will display your `access_token`.

---

## 2. Setup in Zapier

### A. Set Up Your App Permissions
1. Go to the [Twitter Developer Portal](https://developer.x.com/en/portal/dashboard).
2. Select your Project and then your App.
3. Click on the **Settings** (gear icon) for your app.
4. Under **User authentication settings**, click **Edit**:
   - Enable **OAuth 1.0a** (for permanent tokens) OR **OAuth 2.0** (for the FastAPI tool).
   - Set **App permissions** to **Read and Write**.
   - Under **Type of App**, select **Web App, Android, or iOS**.
   - Set a **Callback URI / Redirect URL** (Must match your tunnel URL).
   - Set a **Website URL** (e.g., your personal site).
5. Click **Save**.

### B. Configuration for "Code by Zapier"
In your Zapier Code step, add the following keys to **Input Data**:

#### For OAuth 1.0a (Permanent Token):
| Key | Value |
| :--- | :--- |
| `consumer_key` | API Key |
| `consumer_secret` | API Key Secret |
| `access_token` | User Access Token |
| `access_token_secret` | User Access Token Secret |
| `tweet_text` | Tweet content |

#### For OAuth 2.0 (Bearer Token from FastAPI Tool):
| Key | Value |
| :--- | :--- |
| `bearer_token` | `access_token` from FastAPI |
| `tweet_text` | Tweet content |

---

## Troubleshooting

- **403 Forbidden:** Ensure your App permissions are set to "Read and Write".
- **401 Unauthorized:** Check that your tokens/keys are correct and the signature matches.
- **Duplicate Tweet:** Twitter prevents posting the exact same text twice in a row.
