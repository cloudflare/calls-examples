interface Env {
	OPENAI_API_KEY: string
	OPENAI_MODEL_ENDPOINT: string
	CALLS_BASE_URL: string
	CALLS_APP_ID: string
	CALLS_APP_TOKEN: string
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
	errorCode?: string
	errorDescription?: string
}

interface NewTracksResponse {
	tracks: NewTrackResponse[]
	sessionDescription?: SessionDescription
	errorCode?: string
	errorDescription?: string
}

interface TrackLocator {
	location: string
	sessionId: string
	trackName: string
}

class CallsSession {
	sessionId: string
	headers: any
	endpoint: string
	constructor(sessionId: string, headers: any, endpoint: string) {
		this.sessionId = sessionId
		this.headers = headers
		this.endpoint = endpoint
	}
	async NewTracks(body: any): Promise<NewTracksResponse> {
		const newTracksURL = new URL(`${this.endpoint}/sessions/${this.sessionId}/tracks/new?streamDebug`)
		const newTracksResponse = await fetch(
			newTracksURL.href,
			{
				method: "POST",
				headers: this.headers,
				body: JSON.stringify(body)
			},
		).then((res) => res.json()) as NewTracksResponse;
		return newTracksResponse
	}
	async Renegotiate(sdp: SessionDescription) {
		const renegotiateBody = {
			"sessionDescription": sdp
		}
		const renegotiateURL = new URL(`${this.endpoint}/sessions/${this.sessionId}/renegotiate?streamDebug`)
		const renegotiateResponse = await fetch(renegotiateURL.href, {
			method: 'PUT',
			headers: this.headers,
			body: JSON.stringify(renegotiateBody),
		})
	}
}

async function CallsNewSession(baseURL: string, appID: string, appToken: string, thirdparty: boolean = false): Promise<CallsSession> {
	const headers = {
		Authorization: `Bearer ${appToken}`,
		"Content-Type": "application/json",
	}
	const endpoint = `${baseURL}/${appID}`
	const newSessionURL = new URL(`${endpoint}/sessions/new?streamDebug`)
	if (thirdparty) {
		newSessionURL.searchParams.set('thirdparty', 'true')
	}
	const sessionResponse = await fetch(newSessionURL.href,
		{
			method: "POST",
			headers: headers,
		},
	).then((res) => res.json()) as NewSessionResponse;
	return new CallsSession(sessionResponse.sessionId, headers, endpoint)
}

function checkNewTracksResponse(newTracksResponse: NewTracksResponse, sdpExpected: boolean = false) {
	if (newTracksResponse.errorCode) {
		throw newTracksResponse.errorDescription
	}
	if (newTracksResponse.tracks[0].errorDescription) {
		throw newTracksResponse.tracks[0].errorDescription
	}
	if (sdpExpected && newTracksResponse.sessionDescription == null) {
		throw "empty sdp from Calls for session A"
	}
}

async function requestOpenAIService(originalRequest: Request, offer: SessionDescription, env: Env): Promise<SessionDescription> {
	const apiKey = env.OPENAI_API_KEY
	const originalRequestURL = new URL(originalRequest.url)
	const endpointURL = new URL(env.OPENAI_MODEL_ENDPOINT)
	const originalParams = new URLSearchParams(endpointURL.search)
	const newParams = new URLSearchParams(originalRequestURL.search)

	// Merge the params, giving priority to the original request URL params
	for (const [key, value] of originalParams) {
	if (!newParams.has(key)) {
		newParams.set(key, value)
	}
	}

	endpointURL.search = newParams.toString()
	const response = await fetch(endpointURL.href, {
		method: 'POST',
		body: offer.sdp,
		headers: {
			Authorization: `Bearer ${apiKey}`,
			'Content-Type': 'application/sdp'
		}
	})
	const answerSDP = await response.text()
	return { type: "answer", sdp: answerSDP } as SessionDescription
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

const corsHeaders = { "access-control-allow-origin": "*" }

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		if (request.method == 'OPTIONS') {
			return optionsResponse()
		}
		if (!new URL(request.url).pathname.endsWith("/endpoint")) {
			return new Response("not found", {status: 404})
		}
		// PeerConnection A
		// This session establishes a PeerConnection between the end-user and Calls.
		const sessionA = await CallsNewSession(env.CALLS_BASE_URL, env.CALLS_APP_ID, env.CALLS_APP_TOKEN)
		const userSDP = await request.text()
		const newTracksResponseA = await sessionA.NewTracks({
			"sessionDescription": { "sdp": userSDP, "type": "offer" },
			"tracks": [{
				"location": "local",
				"trackName": "user-mic",
				// Let it know a sendrecv transceiver is wanted to receive this track instead of a recvonly one
				"bidirectionalMediaStream": true,
				// Needed to create an appropriate response
				"kind": "audio",
				"mid": "0",
			}]
		});
		checkNewTracksResponse(newTracksResponseA, true)

		// PeerConnection B
		// This session establishes a PeerConnection between Calls and OpenAI.
		// CallsNewSession thirdparty parameter must be true to be able to connect to an external WebRTC server
		const sessionB = await CallsNewSession(env.CALLS_BASE_URL, env.CALLS_APP_ID, env.CALLS_APP_TOKEN, true)
		const newTracksResponseB = await sessionB.NewTracks({
			// No offer is provided so Calls will generate one for us
			"tracks": [{
				"location": "local",
				"trackName": "ai-generated-voice",
				// Let it know a sendrecv transceiver is wanted to receive this track instead of a recvonly one
				"bidirectionalMediaStream": true,
				// Needed to create an appropriate response
				"kind": "audio",
			}]
		});
		checkNewTracksResponse(newTracksResponseB, true)
		// The Calls's offer is sent to OpenAI
		const openaiAnswer = await requestOpenAIService(request, newTracksResponseB.sessionDescription || {} as SessionDescription, env)
		// And the negotiation is completed by setting the answer from OpenAI
		await sessionB.Renegotiate(openaiAnswer)

		// PeerConnection A answer SDP must be sent before anything else
		// in order to establish a connection first. That's the reason
		// to make the exchange requests after returning a response
		ctx.waitUntil((async () => {
			console.log("Starting exchange")
			// The tracks exchange happens here
			const exchangeStepOne = await sessionA.NewTracks({
				// Session A is the PeerConnection from Calls to the end-user.
				// The following request instructs Calls to pull the 'ai-generated-voice' from session B and to send
				// it back to the end-user through an existing transceiver that was created to
				// publish the user-mic track at the beginning
				//
				//
				//                 PeerConnection A     
				// end-user <-> [sendrecv transceiver] <---- ai-generated-voice (new!)  
				//                mid=0 (#user-mic)   \
				//                                     `--> user-mic
				"tracks": [{
					"location": "remote",
					"sessionId": sessionB.sessionId,
					"trackName": "ai-generated-voice",
					// We may not know the exact mid value associated to the user-mic transceiver
					// so instead of providing it, let Calls to resolve it for you
					"mid": "#user-mic"
				}]
			})
			checkNewTracksResponse(exchangeStepOne)
			console.log("exchangeStepOne ready")
				// Session B is the PeerConnection from Calls to OpenAI.
				// The following request instructs Calls to pull the 'user-mic' from session A and to send
				// it back to OpenAI through an existing transceiver that was created to
				// publish the ai-generated-voice
				//
				//
				//               PeerConnection B     
				// OpenAI <-> [sendrecv transceiver]     <-------- user-mic (new!)
				//          mid=0 (#ai-generated-voice) \  
				//                                       \
				//                                        `--> ai-generated-voice
			const exchangeStepTwo = await sessionB.NewTracks({
				"tracks": [{
					"location": "remote",
					"sessionId": sessionA.sessionId,
					"trackName": "user-mic",
					// Let Calls to find out the actual mid value
					"mid": "#ai-generated-voice"
				}]
			})
			checkNewTracksResponse(exchangeStepTwo)
			console.log("exchangeStepTwo ready")
		})())
		// This will complete the negotiation to connect PeerConnection A
		return new Response(newTracksResponseA.sessionDescription?.sdp, {
			status: 200,
			headers: corsHeaders
		})
	},
} satisfies ExportedHandler<Env>;