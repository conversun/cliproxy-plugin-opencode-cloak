package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestInterceptAfter_returnsEmptyEnvelope_whenWireRequestIsMalformed(t *testing.T) {
	for _, raw := range [][]byte{[]byte("{"), []byte("not-json")} {
		t.Run(string(raw), func(t *testing.T) {
			// When
			responseRaw, err := interceptAfter(raw)

			// Then
			if err != nil {
				t.Fatalf("interceptAfter() error = %v", err)
			}
			response := unwrapInterceptResponse(t, responseRaw)
			if response.Body != nil {
				t.Fatalf("interceptAfter() body = %q, want nil", response.Body)
			}
		})
	}
}

func TestTransformInterceptAfter_skipsSecondInvocation_whenBodyWasAlreadyTransformed(t *testing.T) {
	// Given
	req := newTransformRequest(
		`{"system":"benign instruction","messages":[{"role":"user","content":"hello"}]}`,
		http.Header{"User-Agent": []string{"opencode/1.14.3"}},
	)
	first, ok := transformInterceptAfter(req, defaultConfig())
	if !ok {
		t.Fatal("first transformInterceptAfter() did not transform")
	}
	req.Body = first

	// When
	second, ok := transformInterceptAfter(req, defaultConfig())

	// Then
	if ok || second != nil {
		t.Fatalf("second transformInterceptAfter() = (%q, %t), want (nil, false)", second, ok)
	}
}

func unwrapInterceptResponse(t *testing.T, raw []byte) pluginapi.RequestInterceptResponse {
	t.Helper()
	var wire envelope
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !wire.OK {
		t.Fatalf("envelope OK = false, error = %#v", wire.Error)
	}
	var response pluginapi.RequestInterceptResponse
	if err := json.Unmarshal(wire.Result, &response); err != nil {
		t.Fatalf("unmarshal interceptor response: %v", err)
	}
	return response
}
