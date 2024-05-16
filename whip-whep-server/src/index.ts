import { DurableObject } from "cloudflare:workers";

interface Env {
	CALLS_API: string
	CALLS_APP_ID: string
	CALLS_APP_SECRET: string
	LIVE_STORE: DurableObjectNamespace<LiveStore>
}

interface SessionDescription {
	sdp: string
	type: string
}

interface NewSessionResponse {
	sessionId: string
}

interface NewTrackResponse {
	trackName: string
	mid: string
}

interface NewTracksResponse {
	tracks: NewTrackResponse[]
	sessionDescription: SessionDescription
}

interface TrackLocator {
	location: string
	sessionId: string
	trackName: string
}

export class LiveStore extends DurableObject {
	constructor(ctx: DurableObjectState, env: Env) {
		super(ctx, env);
	}

	async setTracks(tracks: TrackLocator[]): Promise<void> {
		await this.ctx.storage.put("tracks", tracks)
	}

	async getTracks(): Promise<TrackLocator[]> {
		return await this.ctx.storage.get("tracks") || []
	}

	async deleteTracks() : Promise<void> {
		await this.ctx.storage.delete("tracks")
	} 
}

function optionsResponse(): Response {
	return new Response(null, {
		status: 204,
		headers: {
			"accept-post": "application/sdp",
			"access-control-allow-credentials": "true",
			"access-control-allow-headers": "content-type,authorization,if-match",
			"access-control-allow-methods": "PATCH,POST,PUT,DELETE,OPTIONS",
			"access-control-allow-origin": "*",
			"access-control-expose-headers": "x-thunderclap,location,link,accept-post,accept-patch,etag",
			"link": "<stun:stun.cloudflare.com:3478>; rel=\"ice-server\""
		}
	})
}
async function whipHandler(request: Request, env: Env, ctx: ExecutionContext, parsedURL: URL): Promise<Response> 
{
	const groups = /\/ingest\/([\w-]+)\/?([\w-]+)?/g.exec(parsedURL.pathname)
	if(!groups || groups.length < 2) {
		return new Response("not found", {status: 404})
	}
	const liveId = groups[1]
	let stub = env.LIVE_STORE.get(env.LIVE_STORE.idFromName(liveId))
	switch(request.method) {
	case 'OPTIONS':
		return optionsResponse()
	case 'POST':
		break
	case 'DELETE':
		stub.deleteTracks()
		return new Response("OK")
	default:
		return new Response("Not supported", {status: 400})
	}
	const CallsEndpoint = `${env.CALLS_API}/v1/apps/${env.CALLS_APP_ID}`
	const CallsEndpointHeaders = {'Authorization': `Bearer ${env.CALLS_APP_SECRET}`}
	const newSessionResult = await (await fetch(`${CallsEndpoint}/sessions/new`, {method: 'POST', headers: CallsEndpointHeaders})).json() as NewSessionResponse
	const newTracksBody = {
			"sessionDescription": {
				"type": "offer",
				"sdp": await request.text()
			},
			"autoDiscover": true
	}
	const newTracksResult = await (await fetch(`${CallsEndpoint}/sessions/${newSessionResult.sessionId}/tracks/new`, {
		method: 'POST',
		headers: CallsEndpointHeaders,
		body: JSON.stringify(newTracksBody)
	})).json() as NewTracksResponse
	const tracks = newTracksResult.tracks.map(track => {
		return {location: "remote", "sessionId": newSessionResult.sessionId, "trackName": track.trackName} 
	}) as TrackLocator[]
	await stub.setTracks(tracks)
	return new Response(newTracksResult.sessionDescription.sdp, {
		status: 201,
		headers: {
			'content-type': "application/sdp",
			'protocol-version': "draft-ietf-wish-whip-06",
			etag: `"${newSessionResult.sessionId}"`,
			location: `/ingest/${liveId}/${newSessionResult.sessionId}`
		},
	})
}

async function whepHandler(request: Request, env: Env, ctx: ExecutionContext, parsedURL: URL): Promise<Response> 
{
	const groups = /\/play\/([\w-]+)\/?([\w-]+)?/g.exec(parsedURL.pathname)
	if(!groups || groups.length < 2) {
		return new Response("not found", {status: 404})
	}
	const liveId = groups[1]
	const CallsEndpoint = `${env.CALLS_API}/v1/apps/${env.CALLS_APP_ID}`
	const CallsEndpointHeaders = {'Authorization': `Bearer ${env.CALLS_APP_SECRET}`}
	switch(request.method) {
		case 'OPTIONS':
			return optionsResponse()
		case 'POST':
			break
		case 'DELETE':
			return new Response("OK")
		case 'PATCH':
			const sessionId = groups[2]
			const renegotiateBody = {
				"sessionDescription": {
					"type": "answer",
					"sdp": await request.text()
				}
			}
			const renegotiateResponse = await fetch(`${CallsEndpoint}/sessions/${sessionId}/renegotiate`, {
				method: 'PUT',
				headers: CallsEndpointHeaders,
				body: JSON.stringify(renegotiateBody),
			})
			return new Response(null, {status: renegotiateResponse.status, headers: {
				"access-control-allow-origin": "*"
			}})
		default:
			return new Response("Not supported", {status: 400})
	}
	let stub = env.LIVE_STORE.get(env.LIVE_STORE.idFromName(liveId))
	const tracks = await stub.getTracks() as TrackLocator[]
	if(tracks.length == 0) {
		return new Response("Live not started yet", {status: 404})
	}
	const newSessionResult = await (await fetch(`${CallsEndpoint}/sessions/new`, {method: 'POST', headers: CallsEndpointHeaders})).json() as NewSessionResponse
	const newTracksBody = {
			"tracks": tracks
	}
	const newTracksResult = await (await fetch(`${CallsEndpoint}/sessions/${newSessionResult.sessionId}/tracks/new`, {
		method: 'POST',
		headers: CallsEndpointHeaders,
		body: JSON.stringify(newTracksBody)
	})).json() as NewTracksResponse
	return new Response(newTracksResult.sessionDescription.sdp, {
		status: 201,
		headers: {
			"access-control-expose-headers": "location",
			"access-control-allow-origin": "*",
			'content-type': "application/sdp",
			'protocol-version': "draft-ietf-wish-whep-00",
			"etag": `"${newSessionResult.sessionId}"`,
			"location": `/play/${liveId}/${newSessionResult.sessionId}`,
		},
	})
}

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		const parsedURL = new URL(request.url)
		if(parsedURL.pathname.split('/')[1] == 'ingest') {
			return whipHandler(request, env, ctx, parsedURL)
		}
		return whepHandler(request, env, ctx, parsedURL)
	},
};
