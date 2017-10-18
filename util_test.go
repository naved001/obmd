package main

// utility functions for testing.

import (
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zenhack/obmd/internal/driver"
	"github.com/zenhack/obmd/internal/driver/dummy"
	"github.com/zenhack/obmd/internal/driver/mock"
)

var theConfig *Config

func errpanic(err error) {
	if err != nil {
		panic(err)
	}
}

func init() {
	theConfig = &Config{
		ListenAddr: ":8080", // Not actually used directly by the handler.
	}
	errpanic((&theConfig.AdminToken).
		UnmarshalText([]byte("44d5ebcb1aae23bfefc8dca8314797eb")))
}

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
	errpanic(err)
	req.SetBasicAuth("admin", string(text))
	return req
}

// Wraps makeHandler, passing testing-appropriate arguments
func newHandler() http.Handler {
	db, err := sql.Open("sqlite3", ":memory:")
	errpanic(err)
	state, err := NewState(db, driver.Registry{
		"ipmi":  mock.Driver,
		"dummy": dummy.Driver,
	})
	errpanic(err)
	return makeHandler(theConfig, NewDaemon(state))
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