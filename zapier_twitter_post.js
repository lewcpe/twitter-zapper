// This code is designed for a "Code by Zapier" (JavaScript) step.
// It uses the Twitter API v2 to post a tweet.
// Prerequisite: Your Bearer Token must have 'tweet.write' scope.
// Note: App-only Bearer Tokens from the Developer Portal cannot post tweets. 
// You must use a User Access Token obtained via OAuth 2.0.

const { bearer_token, tweet_text } = inputData;

if (!bearer_token || !tweet_text) {
  return { error: "Missing bearer_token or tweet_text in Input Data." };
}

const url = 'https://api.twitter.com/2/tweets';

try {
  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${bearer_token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      text: tweet_text
    }),
  });

  const result = await response.json();

  if (!response.ok) {
    // Handle specific Twitter API errors
    return { 
      status: "error", 
      error: result,
      message: "Failed to post tweet. Ensure your token has 'tweet.write' scope and is a User token."
    };
  }

  return { 
    status: "success", 
    data: result.data 
  };

} catch (error) {
  return { 
    status: "error", 
    message: error.message 
  };
}
