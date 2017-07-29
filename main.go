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
	"sync"

	"github.com/gorilla/mux"

	_ "github.com/mattn/go-sqlite3"
)

// An IpmiDialer establishes a connection to a console based on an IpmiInfo
// struct. This is an interface for testing purposes.
type IpmiDialer interface {
	DialIpmi(info *IpmiInfo) (io.ReadWriteCloser, error)
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
	Owner        string
	Ipmi         IpmiInfo
	Conn         io.ReadWriteCloser // Active console connection, if any.
	CurrentToken Token              // Token for console access.
}

// A cryptographically random 128-bit value.
type Token [128 / 8]byte

// Request body for the calls that include owner information.
type OwnerArgs struct {
	// Name of owner. Must not be "".
	Owner string `json:"owner"`
}

// Response body for successful new token requests.
type TokenResp struct {
	Token Token `json:"token"`
}

// Global state; used to look up nodes/console tokens.
type State struct {
	sync.Mutex
	Nodes map[string]*Node
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

// Updates the owner of the node, disconnecting any existing connections and
// invalidating any tokens.
func (n *Node) UpdateOwner(newOwner string) {
	n.ClearToken()
	n.Disconnect()
	n.Owner = newOwner
}

// Returns a new node with the given ipmi information, no owner, and no valid
// token.
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
func (n *Node) Connect(dialer IpmiDialer) (io.ReadWriteCloser, error) {
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

// Create an HTTP handler for the core logic of our system, using the provided,
// configuration and the dialer for establishing connections.
func makeHandler(config *Config, dialer IpmiDialer, db *sql.DB) http.Handler {

	state := &State{
		Nodes: make(map[string]*Node),
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
			node := Node{}
			err := json.NewDecoder(req.Body).Decode(&node.Ipmi)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			nodeId := mux.Vars(req)["node_id"]
			oldNode, ok := state.Nodes[nodeId]

			if ok {
				*oldNode = node
			} else {
				state.Nodes[nodeId] = &node
			}
		}))

	// Delete/unregister a node.
	adminR.Methods("DELETE").Path("/node/{node_id}").
		Handler(withLock(func(w http.ResponseWriter, req *http.Request) {
			nodeId := mux.Vars(req)["node_id"]
			node, ok := state.Nodes[nodeId]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			node.Disconnect()
			node.ClearToken()
			delete(state.Nodes, nodeId)
		}))

	// Change the owner of a node
	adminR.Methods("PUT").Path("/node/{node_id}/owner").
		Handler(withLock(func(w http.ResponseWriter, req *http.Request) {
			nodeId := mux.Vars(req)["node_id"]
			node, ok := state.Nodes[nodeId]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			args := OwnerArgs{}
			err := json.NewDecoder(req.Body).Decode(&args)
			if err != nil || args.Owner == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			node.UpdateOwner(args.Owner)
		}))

	// Remove the owner of a node
	adminR.Methods("DELETE").Path("/node/{node_id}/owner").
		Handler(withLock(func(w http.ResponseWriter, req *http.Request) {
			node, ok := state.Nodes[mux.Vars(req)["node_id"]]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			node.UpdateOwner("")
		}))

	// Get a new console token
	adminR.Methods("POST").Path("/node/{node_id}/console-endpoints").
		Handler(withLock(func(w http.ResponseWriter, req *http.Request) {
			node, ok := state.Nodes[mux.Vars(req)["node_id"]]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			args := OwnerArgs{}
			err := json.NewDecoder(req.Body).Decode(&args)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if args.Owner != node.Owner {
				// Client is mistaken about who owns the node; make them try
				// again after regaining their bearings.
				w.WriteHeader(http.StatusConflict)
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

			var Conn io.ReadWriteCloser

			if !func() bool {
				// Wrapping this in a function and using defer simplifies the
				// control flow with unlock; we therefore put our critical
				// section here.
				state.Lock()
				defer state.Unlock()
				node, ok := state.Nodes[mux.Vars(req)["node_id"]]
				if !ok {
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

	return r
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
	chkfatal(initDB(db))
	srv := makeHandler(&config, dialer, db)
	http.Handle("/", srv)
	chkfatal(http.ListenAndServe(config.ListenAddr, nil))
}
