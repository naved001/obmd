package main

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

// request body for the power cycle call
type PowerCycleArgs struct {
	Force bool `json:"force"`
}

// request body for the set bootdev call
type SetBootdevArgs struct {
	Dev string `json:"bootdev"`
}

// Connection info for an OBM.
type ConnInfo struct {
	// The name of the driver to use:
	Type string `json:"type"`

	// Driver-specific connection info:
	Info []byte
}

// Response body for successful new token requests.
type TokenResp struct {
	Token Token `json:"token"`
}

func makeHandler(config *Config, daemon *Daemon) http.Handler {
	r := mux.NewRouter()

	// ----- helper functions ------

	// Handle the errors returned by Daemon methods, reporting the correct http status.
	// This calls w.WriteHeader, so headers must be set before calling this method.
	relayError := func(w http.ResponseWriter, context string, err error) {
		switch err {
		case nil:
			w.WriteHeader(http.StatusOK)
		case ErrNoSuchNode:
			w.WriteHeader(http.StatusNotFound)
		case ErrInvalidToken:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Unexpected error returned (%s): %v\n", context, err)
		}
	}

	// Fetch the node_id out of a request's captured variables. This requires that
	// req was matched by a route that had "{node_id}" somewhere in its path.
	nodeId := func(req *http.Request) string {
		return mux.Vars(req)["node_id"]
	}

	getToken := func(req *http.Request) (token Token, err error) {
		err = (&token).UnmarshalText([]byte(req.URL.Query().Get("token")))
		return token, err
	}

	// Router for admin-only requests. Because we validate the admin token here,
	// anything with an invalid admin token will simply not match, returning 404
	// (Not found). TODO: think about whether we want that as an explicit security
	// feature. It masks the presence or abscence of nodes, which is nice (but if
	// we're to rely on that, we need to mitigate timing attacks).
	adminR := r.MatcherFunc(func(req *http.Request, m *mux.RouteMatch) bool {
		user, pass, ok := req.BasicAuth()
		if !(ok && user == "admin") {
			return false
		}
		var tok Token
		err := (&tok).UnmarshalText([]byte(pass))
		if err != nil {
			return false
		}
		return subtle.ConstantTimeCompare(tok[:], config.AdminToken[:]) == 1
	}).Subrouter()

	// ------ Admin-only requests ------

	// Register a new node, or update the information in an existing one.
	adminR.Methods("PUT").Path("/node/{node_id}").
		HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			info, err := ioutil.ReadAll(req.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			relayError(w, "daemon.SetNode()", daemon.SetNode(nodeId(req), info))
		})

	adminR.Methods("DELETE").Path("/node/{node_id}").
		HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			relayError(w, "daemon.DeleteNode()", daemon.DeleteNode(nodeId(req)))
		})

	adminR.Methods("POST").Path("/node/{node_id}/token").
		HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			token, err := daemon.GetNodeToken(nodeId(req))
			if err != nil {
				relayError(w, "daemon.GetNodeToken()", err)
			} else {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(&TokenResp{
					Token: token,
				})
			}
		})

	adminR.Methods("DELETE").Path("/node/{node_id}/token").
		HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			err := daemon.InvalidateNodeToken(nodeId(req))
			relayError(w, "daemon.InvalidateNodeToken()", err)
		})

	// ------ "Regular user" requests ------

	r.Methods("GET").Path("/node/{node_id}/console").
		HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			token, err := getToken(req)
			if err != nil {
				relayError(w, "getToken()", err)
				return
			}
			conn, err := daemon.DialNodeConsole(nodeId(req), &token)
			if err != nil {
				relayError(w, "daemon.DialNodeConsole()", err)
			} else {
				defer conn.Close()
				w.Header().Set("Content-Type", "application/octet-stream")

				// Copy stream to the client. Unfortunately we can't just use
				// io.Copy here, because we need to call Flush() between writes.
				// otherwise, the client won't receive console data in a timely
				// manner, because the ResponseWriter may buffer it.
				var buf [4096]byte
				for err == nil {
					var n int
					n, err = conn.Read(buf[:])
					if n != 0 {
						_, err = w.Write(buf[:n])
					}
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
				}

				if err != io.EOF {
					log.Println("Error reading from console:", err)
				}
			}
		})

	return r
}

/*
// Create an HTTP handler for the core logic of our system, using the provided
// configuration and the driver for establishing connections.
func makeHandler(config *Config, driver driver.Driver, db *sql.DB) (http.Handler, error) {

	r.Methods("POST").Path("/node/{node_id}/power_off").
		Handler(withToken(func(w http.ResponseWriter, req *http.Request, token *Token) {
			err := dialer.PowerOff(mux.Vars(req)["node_id"], token)
			if err != nil {
				log.Println("power_off:", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))

	r.Methods("POST").Path("/node/{node_id}/power_cycle").
		Handler(withValidToken(func(w http.ResponseWriter, req *http.Request, node *Node) {
			var args PowerCycleArgs
			err := json.NewDecoder(req.Body).Decode(&args)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			err = dialer.PowerCycle(&node.Ipmi, args.Force)
			if err != nil {
				log.Println("power_cycle:", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))

	r.Methods("PUT").Path("/node/{node_id}/boot_device").
		Handler(withValidToken(func(w http.ResponseWriter, req *http.Request, node *Node) {
			var args SetBootdevArgs
			err := json.NewDecoder(req.Body).Decode(&args)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			err = dialer.SetBootdev(&node.Ipmi, args.Dev)
			switch err {
			case ErrInvalidBootdev:
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(err.Error()))
			case nil:
				// Success!
				return
			default:
				log.Println("set_bootdev:", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))


	return r, nil
}
*/
