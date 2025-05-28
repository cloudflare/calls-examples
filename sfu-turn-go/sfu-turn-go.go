package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pion/webrtc/v3"
)

// IceServer represents the structure of an iceServer entry in the JSON.
type IceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`   // Use omitempty to handle missing fields
	Credential string   `json:"credential,omitempty"` // Use omitempty to handle missing fields
}

// TurnResponse represents the top-level JSON structure.
type TurnResponse struct {
	IceServers []IceServer `json:"iceServers"`
}

// Helper function to fetch TURN credentials from Cloudflare API.
func getCloudflareTurnCredentials(apiToken, accountID string) (string, string, error) {
	// API endpoint for TURN credentials.
	endpoint := fmt.Sprintf("https://rtc.live.cloudflare.com/v1/turn/keys/%s/credentials/generate-ice-servers", accountID)

	// Request body for the TURN credentials API.
	requestBody := map[string]interface{}{
		"ttl": 86400, // Time-to-live in seconds (24 hours)
	}
	requestBodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return "", "", fmt.Errorf("error marshalling request body: %w", err)
	}

	// Create an HTTP request.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(string(requestBodyJSON)))
	if err != nil {
		return "", "", fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set the authorization header with the API token.
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiToken))
	req.Header.Set("Content-Type", "application/json") // Add content type header

	// Create an HTTP client and send the request.
	client := &http.Client{
		Timeout: 10 * time.Second, // set timeout
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer resp.Body.Close() // ensure body is closed

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("error reading response body: %w", err)
	}

	// Check the response status code.
	if resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("API request failed with status %s and body: %s", resp.Status, string(body))
	}

	// Unmarshal the JSON data into the 'response' struct.
	var response TurnResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return "", "", fmt.Errorf("error unmarshalling JSON response: %w", err)
	}

	var username, credential string
	// Iterate over the iceServers array.
	for _, server := range response.IceServers {
		// Check if both Username and Credential are present.
		if server.Username != "" {
			username = server.Username
		}
		if server.Credential != "" {
			credential = server.Credential
		}
	}

	return username, credential, nil
}

func createNewWebrtcConfiguration(apiToken string, accountID string) webrtc.Configuration {
	// Fetch TURN credentials from Cloudflare API.
	username, credential, err := getCloudflareTurnCredentials(apiToken, accountID)
	if err != nil {
		log.Fatalf("error fetching TURN credentials: %v", err)
	}
	log.Printf("Received from Cloudflare API username: %v, credential: %v", username, credential)

	// Set up Cloudflare TURN server configuration with the relay-only policy.
	return webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{
					// TODO these should get extracted from the API call as well
					// instead of being hard coded.
					"turn:turn.cloudflare.com:3478?transport=udp",
					"turn:turn.cloudflare.com:3478?transport=tcp",
					"turns:turn.cloudflare.com:5349?transport=tcp",
				},
				// Use the credentials from Cloudflare API.
				Username:   username,
				Credential: credential,
			},
		},
		ICETransportPolicy: webrtc.ICETransportPolicyRelay, // Enforce relay-only.
	}
}

type SessionDescription struct {
	Type string `json:"type"`
	Sdp  string `json:"sdp"`
}

// SessionResponse represents the top-level JSON structure.
type SessionResponse struct {
	SessionId   string             `json:"sessionId"`
	Description SessionDescription `json:"sessionDescription"`
}

func getCloudflareSfuSession(apiToken, appId, sdp string) (string, string, error) {
	// API endpoint for SFU session.
	endpoint := fmt.Sprintf("https://rtc.live.cloudflare.com/v1/apps/%s/sessions/new", appId)

	// Request body for the data channels API.
	requestBody := map[string]interface{}{
		"sessionDescription": SessionDescription{
			Type: "offer",
			Sdp:  sdp,
		},
	}
	requestBodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return "", "", fmt.Errorf("error marshalling request body: %w", err)
	}

	// Create an HTTP request.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(string(requestBodyJSON)))
	if err != nil {
		return "", "", fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set the authorization header with the API token.
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiToken))
	req.Header.Set("Content-Type", "application/json") // Add content type header

	// Create an HTTP client and send the request.
	client := &http.Client{
		Timeout: 10 * time.Second, // set timeout
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer resp.Body.Close() // ensure body is closed

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("error reading response body: %w", err)
	}

	// Check the response status code.
	if resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("API request failed with status %s and body: %s", resp.Status, string(body))
	}

	// Unmarshal the JSON data into the 'response' struct.
	var response SessionResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return "", "", fmt.Errorf("error unmarshalling JSON response: %w", err)
	}

	return response.SessionId, response.Description.Sdp, nil
}

type DataChannelRequest struct {
	Location        string  `json:"location"`
	DataChannelName string  `json:"dataChannelName"`
	SessionId       *string `json:"sessionId,omitempty"`
}

type DataChannelRequests struct {
	DataChannels []DataChannelRequest `json:"dataChannels"`
}

type DataChannelResponse struct {
	Location        string `json:"location"`
	DataChannelName string `json:"dataChannelName"`
	Id              uint16 `json:"id"`
}

type DataChannelResponses struct {
	DataChannels []DataChannelResponse `json:"dataChannels"`
}

func publishDataChannel(apiToken, appId, sessionId, channelName string) (uint16, error) {
	endpoint := fmt.Sprintf("https://rtc.live.cloudflare.com/v1/apps/%s/sessions/%s/datachannels/new", appId, sessionId)

	// Request body for the data channels API.
	dataChannel := DataChannelRequest{
		Location:        "local",
		DataChannelName: channelName,
	}
	requestBody := DataChannelRequests{
		DataChannels: []DataChannelRequest{dataChannel},
	}

	requestBodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return 0, fmt.Errorf("error marshalling request body: %w", err)
	}

	// Create an HTTP request.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(string(requestBodyJSON)))
	if err != nil {
		return 0, fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set the authorization header with the API token.
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiToken))
	req.Header.Set("Content-Type", "application/json") // Add content type header

	// Create an HTTP client and send the request.
	client := &http.Client{
		Timeout: 10 * time.Second, // set timeout
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer resp.Body.Close() // ensure body is closed

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("error reading response body: %w", err)
	}

	// Check the response status code.
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("API request failed with status %s and body: %s", resp.Status, string(body))
	}

	// Unmarshal the JSON data into the 'response' struct.
	var response DataChannelResponses
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, fmt.Errorf("error unmarshalling JSON response: %w", err)
	}

	return response.DataChannels[0].Id, nil

}

func subscribeDataChannel(apiToken, appId, sessionId, remoteSessionId, channelName string) (uint16, error) {
	endpoint := fmt.Sprintf("https://rtc.live.cloudflare.com/v1/apps/%s/sessions/%s/datachannels/new", appId, sessionId)

	// Request body for the data channels API.
	dataChannel := DataChannelRequest{
		Location:        "remote",
		DataChannelName: channelName,
		SessionId:       &remoteSessionId,
	}
	requestBody := DataChannelRequests{
		DataChannels: []DataChannelRequest{dataChannel},
	}

	requestBodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return 0, fmt.Errorf("error marshalling request body: %w", err)
	}

	// Create an HTTP request.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(string(requestBodyJSON)))
	if err != nil {
		return 0, fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set the authorization header with the API token.
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiToken))
	req.Header.Set("Content-Type", "application/json") // Add content type header

	// Create an HTTP client and send the request.
	client := &http.Client{
		Timeout: 10 * time.Second, // set timeout
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer resp.Body.Close() // ensure body is closed

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("error reading response body: %w", err)
	}

	// Check the response status code.
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("API request failed with status %s and body: %s", resp.Status, string(body))
	}

	// Unmarshal the JSON data into the 'response' struct.
	var response DataChannelResponses
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, fmt.Errorf("error unmarshalling JSON response: %w", err)
	}

	return response.DataChannels[0].Id, nil
}

func main() {
	// Check if the required command-line arguments are provided.
	if len(os.Args) != 5 {
		fmt.Println("Usage: go run main.go <cloudflare_turn_api_token> <cloudflare_turn_account_id> <cloudflare_sfu_api_token> <cloudflare_sfu_appid>")
		os.Exit(1)
	}

	// Get the Cloudflare API token and account ID from the command line.
	turnApiToken := os.Args[1]
	turnAccountID := os.Args[2]
	sfuApiToken := os.Args[3]
	sfuAppID := os.Args[4]

	// ==========================================================================================
	// Create two PeerConnections which are only allowed to connect through the TURN relays each.
	// ==========================================================================================

	// Create the first RTCPeerConnection (peer1).
	peer1, err := webrtc.NewPeerConnection(createNewWebrtcConfiguration(turnApiToken, turnAccountID))
	if err != nil {
		log.Fatalf("error creating peer1: %v", err)
	}
	defer peer1.Close()

	// Create the second RTCPeerConnection (peer2).
	peer2, err := webrtc.NewPeerConnection(createNewWebrtcConfiguration(turnApiToken, turnAccountID))
	if err != nil {
		log.Fatalf("error creating peer2: %v", err)
	}
	defer peer2.Close()

	// ===============================================
	// Set up ICE call backs for both PeerConnections.
	// ===============================================

	// Gather ICE candidates for peer1.
	gatherComplete1 := make(chan struct{})
	peer1.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			close(gatherComplete1)
			return
		}
		candJson := candidate.ToJSON()
		log.Printf("Peer1 gathered ICE candidate: %v", candJson)
	})

	peer1.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		log.Printf("Peer1 ICE connection state: %v", is)
	})

	connected1 := make(chan struct{})
	peer1.OnConnectionStateChange(func(pcs webrtc.PeerConnectionState) {
		log.Printf("Peer1 connection state: %v", pcs)
		if pcs == webrtc.PeerConnectionStateConnected {
			close(connected1)
		}
	})

	// Gather ICE candidates for peer2.
	gatherComplete2 := make(chan struct{})
	peer2.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			close(gatherComplete2)
			return
		}
		candJson := candidate.ToJSON()
		log.Printf("Peer2 gathered ICE candidate: %v", candJson)
	})

	peer2.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		log.Printf("Peer2 ICE connection state: %v", is)
	})

	connected2 := make(chan struct{})
	peer2.OnConnectionStateChange(func(pcs webrtc.PeerConnectionState) {
		log.Printf("Peer2 connection state: %v", pcs)
		if pcs == webrtc.PeerConnectionStateConnected {
			close(connected2)
		}
	})

	// ==============================================
	// Set up data channels for both PeerConnections.
	// ==============================================

	// Create the system event data channels on peer1 and peer2.
	// These are not (yet) used for anything.
	systemDataChannel1, err := peer1.CreateDataChannel("server-events", nil)
	if err != nil {
		log.Fatalf("error creating data channel on peer1: %v", err)
	}
	systemDataChannel2, err := peer2.CreateDataChannel("server-events", nil)
	if err != nil {
		log.Fatalf("error creating data channel on peer2: %v", err)
	}

	// Send test messages to the SFU.
	systemDataChannel1.OnOpen(func() {
		log.Println("System data channel on peer1 opened")
		err = systemDataChannel1.SendText("Hello from peer1!")
		if err != nil {
			log.Fatalf("error sending message from peer1: %v", err)
		}
	})
	systemDataChannel2.OnOpen(func() {
		log.Println("System data channel on peer2 opened")
		err = systemDataChannel2.SendText("Hello from peer2!")
		if err != nil {
			log.Fatalf("error sending message from peer2: %v", err)
		}
	})

	// =============================================
	// Next we establish PeerConnection1 to the SFU.
	// And start publishing a data channel.
	// =============================================

	// Create an offer from peer1.
	offer1, err := peer1.CreateOffer(nil)
	if err != nil {
		log.Fatalf("error creating offer peer1: %v", err)
	}

	// Set peer1's local description.
	err = peer1.SetLocalDescription(offer1)
	if err != nil {
		log.Fatalf("error setting local description for peer1: %v", err)
	}

	// We wait here for gathering to finish, so that all the ICE
	// candidates are included in the SDP offer.
	fmt.Printf("waiting for gathering to finish for peer1\n")
	<-gatherComplete1

	sessionId1, sdpAnswer1, err := getCloudflareSfuSession(sfuApiToken, sfuAppID, peer1.LocalDescription().SDP)
	if err != nil {
		log.Fatalf("error requesting a session ID for peer1: %v", err)
	}
	fmt.Printf("sessionID for peer1: %v\n", sessionId1)

	// Set peer1's remote description with the answer.
	err = peer1.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdpAnswer1})
	if err != nil {
		log.Fatalf("error setting remote description for peer1: %v", err)
	}

	log.Printf("Waiting for PeerConnection1 to connect to the SFU")
	<-connected1

	// For illustration purposes lets try to find the IP address and port
	// number peer1 got connected to. Unfortunately Pion doesn't fill the
	// SelectedCandidatePairID field of the TransportStats, so this is a little
	// hacky workaround.
	stats := peer1.GetStats()
	for _, s := range stats {
		switch stat := s.(type) {
		case webrtc.ICECandidatePairStats:
			if stat.State == webrtc.StatsICECandidatePairStateSucceeded && stat.Nominated {
				remoteID := stat.RemoteCandidateID
				for _, allStats := range stats {
					switch allType := allStats.(type) {
					case webrtc.ICECandidateStats:
						if allType.Type == webrtc.StatsTypeRemoteCandidate &&
							allType.ID == remoteID {
							log.Printf("Peer1 is connected to IP(%s) Port(%d)", allType.IP, allType.Port)
						}
					default:
					}
				}
			}
		default:
		}
	}

	publisherId, err := publishDataChannel(sfuApiToken, sfuAppID, sessionId1, "channel-one")
	if err != nil {
		log.Fatalf("error publishing data channel request for peer1: %v", err)
	}
	fmt.Printf("publisher channel id: %v\n", publisherId)

	negotiated := true

	// Add the data channel to the PeerConnection.
	publisherDataChannel, err := peer1.CreateDataChannel("channel-one",
		&webrtc.DataChannelInit{
			Negotiated: &negotiated,
			ID:         &publisherId,
		})
	if err != nil {
		log.Fatalf("error creating data channel on peer1: %v", err)
	}

	// =====================================================
	// Now it's time to establish the second PeerConnection.
	// And subscribe to the data channel from peer 1.
	// =====================================================

	// Create an offer from peer2.
	offer2, err := peer2.CreateOffer(nil)
	if err != nil {
		log.Fatalf("error creating offer for peer2: %v", err)
	}

	// Set peer2's local description.
	err = peer2.SetLocalDescription(offer2)
	if err != nil {
		log.Fatalf("error setting local description for peer2: %v", err)
	}

	// We wait here for gathering to finish, so that all the ICE
	// candidates are included in the SDP offer.
	fmt.Printf("waiting for gathering to finish for peer2\n")
	<-gatherComplete2

	sessionId2, sdpAnswer1, err := getCloudflareSfuSession(sfuApiToken, sfuAppID, peer2.LocalDescription().SDP)
	if err != nil {
		log.Fatalf("error requesting a session ID for peer2: %v", err)
	}
	fmt.Printf("sessionID for peer2: %v\n", sessionId2)

	// Set peer2's remote description with the answer.
	err = peer2.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdpAnswer1})
	if err != nil {
		log.Fatalf("error setting remote description for peer2: %v", err)
	}

	log.Printf("Waiting for PeerConnection2 to connect to the SFU")
	<-connected2

	subscriberId, err := subscribeDataChannel(sfuApiToken, sfuAppID, sessionId2, sessionId1, "channel-one")
	if err != nil {
		log.Fatalf("error subscribing to data channel from peer1 on peer2: %v", err)
	}
	log.Printf("subscribed channel id: %v\n", subscriberId)

	subscriberDataChannel, err := peer2.CreateDataChannel("channel-one-subscribed",
		&webrtc.DataChannelInit{
			Negotiated: &negotiated,
			ID:         &subscriberId,
		})

	subscriberDataChannel.OnOpen(func() {
		log.Printf("subscribed data channel opened on peer2\n")
	})
	subscriberDataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		log.Printf("peer 2 received: %v\n", string(msg.Data))
	})

	// Read from the console and send messages from peer1 to peer2.
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter message to send from peer1 (\"exit\" to quit): ")
		msg, _ := reader.ReadString('\n')
		msg = strings.TrimSpace(msg)

		if msg == "exit" {
			log.Println("Exiting...")
			break
		}

		if publisherDataChannel.ReadyState() == webrtc.DataChannelStateOpen {
			err = publisherDataChannel.SendText(msg)
			if err != nil {
				log.Printf("error sending message from peer1: %v", err)
			}
		} else {
			log.Println("Data channel is not open.  Cannot send message.")
		}
	}

	// Close the data channels and peer connections.  These will be closed
	// automatically by the defer statements, but it's good to be explicit.
	if systemDataChannel1 != nil {
		systemDataChannel1.Close()
	}
	if systemDataChannel2 != nil {
		systemDataChannel2.Close()
	}
	if publisherDataChannel != nil {
		systemDataChannel1.Close()
	}
	if subscriberDataChannel != nil {
		subscriberDataChannel.Close()
	}

	peer1.Close()
	peer2.Close()
}
