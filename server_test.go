package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
var adminRequests = []struct {
	method, url, body string
}{
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
		req := httptest.NewRequest(v.method, v.url, bytes.NewBuffer([]byte(v.body)))
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

	expected := []int{404, 200, 200, 200, 404}

	for i, v := range adminRequests {
		req := httptest.NewRequest(v.method, v.url, bytes.NewBuffer([]byte(v.body)))
		req.SetBasicAuth("admin", theConfig.AdminToken)
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
	requestSequence := []struct {
		method, url, body string
	}{
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
	for i, v := range requestSequence {
		req := httptest.NewRequest(v.method, v.url, bytes.NewBuffer([]byte(v.body)))
		req.SetBasicAuth("admin", theConfig.AdminToken)
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
