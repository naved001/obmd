package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// adminRequests is a sequence of admin-only requests that is used by various tests.
var adminRequests = []requestSpec{
	{"PUT", "http://localhost:8080/node/somenode", `{
		"type": "ipmi",
		"info": {
			"host": "10.0.0.3",
			"user": "ipmiuser",
			"pass": "secret"
		}
	}`},
	{"POST", "http://localhost:8080/node/somenode/token", ""},
	{"DELETE", "http://localhost:8080/node/somenode", ""},
	{"DELETE", "http://localhost:8080/node/somenode/token", ""},
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

	expected := []int{200, 200, 200, 404}

	for i, v := range adminRequests {
		resp := adminReq(handler, v)
		actual := resp.Result().StatusCode
		if actual != expected[i] {
			t.Fatalf("Unexpected status code for authenticated adminRequests[%d]; "+
				"wanted %d but got %d", i, expected[i], actual)
		}
	}
}

// Go through the motions of granting access to the console, viewing it, and then having access
// revoked.
func TestViewConsole(t *testing.T) {
	handler := newHandler()
	makeNode(t, handler, "somenode", `{
		"type": "ipmi",
		"info": {
			"addr": "10.0.0.3",
			"user": "ipmiuser",
			"pass": "secret"
		}
	}`)

	streamConsole := func(token string) io.ReadCloser {
		req := httptest.NewRequest(
			"GET",
			"http://localhost/node/somenode/console?token="+token,
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
		return r
	}

	numReadsFirstClient := make(chan int)
	go func() {
		r := bufio.NewReader(streamConsole(getToken(t, handler, "somenode")))
		i := 0
		defer func() { numReadsFirstClient <- i }()
		for {
			line, err := r.ReadString('\n')
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("Error reading from console: %v", err)
			}
			expected := fmt.Sprintf("%d\n", i)
			if line != expected {
				t.Fatalf("Unexpected data read from console. Wanted %q but got %q",
					expected, line)
			}
			i++
		}
	}()
	time.Sleep(time.Second)
	resp := adminReq(handler, requestSpec{"DELETE", "http://localhost/node/somenode/token", ""})
	requireStatus(t, "Invalidating token", resp, http.StatusOK)

	r := bufio.NewReader(streamConsole(getToken(t, handler, "somenode")))
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatal("Error reading from console:", err)
	}
	var readsSecond int
	n, err := fmt.Sscanf(line, "%d\n", &readsSecond)
	if err != nil {
		t.Fatalf("Error parsing output %q from console: %v", line, err)
	}
	if n != 1 {
		t.Fatal("Incorrect number of items parsed by Sscanf:", n)
	}

	readsFirst := <-numReadsFirstClient
	if readsFirst >= readsSecond {
		t.Fatal("First console reader read a line that was not before "+
			"what was read by the second reader:",
			readsFirst, "vs.", readsSecond)
	}
}
