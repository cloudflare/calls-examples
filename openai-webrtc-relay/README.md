# Calls - OpenAI demo

This is a simple example of how you can set up OpenAI's WebRTC realtime API with Cloudflare Calls.

## Configuration

Please update the environment variables in wrangler.toml
```
OPENAI_API_KEY = "<openai api key>"
OPENAI_MODEL_ENDPOINT = "https://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-10-01"
CALLS_BASE_URL = "https://rtc.live.cloudflare.com/v1/apps"
CALLS_APP_ID = "<calls app id>"
CALLS_APP_TOKEN = "<calls app token>"
```

## How to run it
Install dependencies if you run this for first time:
```
npm install --include=dev
```
Once everything is in place, run the dev server:
```
npm start -- --port 7878
```