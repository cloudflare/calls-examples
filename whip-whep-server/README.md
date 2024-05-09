# WHIP-WHEP Server

WHIP and WHEP server implemented on top of Calls API

## Usage
### Configuration
The following environment variables must be set in wrangler.toml before running it:

* CALLS_APP_ID
* CALLS_APP_SECRET

### Install dependencies

```
npm install --include=dev
```

### Run it locally or deploy it to Earth

To run it locally:

```
npx wrangler dev --local
```

If you want it to run on the Cloudflare network:

```
npx wrangler deploy
```