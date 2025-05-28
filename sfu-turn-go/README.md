# TURN to TURN example in Go

This command line example written in Go shows how to fetch TURN credentials from the Cloudflare API for two PeerConnections.
Then configures two PeerConnection in Pion with the TURN credentials, both set with the relay only policy.
Then it connects the two PeerConnections and estalishes a data channel between the two peers.

## Building

Running `go build` should result in a binary called `turn-go` getting build.

## Executing

Simply invoke the `turn-go` binary with two arguments: the API token and the TURN roken.
You get these two parameters when you create a new TURN application on your Cloudflare dashboard.