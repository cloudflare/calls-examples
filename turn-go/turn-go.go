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

// Response represents the top-level JSON structure.
type Response struct {
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
	var response Response
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

func main() {
	// Check if the required command-line arguments are provided.
	if len(os.Args) != 3 {
		fmt.Println("Usage: go run main.go <cloudflare_api_token> <cloudflare_account_id>")
		os.Exit(1)
	}

	// Get the Cloudflare API token and account ID from the command line.
	apiToken := os.Args[1]
	accountID := os.Args[2]

	// Create the first RTCPeerConnection (peer1).
	peer1, err := webrtc.NewPeerConnection(createNewWebrtcConfiguration(apiToken, accountID))
	if err != nil {
		log.Fatalf("error creating peer1: %v", err)
	}
	defer peer1.Close()

	// Create the second RTCPeerConnection (peer2).
	peer2, err := webrtc.NewPeerConnection(createNewWebrtcConfiguration(apiToken, accountID))
	if err != nil {
		log.Fatalf("error creating peer2: %v", err)
	}
	defer peer2.Close()

	// Gather ICE candidates for peer1.
	gatherComplete1 := make(chan struct{})
	peer1.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			close(gatherComplete1)
			return
		}
		candJson := candidate.ToJSON()
		log.Printf("Peer2 adding ICE candidate: %v", candJson)
		err := peer2.AddICECandidate(candJson)
		if err != nil {
			log.Printf("error adding ICE candidate to peer2: %v", err)
		}
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
		log.Printf("Peer1 adding ICE candidate: %v", candJson)
		err := peer1.AddICECandidate(candJson)
		if err != nil {
			log.Printf("error adding ICE candidate to peer1: %v", err)
		}
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

	// Create a data channel on peer1.  This is how we'll send data.
	dataChannel1, err := peer1.CreateDataChannel("dataChannel1", nil)
	if err != nil {
		log.Fatalf("error creating data channel on peer1: %v", err)
	}

	// dataChannel2 will be set when peer2 receives the offer.
	var dataChannel2 *webrtc.DataChannel
	dataChannel2Open := make(chan struct{}) // Add this channel

	// Set the handler for peer2's data channel.
	peer2.OnDataChannel(func(d *webrtc.DataChannel) {
		dataChannel2 = d
		dataChannel2.OnOpen(func() {
			log.Println("Data channel on peer2 opened")
			close(dataChannel2Open) // Close the channel when dataChannel2 is open
		})
		dataChannel2.OnMessage(func(msg webrtc.DataChannelMessage) {
			log.Printf("peer2 received: %s\n", string(msg.Data))
		})
		dataChannel2.OnError(func(err error) {
			log.Printf("Data channel on peer2 error: %v", err)
		})
		dataChannel2.OnClose(func() {
			log.Println("Data channel on peer 2 closed")
		})
	})

	// For a proper demo with peer1 and peer2 running on different machines the
	// offer and answer from the next coupld of steps would need to get exchanged
	// via a signaling server.

	// Create an offer from peer1.
	offer, err := peer1.CreateOffer(nil)
	if err != nil {
		log.Fatalf("error creating offer: %v", err)
	}

	// Set peer1's local description.
	err = peer1.SetLocalDescription(offer)
	if err != nil {
		log.Fatalf("error setting local description for peer1: %v", err)
	}

	// Set peer2's remote description with the offer.
	err = peer2.SetRemoteDescription(offer)
	if err != nil {
		log.Fatalf("error setting remote description for peer2: %v", err)
	}

	// Create an answer from peer2.
	answer, err := peer2.CreateAnswer(nil)
	if err != nil {
		log.Fatalf("error creating answer: %v", err)
	}

	// Set peer2's local description.
	err = peer2.SetLocalDescription(answer)
	if err != nil {
		log.Fatalf("error setting local description for peer2: %v", err)
	}

	// Set peer1's remote description with the answer.
	err = peer1.SetRemoteDescription(answer)
	if err != nil {
		log.Fatalf("error setting remote description for peer1: %v", err)
	}

	// Wait for ICE gathering to complete on both peers.
	<-gatherComplete1
	<-gatherComplete2
	log.Printf("ICE gathering has finished")

	log.Printf("Waiting for PeerConnection to connect")
	<-connected1
	<-connected2

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

	// Block until the data channel is open on peer2
	log.Printf("Waiting for data channel to open on peer2")
	<-dataChannel2Open // Use the channel here
	log.Printf("Data channel opened on peer2!")

	// Send a message from peer1 to peer2 once the channel is open.
	dataChannel1.OnOpen(func() {
		log.Println("Data channel on peer1 opened")
		err = dataChannel1.SendText("Hello from peer1!")
		if err != nil {
			log.Fatalf("error sending message from peer1: %v", err)
		}
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

		if dataChannel1.ReadyState() == webrtc.DataChannelStateOpen {
			err = dataChannel1.SendText(msg)
			if err != nil {
				log.Printf("error sending message from peer1: %v", err)
			}
		} else {
			log.Println("Data channel is not open.  Cannot send message.")
		}
	}

	// Close the data channels and peer connections.  These will be closed
	// automatically by the defer statements, but it's good to be explicit.
	if dataChannel1 != nil {
		dataChannel1.Close()
	}
	if dataChannel2 != nil {
		dataChannel2.Close()
	}
	peer1.Close()
	peer2.Close()
}
