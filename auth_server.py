import os
import base64
import hashlib
import secrets
import requests
from fastapi import FastAPI, Request, HTTPException
from fastapi.responses import RedirectResponse, JSONResponse
from typing import Optional

app = FastAPI(title="Twitter OAuth 2.0 Token Getter")

# Configuration - These should be set in your environment
CLIENT_ID = os.getenv("TWITTER_CLIENT_ID")
CLIENT_SECRET = os.getenv("TWITTER_CLIENT_SECRET")
REDIRECT_URI = os.getenv("TWITTER_REDIRECT_URI") # e.g. https://your-tunnel.cloudflare.com/callback

# In-memory store for PKCE verifiers (use a proper store for production)
# Key: state, Value: code_verifier
state_store = {}

def generate_pkce():
    code_verifier = secrets.token_urlsafe(64)
    code_challenge = base64.urlsafe_b64encode(
        hashlib.sha256(code_verifier.encode()).digest()
    ).decode().replace("=", "")
    return code_verifier, code_challenge

@app.get("/")
def login():
    if not CLIENT_ID or not REDIRECT_URI:
        return {"error": "TWITTER_CLIENT_ID and TWITTER_REDIRECT_URI must be set"}
    
    code_verifier, code_challenge = generate_pkce()
    state = secrets.token_urlsafe(16)
    state_store[state] = code_verifier
    
    scopes = "tweet.read tweet.write users.read offline.access"
    
    auth_url = (
        f"https://twitter.com/i/oauth2/authorize?response_type=code&"
        f"client_id={CLIENT_ID}&redirect_uri={REDIRECT_URI}&"
        f"scope={scopes.replace(' ', '%20')}&state={state}&"
        f"code_challenge={code_challenge}&code_challenge_method=S256"
    )
    
    return RedirectResponse(auth_url)

@app.get("/callback")
def callback(code: str, state: str):
    code_verifier = state_store.pop(state, None)
    if not code_verifier:
        raise HTTPException(status_code=400, detail="Invalid state")
    
    # Exchange code for token
    token_url = "https://api.twitter.com/2/oauth2/token"
    
    # Authentication for the token endpoint
    # For a confidential client (with secret):
    auth = (CLIENT_ID, CLIENT_SECRET)
    
    data = {
        "grant_type": "authorization_code",
        "code": code,
        "redirect_uri": REDIRECT_URI,
        "code_verifier": code_verifier,
    }
    
    response = requests.post(token_url, data=data, auth=auth)
    
    if response.status_code != 200:
        return JSONResponse(
            status_code=response.status_code,
            content={"error": "Failed to fetch token", "details": response.json()}
        )
    
    token_data = response.json()
    
    # In a real app, you'd save this securely. 
    # Here we just display it for the user to copy.
    return {
        "message": "Token acquired successfully!",
        "access_token": token_data.get("access_token"),
        "refresh_token": token_data.get("refresh_token"),
        "expires_in": token_data.get("expires_in"),
        "instruction": "Use the 'access_token' in your Zapier script."
    }

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
