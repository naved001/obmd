package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// adminRequests is a sequence of admin-only requests that is used by various tests.
var adminRequests = []requestSpec{
	{"PUT", "http://localhost:8080/node/somenode/version", `{
		"version": 2
	}`},
	{"PUT", "http://localhost:8080/node/somenode", `{
		"type": "ipmi",
		"info": {
			"host": "10.0.0.3",
			"user": "ipmiuser",
			"pass": "secret"
		}
	}`},
	{"PUT", "http://localhost:8080/node/somenode/version", `{
		"version": 2
	}`},
	{"POST", "http://localhost:80080/node/somenode/console-endpoints", `{
		"version": 2
	}`},
	{"DELETE", "http://localhost:8080/node/somenode", ""},
	{"PUT", "http://localhost:8080/node/somenode/version", `{
		"version": 3
	}`},
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
// We mitigate this by including a version number that the admin handler believes is
// current part of the token granting request; step (2) will have changed the version,
// and so the console server will detect the descrepency, rejecting the request.
func TestOwnerRace(t *testing.T) {
	handler := newHandler()

	// preliminary requests; a node is created, and the version is bumped
	// twice.
	setupRequests := []requestSpec{
		{"PUT", "http://localhost/node/somenode", `{
			"type": "ipmi",
			"info": {
				"addr": "10.0.0.3",
				"user": "ipmiuser",
				"pass": "secret"
			}
		}`},
		{"PUT", "http://localhost/node/somenode/version", `{
			"version": 2
		}`},
		{"PUT", "http://localhost/node/somenode/version", `{
			"version": 3
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

	// Now, try to request a token with version 1 as the expected owner. This should
	// fail with a 409 CONFLICT status, as the current version should be 3.
	req := httptest.NewRequest("POST", "http://localhost/node/somenode/console-endpoints",
		bytes.NewBuffer([]byte(`{"version": 1}`)))
	tokenText, err := theConfig.AdminToken.MarshalText()
	if err != nil {
		panic(err)
	}
	req.SetBasicAuth("admin", string(tokenText))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	result := resp.Result()
	if result.StatusCode != http.StatusConflict {
		t.Fatalf("Version mismatch did not result in an HTTP 409 CONFLICT "+
			"(Got %v instead).", result.StatusCode)
	}
	version := VersionArgs{}
	err = json.NewDecoder(result.Body).Decode(&version)
	if err != nil {
		t.Fatal("Error decoding body of response:", err)
	}
	if version.Version != 3 {
		t.Fatal("Unexpected version number; expected 2 but got", version.Version)
	}
}

// Go through the motions of granting access to the console, viewing it, and then having access
// revoked.
func TestViewConsole(t *testing.T) {
	handler := newHandler()

	spec := requestSpec{
		"PUT", "http://localhost/node/somenode", `{
			"type": "ipmi",
			"info": {
				"addr": "10.0.0.3",
				"user": "ipmiuser",
				"pass": "secret"
			}
		}`,
	}
	req := spec.toAdminAuth()
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	status := resp.Result().StatusCode
	if status != http.StatusOK {
		t.Fatalf("During setup in TestViewConsole: Request %v failed with status %d.",
			spec, status)
	}
	req = (&requestSpec{"POST", "http://localhost/node/somenode/console-endpoints", `{
		"version": 1
	}`}).toAdminAuth()
	resp = httptest.NewRecorder()
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

	req = (&requestSpec{"PUT", "http://localhost/node/somenode/version", `{
		"version": 2
	}`}).toAdminAuth()
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	status = resp.Result().StatusCode
	if status != http.StatusOK {
		t.Fatalf("version bump request failed with status: %d", status)
	}

	// Clear out any buffered data:
	bufReader.Discard(bufReader.Buffered())
	// Now try to keep reading. The first of these *might* succeed, since the mock console
	// goroutine may have made a call to write that we didn't match with a read before the
	// server called Close(). But the second one should always fail; we should have been
	// disconnected by the version bump, and the mock console goroutine should have seen
	// it on its next call to Write.
	line, err := bufReader.ReadString('\n')
	if err != nil {
		t.Logf("Read another line (%q) after the connection was closed; "+
			"no cause for alarm.", line)
	}
	line, err = bufReader.ReadString('\n')
	if err == nil {
		t.Fatalf("Connection should have been closed, but we were able to successfully "+
			"read data: %q", line)
	}
}

// Check that bumping the version works if and only if the requested version is one greater
// than the current version.
func TestVersionMustBePlus1(t *testing.T) {
	h := newHandler()

	// Setup: register the node:
	adminRequireStatus(t, h, http.StatusOK, requestSpec{
		"PUT", "http://localhost/node/somenode", `{
			"type": "ipmi",
			"info": {
				"addr": "10.0.0.3",
				"user": "ipmiuser",
				"pass": "secret"
			}
		}`,
	})

	// Starting version is one, so this should fail:
	adminRequireStatus(t, h, http.StatusConflict, requestSpec{
		"PUT", "http://localhost/node/somenode/version", `{
			"version": 3
		}`,
	})

	// But this correct:
	adminRequireStatus(t, h, http.StatusOK, requestSpec{
		"PUT", "http://localhost/node/somenode/version", `{
			"version": 2
		}`,
	})

	// ...and now that the version has been bumped to 2, this should work:
	adminRequireStatus(t, h, http.StatusOK, requestSpec{
		"PUT", "http://localhost/node/somenode/version", `{
			"version": 3
		}`,
	})
}

func TestGetVersion(t *testing.T) {
	h := newHandler()
	args := VersionArgs{}

	adminRequireStatus(t, h, http.StatusOK, requestSpec{
		"PUT", "http://localhost/node/somenode", `{
			"type": "ipmi",
			"info": {
				"addr": "10.0.0.3",
				"user": "ipmiuser",
				"pass": "secret"
			}
		}`,
	})

	// Helper for verifying the version.
	expectVersion := func(expectedVersion uint64) {
		resp := adminReq(h, requestSpec{"GET", "http://localhost/node/somenode/version", ""})
		err := json.NewDecoder(resp.Body).Decode(&args)
		if err != nil {
			t.Fatal("Error decoding body:", err)
		}
		if args.Version != expectedVersion {
			t.Fatal("Version is incorrect; expected",
				expectedVersion, "but got", args.Version)
		}
	}

	// The version starts at 1.
	expectVersion(1)

	// Bump it.
	adminRequireStatus(t, h, http.StatusOK, requestSpec{
		"PUT", "http://localhost/node/somenode/version", `{
			"version": 2
		}`,
	})

	// And check it again:
	expectVersion(2)
}
