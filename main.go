package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	_ "github.com/mattn/go-sqlite3"
)

// An IpmiDialer establishes a connection to a console based on an IpmiInfo
// struct. This is an interface for testing purposes.
type IpmiDialer interface {
	DialIpmi(info *IpmiInfo) (io.ReadCloser, error)
}

// Contents of the config file
type Config struct {
	ListenAddr string
	AdminToken string
}

// Ipmi connection info
type IpmiInfo struct {
	Addr string `json:"addr"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

// Information about a node
type Node struct {
	Label        string
	Version      uint64
	Ipmi         IpmiInfo
	Conn         io.ReadCloser // Active console connection, if any.
	CurrentToken Token         // Token for console access.
}

// A cryptographically random 128-bit value.
type Token [128 / 8]byte

// Request/response body for the calls that include version information.
type VersionArgs struct {
	// Expected version number.
	Version uint64 `json:"version"`
}

// Convert v to JSON. This is a convienence wrapper around json.Marshal,
// which returns an error even though for this data type it can't ever
// fail.
func (v VersionArgs) asJson() []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic("BUG: json marshaling failed: " + err.Error())
	}
	return data
}

// Response body for successful new token requests.
type TokenResp struct {
	Token Token `json:"token"`
}

var (
	configPath  = flag.String("config", "config.json", "Path to config file")
	dummyDialer = flag.Bool("dummydialer", false, "Use dummy dialer (for development)")
	dbPath      = flag.String("dbpath", ":memory:", "Path to sqlite database")

	// A dummy token to be used when there is no "valid" token. This is
	// generated in init(), and never escapes the program. It exists so
	// we don't have to have special purpose logic for the case where
	// there is no correct token; we just set the node's token to this
	// value which is inaccessable to *anyone*.
	noToken Token
)

func init() {
	_, err := rand.Read(noToken[:])
	chkfatal(err)
}

func (t Token) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("%0x", t)), nil
}

func (t *Token) UnmarshalText(text []byte) error {
	var buf []byte
	_, err := fmt.Fscanf(bytes.NewBuffer(text), "%32x", &buf)
	if err != nil {
		return err
	}
	copy(t[:], buf)
	return nil
}

// Bumps the version of the node, disconnecting any existing connections and
// invalidating any tokens.
func (n *Node) BumpVersion(db *sql.DB) error {
	n.ClearToken()
	n.Disconnect()
	n.Version++
	_, err := db.Exec(
		`UPDATE nodes SET version = ? WHERE label = ?`,
		n.Version, n.Label)
	return err
}

// Returns a new node with the given ipmi information, at version 0, with no
// valid token.
func NewNode(info IpmiInfo) *Node {
	ret := &Node{
		Ipmi: info,
	}
	copy(ret.CurrentToken[:], noToken[:])
	return ret
}

// Generate a new token, invaidating the old one if any, and disconnecting
// clients using it. If an error occurs, the state of the node/token will
// be unchanged.
func (n *Node) NewToken() (Token, error) {
	var token Token
	_, err := rand.Read(token[:])
	if err != nil {
		return token, err
	}
	n.ClearToken()
	copy(n.CurrentToken[:], token[:])
	return n.CurrentToken, nil
}

// Return whether a token is valid.
func (n *Node) ValidToken(token Token) bool {
	return subtle.ConstantTimeCompare(n.CurrentToken[:], token[:]) == 1
}

// Clear any existing token, and disconnect any clients
func (n *Node) ClearToken() {
	n.Disconnect()
	copy(n.CurrentToken[:], noToken[:])
}

// Disconnects any live console connections to the node
func (n *Node) Disconnect() {
	if n.Conn != nil {
		n.Conn.Close()
		n.Conn = nil
	}
}

// Connect to the node's console using `dialer`. Disconnect any previously existing
// connection.
func (n *Node) Connect(dialer IpmiDialer) (io.ReadCloser, error) {
	// Disconnect the old client, if any.
	n.Disconnect()
	return dialer.DialIpmi(&n.Ipmi)
}

// Exit with an error message if err != nil.
func chkfatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// Create an HTTP handler for the core logic of our system, using the provided
// configuration and the dialer for establishing connections.
func makeHandler(config *Config, dialer IpmiDialer, db *sql.DB) (http.Handler, error) {
	state, err := NewState(db)
	if err != nil {
		return nil, err
	}

	// Wrap a request handler with calls to state.Lock/state.Unlock
	withLock := func(handler func(w http.ResponseWriter, req *http.Request)) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			state.Lock()
			defer state.Unlock()
			handler(w, req)
		})
	}

	r := mux.NewRouter()

	adminR := r.MatcherFunc(func(req *http.Request, m *mux.RouteMatch) bool {
		user, pass, ok := req.BasicAuth()
		// FIXME: `pass ==` opens a timing attack. We should be using
		// some off-the shelf password library.
		return ok && user == "admin" && pass == config.AdminToken
	}).Subrouter()

	// Register a new node, or update the information in an existing one.
	adminR.Methods("PUT").Path("/node/{node_id}").
		Handler(withLock(func(w http.ResponseWriter, req *http.Request) {
			var info IpmiInfo
			err := json.NewDecoder(req.Body).Decode(&info)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			nodeId := mux.Vars(req)["node_id"]
			err = state.SetNode(nodeId, info)
			if err != nil {
				log.Println("Error in SetNode():", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}))

	// Delete/unregister a node.
	adminR.Methods("DELETE").Path("/node/{node_id}").
		Handler(withLock(func(w http.ResponseWriter, req *http.Request) {
			nodeId := mux.Vars(req)["node_id"]
			node, err := state.GetNode(nodeId)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			node.Disconnect()
			node.ClearToken()
			state.DelNode(nodeId)
		}))

	// Bump the version of a node.
	adminR.Methods("POST").Path("/node/{node_id}/version").
		Handler(withLock(func(w http.ResponseWriter, req *http.Request) {
			nodeId := mux.Vars(req)["node_id"]
			node, err := state.GetNode(nodeId)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			err = node.BumpVersion(db)
			if err != nil {
				log.Println("Bumping version for node", nodeId, ":", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Write(VersionArgs{Version: node.Version}.asJson())
		}))

	// Get a new console token
	adminR.Methods("POST").Path("/node/{node_id}/console-endpoints").
		Handler(withLock(func(w http.ResponseWriter, req *http.Request) {
			node, err := state.GetNode(mux.Vars(req)["node_id"])
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			args := VersionArgs{}
			err = json.NewDecoder(req.Body).Decode(&args)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if args.Version != node.Version {
				// Client is mistaken about the version the
				// node; give them the correct version and
				// make them try again after regaining
				// their bearings:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				w.Write(VersionArgs{Version: node.Version}.asJson())
				return
			}
			token, err := node.NewToken()
			if err != nil {
				log.Printf("Failed to generate token for node: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(&TokenResp{
				Token: token,
			})
		}))

	r.Methods("POST").Path("/node/{node_id}/power_off").
		HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		})

	// Connect to the console. This is the one thing that doesn't require the admin token.
	r.Methods("GET").Path("/node/{node_id}/console").
		HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// We don't use withLock here, since we want to
			// release the lock before returning.
			var token Token
			err := (&token).UnmarshalText([]byte(req.URL.Query().Get("token")))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			var Conn io.ReadCloser

			if !func() bool {
				// Wrapping this in a function and using defer simplifies the
				// control flow with unlock; we therefore put our critical
				// section here.
				state.Lock()
				defer state.Unlock()
				node, err := state.GetNode(mux.Vars(req)["node_id"])
				if err != nil {
					w.WriteHeader(http.StatusNotFound)
					return false
				}
				if !node.ValidToken(token) {
					w.WriteHeader(http.StatusBadRequest)
					return false
				}

				// OK, auth checks out; make the connection.
				Conn, err = node.Connect(dialer)
				node.Conn = Conn
				return err == nil
			}() {
				// Something in the critical section failed; bail out.
				return
			}

			// We have a connection; stream the data to the client.
			defer Conn.Close()
			w.Header().Set("Content-Type", "application/octet-stream")
			io.Copy(w, Conn)
		})

	return r, nil
}

func main() {
	flag.Parse()
	buf, err := ioutil.ReadFile(*configPath)
	chkfatal(err)
	var config Config
	chkfatal(json.Unmarshal(buf, &config))
	db, err := sql.Open("sqlite3", *dbPath)
	chkfatal(err)
	chkfatal(db.Ping())

	var dialer IpmiDialer
	if *dummyDialer {
		dialer = &DummyIpmiDialer{}
	} else {
		dialer = &IpmitoolDialer{}
	}
	srv, err := makeHandler(&config, dialer, db)
	chkfatal(err)
	http.Handle("/", srv)
	chkfatal(http.ListenAndServe(config.ListenAddr, nil))
}
