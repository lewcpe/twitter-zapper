# Twitter Zapier Post (OAuth 1.0a)

This repository contains a JavaScript snippet for use in a **"Code by Zapier"** step to post tweets via the Twitter (X) API v2.

## 1. Get Your Twitter API Credentials

To post tweets, you need **OAuth 1.0a User Context** credentials. Follow these steps:

### A. Set Up Your App Permissions
1. Go to the [Twitter Developer Portal](https://developer.x.com/en/portal/dashboard).
2. Select your Project and then your App.
3. Click on the **Settings** (gear icon) for your app.
4. Under **User authentication settings**, click **Edit**:
   - Enable **OAuth 1.0a**.
   - Set **App permissions** to **Read and Write**.
   - Under **Type of App**, select **Web App, Android, or iOS**.
   - Set a **Callback URI / Redirect URL** (e.g., `http://localhost`).
   - Set a **Website URL** (e.g., your personal site).
5. Click **Save**.

### B. Generate Keys and Tokens
1. Go to the **Keys and tokens** tab of your app.
2. Under **Consumer Keys**, generate (or regenerate) your **API Key** and **API Key Secret**. (In Zapier, these are `consumer_key` and `consumer_secret`).
3. Under **Authentication Tokens**, find **Access Token and Secret**.
4. Click **Generate** (or Regenerate).
   - *Note: You must do this AFTER setting permissions to "Read and Write". If you did it before, regenerate them now.*
5. Copy the **Access Token** and **Access Token Secret**.

---

## 2. Setup in Zapier

1. Create a new Zap and add a **"Code by Zapier"** action.
2. Select **Run JavaScript**.
3. In the **Input Data** section, add the following 5 keys exactly as shown:

| Key | Value (Map from previous steps or paste) |
| :--- | :--- |
| `consumer_key` | Your Twitter API Key |
| `consumer_secret` | Your Twitter API Key Secret |
| `access_token` | Your Twitter Access Token |
| `access_token_secret` | Your Twitter Access Token Secret |
| `tweet_text` | The content of the tweet you want to post |

4. In the **Code** section, paste the contents of `zapier_twitter_post.js` from this repository.
5. Test the action.

---

## Troubleshooting

- **403 Forbidden:** Ensure your App permissions are set to "Read and Write" and that you regenerated your Access Token *after* changing that setting.
- **401 Unauthorized:** Check that all 4 keys (Consumer Key/Secret and Access Token/Secret) are copied correctly and have no trailing spaces.
- **Duplicate Tweet:** Twitter API v2 will return an error if you try to post the exact same text twice in a short period.
