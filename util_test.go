package main

// utility functions for testing.

import (
	"bytes"
	"database/sql"
	"encoding/json"
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

// Get a token for the given node, using handler as the server. If anything goes wrong,
// the test is aborted.
func getToken(t *testing.T, handler http.Handler, nodeId string) string {
	resp := adminReq(handler, requestSpec{"POST", "http://localhost/node/" + nodeId + "/token", ""})
	result := resp.Result()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("getting token failed with status %d.", result.StatusCode)
	}
	var respBody TokenResp
	err := json.NewDecoder(result.Body).Decode(&respBody)
	if err != nil {
		t.Fatalf("Decoding body in getToken: %v", err)
	}
	textToken, err := respBody.Token.MarshalText()
	if err != nil {
		t.Fatalf("Formatting token in getToken: %v", err)
	}
	return string(textToken)
}

// Registered a node with nodeId and the given nodeInfo, using handler. fails the test if anything
// goes wrong.
func makeNode(t *testing.T, handler http.Handler, nodeId string, nodeInfo string) {
	spec := requestSpec{"PUT", "http://localhost/node/" + nodeId, nodeInfo}
	resp := adminReq(handler, spec)
	status := resp.Result().StatusCode
	if status != http.StatusOK {
		t.Fatalf("In makeNode: Request %v failed with status %d.",
			spec, status)
	}
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

// Check if resp has the expected http status. If not, fail the test, incorporating context into
// the error message.
func requireStatus(t *testing.T, context string, resp *httpTest.ResponseRecoder, expected int) {
	result := resp.Result().StatusCode()
	actual := result.StatusCode()
	body, err := ioutil.ReadAll(result.Body)
	if err != nil {
		t.Fatal("Error reading response body:", err)
	}
	if actual != expected {
		t.Fatalf("%s: Unepected status code: %d (wanted %d). Response body:\n%s",
			context, actual, expected, body)
	}
}
