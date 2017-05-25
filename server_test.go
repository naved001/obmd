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
