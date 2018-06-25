package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"github.com/CCI-MOC/obmd/internal/driver"
	"github.com/CCI-MOC/obmd/internal/driver/dummy"
	"github.com/CCI-MOC/obmd/internal/driver/ipmi"
	"github.com/CCI-MOC/obmd/internal/driver/mock"
)

// Contents of the config file
type Config struct {
	DBType     string
	DBPath     string
	ListenAddr string
	AdminToken Token
	Insecure   bool
	TLSCert    string
	TLSKey     string
}

var (
	configPath = flag.String("config", "config.json", "Path to config file")
	genToken   = flag.Bool("gen-token", false,
		"Generate a random token, instead of starting the daemon.")
)

// Exit with an error message if err != nil.
func chkfatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	flag.Parse()

	if *genToken {
		// The user passed -gen-token; generate a token and exit.
		var tok Token
		_, err := rand.Read(tok[:])
		chkfatal(err)
		text, err := tok.MarshalText()
		chkfatal(err)
		fmt.Println(string(text))
		return
	}

	buf, err := ioutil.ReadFile(*configPath)
	chkfatal(err)
	var config Config
	chkfatal(json.Unmarshal(buf, &config))
	// DB Types: sqlite3 or postgres
	db, err := sql.Open(config.DBType, config.DBPath)
	chkfatal(err)
	chkfatal(db.Ping())

	state, err := NewState(db, driver.Registry{
		"ipmi": ipmi.Driver,

		// TODO: maybe mask this behind a build tag, so it's not there
		// in production builds:
		"dummy": dummy.Driver,
		"mock":  mock.Driver,
	})
	chkfatal(err)
	srv := makeHandler(&config, NewDaemon(state))
	http.Handle("/", srv)

	if config.Insecure {
		if config.TLSCert != "" {
			log.Fatal("Error: Do not specify TLS certificate file",
				" when Insecure is true.")
		}
		if config.TLSKey != "" {
			log.Fatal("Error: Do not specify TLS key file",
				" when Insecure is true.")
		}
		chkfatal(http.ListenAndServe(config.ListenAddr, nil))
	} else {
		if config.TLSCert == "" {
			log.Fatal("Error: No TLS certificate file specified.")
		}
		if config.TLSKey == "" {
			log.Fatal("Error: No TLS key file specified.")
		}
		chkfatal(http.ListenAndServeTLS(config.ListenAddr,
			config.TLSCert,
			config.TLSKey,
			nil))
	}
}
