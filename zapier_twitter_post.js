// This code is designed for a "Code by Zapier" (JavaScript) step.
// It uses Twitter API v2 with OAuth 1.0a to post a tweet.
// Prerequisite: Ensure your Twitter App has "Read and Write" permissions.
// You must generate the Access Token and Secret AFTER enabling Write permissions.

const crypto = require('crypto');

// 1. Setup credentials from Input Data
// Expected keys in Zapier Input Data:
// consumer_key, consumer_secret, access_token, access_token_secret, tweet_text
const { 
  consumer_key, 
  consumer_secret, 
  access_token, 
  access_token_secret, 
  tweet_text 
} = inputData;

const url = 'https://api.twitter.com/2/tweets';
const method = 'POST';

// 2. Helper function for RFC 3986 encoding
function percentEncode(str) {
  return encodeURIComponent(str)
    .replace(/[!'()*]/g, (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`);
}

// 3. Prepare OAuth parameters
const oauthParams = {
  oauth_consumer_key: consumer_key,
  oauth_nonce: crypto.randomBytes(16).toString('hex'),
  oauth_signature_method: 'HMAC-SHA1',
  oauth_timestamp: Math.floor(Date.now() / 1000).toString(),
  oauth_token: access_token,
  oauth_version: '1.0',
};

// 4. Generate the Signature Base String
// For v2 JSON posts, the body is NOT included in the signature
const parameterString = Object.keys(oauthParams)
  .sort()
  .map(key => `${percentEncode(key)}=${percentEncode(oauthParams[key])}`)
  .join('&');

const signatureBaseString = [
  method.toUpperCase(),
  percentEncode(url),
  percentEncode(parameterString)
].join('&');

const signingKey = [
  percentEncode(consumer_secret),
  percentEncode(access_token_secret)
].join('&');

const signature = crypto
  .createHmac('sha1', signingKey)
  .update(signatureBaseString)
  .digest('base64');

oauthParams.oauth_signature = signature;

// 5. Build the Authorization Header
const authHeader = 'OAuth ' + Object.keys(oauthParams)
  .sort()
  .map(key => `${percentEncode(key)}="${percentEncode(oauthParams[key])}"`)
  .join(', ');

// 6. Execute the request
try {
  const response = await fetch(url, {
    method: method,
    headers: {
      'Authorization': authHeader,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ text: tweet_text }),
  });

  const data = await response.json();
  
  if (!response.ok) {
    return { 
      status: "error", 
      error_details: data,
      message: "Twitter API error. Ensure your keys are correct and App permissions are 'Read and Write'."
    };
  }

  return { 
    status: "success", 
    tweet_id: data.data.id,
    text: data.data.text 
  };
} catch (error) {
  return { 
    status: "error", 
    message: error.message 
  };
}
