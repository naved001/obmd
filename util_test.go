package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// utility functions for testing.

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
	text, err := theConfig.AdminToken.MarshalText()
	if err != nil {
		panic(err)
	}
	req.SetBasicAuth("admin", string(text))
	return req
}

// Wraps makeHandler, passing testing-appropriate arguments
func newHandler() http.Handler {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		panic(err)
	}
	handler, err := makeHandler(theConfig, &MockIpmiDialer{}, db)
	if err != nil {
		panic(err)
	}
	return handler
}

// Make the specified request, and call t.Fatal if the status code is
// not expectedStatus.
func adminRequireStatus(t *testing.T, handler http.Handler, expectedStatus int, spec requestSpec) {
	actualStatus := adminReq(handler, spec).Result().StatusCode
	if expectedStatus != actualStatus {
		t.Fatalf("Request %v: expected status %d but got %d\n",
			spec, expectedStatus, actualStatus)
	}
}

// Make the specified request, and return a ResponseRecorder with the response.
func adminReq(handler http.Handler, spec requestSpec) *httptest.ResponseRecorder {
	req := spec.toAdminAuth()
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

// Mock ipmi dialer for use in tests
type MockIpmiDialer struct{}

// Connect to a mock console stream. It just writes "addr":"user":"pass" in a
// loop until the connection is closed.
func (d *MockIpmiDialer) DialIpmi(info *IpmiInfo) (io.ReadCloser, error) {
	myConn, theirConn := net.Pipe()

	go func() {
		var err error
		for err == nil {
			_, err = fmt.Fprintf(myConn, "%q:%q:%q\n", info.Addr, info.User, info.Pass)
		}
	}()

	return theirConn, nil
}

func (d *MockIpmiDialer) PowerOff(info *IpmiInfo) error               { panic("Not Implemented") }
func (d *MockIpmiDialer) PowerCycle(info *IpmiInfo, force bool) error { panic("Not Implemented") }
func (d *MockIpmiDialer) SetBootdev(info *IpmiInfo, dev string) error { panic("Not Implemented") }
