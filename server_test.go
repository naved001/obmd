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

	"github.com/CCI-MOC/obmd/internal/driver/mock"
)

// adminRequests is a sequence of admin-only requests that is used by various tests.
var adminRequests = []requestSpec{
	{"PUT", "http://localhost:8080/node/somenode", `{
		"type": "ipmi",
		"info": {
			"addr": "10.0.0.3",
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

func TestPowerStatus(t *testing.T) {
	handler := newHandler()
	makeNode(t, handler, "somenode", `{
		"type": "dummy",
		"info": {
			"addr": "10.0.0.3",
			"user": "ipmiuser",
			"pass": "secret"
		}
	}`)
	token := getToken(t, handler, "somenode")
	testStringOn := string("{\"power_status\":\"on\"}\n")
	testStringOff := string("{\"power_status\":\"off\"}\n")

	// First make sure dummy node is off
	request := requestSpec{"GET",
		"http://localhost/node/somenode/power_status", ""}
	resp := tokenReq(handler, token, request)
	body, err := ioutil.ReadAll(resp.Body)
	errpanic(err)
	if string(body) != testStringOff {
		t.Fatalf("GetPowerStatus: Incorrect power status; "+
			"wanted %v but got %v.", testStringOff, string(body))
	}

	// Now test powering on
	request = requestSpec{"POST",
		"http://localhost/node/somenode/power_cycle", `{"force": false}`,
	}
	// Power on ...
	resp = tokenReq(handler, token, request)
	body, err = ioutil.ReadAll(resp.Body)
	errpanic(err)

	// Get power status ...
	request = requestSpec{"GET",
		"http://localhost/node/somenode/power_status", ""}
	resp = tokenReq(handler, token, request)
	body, err = ioutil.ReadAll(resp.Body)
	errpanic(err)

	if string(body) != testStringOn {
		t.Fatalf("GetPowerStatus: Incorrect power status; "+
			"wanted %v but got %v.", testStringOn, string(body))
	}

	// Now test powering off
	request = requestSpec{"POST",
		"http://localhost/node/somenode/power_off", "",
	}
	// Power off ...
	resp = tokenReq(handler, token, request)
	body, err = ioutil.ReadAll(resp.Body)
	errpanic(err)

	// Get power status ...
	request = requestSpec{"GET",
		"http://localhost/node/somenode/power_status", ""}
	resp = tokenReq(handler, token, request)
	body, err = ioutil.ReadAll(resp.Body)
	errpanic(err)

	if string(body) != testStringOff {
		t.Fatalf("GetPowerStatus: Incorrect power status; "+
			"wanted %v but got %v.", testStringOff, string(body))
	}

}

func TestPowerActions(t *testing.T) {
	handler := newHandler()
	makeNode(t, handler, "somenode", `{
		"type": "ipmi",
		"info": {
			"addr": "10.0.0.3",
			"user": "ipmiuser",
			"pass": "secret"
		}
	}`)
	token := getToken(t, handler, "somenode")

	badToken, _ := Token{}.MarshalText() // All zeros

	testCases := []struct {
		context string
		token   string
		status  int
		action  mock.PowerAction
		request requestSpec
	}{
		// Power off the node, and make sure that the operation went through.
		{
			"power off",
			token,
			http.StatusOK,
			mock.Off,
			requestSpec{"POST", "/node/somenode/power_off", ""},
		},
		// Try to reboot the node with a bad token.
		{
			"power cycle (invalid token)",
			string(badToken),
			http.StatusUnauthorized,
			mock.Off, // should be unchanged.
			requestSpec{
				"POST", "/node/somenode/power_cycle", `{"force": false}`,
			},
		},
		// Now do it with the right token:
		{
			"power cycle (force, with good token)",
			token,
			http.StatusOK,
			mock.ForceReboot,
			requestSpec{
				"POST", "/node/somenode/power_cycle", `{"force": true}`,
			},
		},
		// Check the other operations:
		{
			"power cycle (soft, with good token)",
			token,
			http.StatusOK,
			mock.SoftReboot,
			requestSpec{
				"POST", "/node/somenode/power_cycle", `{"force": false}`,
			},
		},
		{
			"set bootdev to A",
			token,
			http.StatusOK,
			mock.BootDevA,
			requestSpec{
				"PUT", "/node/somenode/boot_device", `{"bootdev": "A"}`,
			},
		},
		{
			"set bootdev to B",
			token,
			http.StatusOK,
			mock.BootDevA,
			requestSpec{
				"PUT", "/node/somenode/boot_device", `{"bootdev": "A"}`,
			},
		},
		{
			"set bootdev to something invalid.",
			token,
			http.StatusBadRequest,
			mock.BootDevA, // should be unchanged.
			requestSpec{
				"PUT", "/node/somenode/boot_device", `{"bootdev": "invalid"}`,
			},
		},
	}

	for _, v := range testCases {
		resp := tokenReq(handler, v.token, v.request)
		status := resp.Result().StatusCode
		if status != v.status {
			t.Fatalf("%s: Unexpected status code; wanted %d but got %d.",
				v.context, v.status, status)
		}
		action := mock.LastPowerActions["10.0.0.3"]
		if action != v.action {
			t.Fatalf("%s: Incorrect power action; wanted %s but got %s.",
				v.context, v.action, action)
		}
	}
}
