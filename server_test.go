package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// http.ResponseWriter that lets us stream a response during test.
type responseStreamer struct {
	code   int
	header http.Header
	body   io.WriteCloser
}

func (w responseStreamer) WriteHeader(code int) {
	w.code = code
}

func (w responseStreamer) Header() http.Header {
	return w.header
}

func (w responseStreamer) Write(p []byte) (n int, err error) {
	if w.code == 0 {
		w.WriteHeader(w.code)
	}
	return w.body.Write(p)
}

// handy type for specifying requests in data literals
type requestSpec struct {
	method, url, body string
}

// Convert the request spec to an unauthenticated http.Request
func (r *requestSpec) toNoAuth() *http.Request {
	return httptest.NewRequest(r.method, r.url, bytes.NewBuffer([]byte(r.body)))
}

// Convert the request spec to a request authenticated as admin.
func (r *requestSpec) toAdminAuth() *http.Request {
	req := r.toNoAuth()
	req.SetBasicAuth("admin", theConfig.AdminToken)
	return req
}

// Mock IpmiDialer for use in tests:
type MockIpmiDialer struct {
}

// Connect to a mock console stream. It just writes "addr":"user":"pass" in a
// loop until the connection is closed.
func (d *MockIpmiDialer) DialIpmi(info *IpmiInfo) (net.Conn, error) {
	myConn, theirConn := net.Pipe()

	go func() {
		var err error
		for err == nil {
			_, err = fmt.Fprintf(myConn, "%q:%q:%q\n", info.Addr, info.User, info.Pass)
		}
	}()

	return theirConn, nil
}

// adminRequests is a sequence of admin-only requests that is used by various tests.
var adminRequests = []requestSpec{
	{"PUT", "http://localhost:8080/node/somenode/owner", `{
		"owner": "bob"
	}`},
	{"PUT", "http://localhost:8080/node/somenode", `{
		"host": "10.0.0.3",
		"user": "ipmiuser",
		"pass": "secret"
	}`},
	{"PUT", "http://localhost:8080/node/somenode/owner", `{
		"owner": "bob"
	}`},
	{"POST", "http://localhost:80080/node/somenode/console-endpoints", `{
		"owner": "bob"
	}`},
	{"DELETE", "http://localhost:8080/node/somenode", ""},
	{"PUT", "http://localhost:8080/node/somenode/owner", `{
		"owner": "bob"
	}`},
}

var theConfig = &Config{
	ListenAddr: ":8080", // Not actually used directly by the handler.
	AdminToken: "secret",
}

// Wraps makeHandler, passing testing-appropriate arguments
func newHandler() http.Handler {
	return makeHandler(theConfig, &MockIpmiDialer{})
}

// Verify: all admin-only requests should return 404 when made without
// authentication.
func TestAdminNoAuth(t *testing.T) {
	handler := newHandler()

	for i, v := range adminRequests {
		req := v.toNoAuth()
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Result().StatusCode != 404 {
			t.Fatalf("Un-authenticated adminRequests[%d] (%v) should have "+
				"returned 404, but did not.", i, v)
		}
	}
}

// Test status codes for authenticated requests in adminRequests
func TestAdminGoodAuth(t *testing.T) {
	handler := newHandler()

	expected := []int{404, 200, 200, 200, 200, 404}

	for i, v := range adminRequests {
		req := v.toAdminAuth()
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		actual := resp.Result().StatusCode
		if actual != expected[i] {
			t.Fatalf("Unexpected status code for authenticated adminRequests[%d]; "+
				"wanted %d but got %d", i, expected[i], actual)
		}
	}
}

// This tests for correct mitigation of a race condition where a daemon acting on behalf of an
// admin has two handlers being executed in parallel: one that revokes or changes access to a
// node, and one that creates a console access token for it. The race condition in question is:
//
// 1. Console-token granting admin handler verifies credentials of its user
// 2. Access-revoking admin handler revokes access
// 3. Console-token granting admin handler generates an access token, and returns it to the
//    user, granting access to a node that should have been revoked.
//
// We mitigate this by including the owner that the admin handler believes is correct as
// part of the token granting request; step (2) will have changed the owner, and so the
// console server will detect the descrepency, rejecting the request.
func TestOwnerRace(t *testing.T) {
	handler := newHandler()

	// preliminary requests; a node is created, granted to bob, and then
	// ownership is changed to alice.
	setupRequests := []requestSpec{
		{"PUT", "http://localhost/node/somenode", `{
			"addr": "10.0.0.3",
			"user": "ipmiuser",
			"pass": "secret"
		}`},
		{"PUT", "http://localhost/node/somenode/owner", `{
			"owner": "bob"
		}`},
		{"PUT", "http://localhost/node/somenode/owner", `{
			"owner": "alice"
		}`},
	}
	for i, v := range setupRequests {
		req := v.toAdminAuth()
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		status := resp.Result().StatusCode
		if status != http.StatusOK {
			t.Fatalf("During setup in TestOwnerRace: Request #%d: %v failed with status %d.",
				i, v, status)
		}
	}

	// Now, try to request a token with bob as the expected owner. This should fail with a 409
	// CONFLICT status.
	req := httptest.NewRequest("POST", "http://localhost/node/somenode/console-endpoints",
		bytes.NewBuffer([]byte(`{
			"owner": "bob"
		}`)))
	req.SetBasicAuth("admin", theConfig.AdminToken)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Result().StatusCode != http.StatusConflict {
		t.Fatal("Owner mismatch did not result in an HTTP 409 CONFLICT.")
	}
}

// Go through the motions of granting access to the console, viewing it, and then having access
// revoked.
func TestViewConsole(t *testing.T) {
	handler := newHandler()

	setupRequests := []requestSpec{
		{"PUT", "http://localhost/node/somenode", `{
			"addr": "10.0.0.3",
			"user": "ipmiuser",
			"pass": "secret"
		}`},
		{"PUT", "http://localhost/node/somenode/owner", `{
			"owner": "bob"
		}`},
	}
	for i, v := range setupRequests {
		req := v.toAdminAuth()
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		status := resp.Result().StatusCode
		if status != http.StatusOK {
			t.Fatalf("During setup in TestViewConsole: Request #%d: %v failed with status %d.",
				i, v, status)
		}
	}
	req := (&requestSpec{"POST", "http://localhost/node/somenode/console-endpoints", `{
		"owner": "bob"
	}`}).toAdminAuth()
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	result := resp.Result()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("TestConsoleView: getting token failed with status %d.", result.StatusCode)
	}
	var respBody TokenResp
	err := json.NewDecoder(result.Body).Decode(&respBody)
	if err != nil {
		t.Fatalf("Decoding body in TestViewConsole: %v", err)
	}
	textToken, err := respBody.Token.MarshalText()
	if err != nil {
		t.Fatalf("Formatting token in TestViewConsole: %v", err)
	}

	req = httptest.NewRequest(
		"GET",
		"http://localhost/node/somenode/console?token="+string(textToken),
		bytes.NewBuffer(nil),
	)

	r, w := io.Pipe()
	respStreamer := &responseStreamer{
		header: make(http.Header),
		body:   w,
	}

	go func() {
		handler.ServeHTTP(respStreamer, req)
		w.Close()
	}()

	bufReader := bufio.NewReader(r)
	for i := 0; i < 10; i++ {
		line, err := bufReader.ReadString('\n')
		if err == io.EOF {
			t.Logf("Request status: %d", respStreamer.code)
		}
		if err != nil {
			t.Fatalf("Error reading from console: %v", err)
		}
		expected := `"10.0.0.3":"ipmiuser":"secret"` + "\n"
		if line != expected {
			t.Fatalf("Unexpected data read from console: %q", line)
		}
	}

	req = (&requestSpec{"DELETE", "http://localhost/node/somenode/owner", ""}).toAdminAuth()
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	status := resp.Result().StatusCode
	if status != http.StatusOK {
		t.Fatalf("ownership revocation request failed with status: %d", status)
	}

	// Clear out any buffered data:
	bufReader.Discard(bufReader.Buffered())
	// Now try to keep reading. This should fail; we should have been disconnected by the
	// DELETE request.
	_, err = bufReader.ReadByte()
	if err == nil {
		t.Fatal("Connection should have been closed, but we were able to successfully " +
			"read data.")
	}
}
