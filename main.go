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

	_ "github.com/mattn/go-sqlite3"

	"github.com/zenhack/obmd/internal/driver"
	"github.com/zenhack/obmd/internal/driver/dummy"
	"github.com/zenhack/obmd/internal/driver/ipmi"
)

// Contents of the config file
type Config struct {
	ListenAddr string
	AdminToken Token
}

var (
	configPath = flag.String("config", "config.json", "Path to config file")
	dbPath     = flag.String("dbpath", ":memory:", "Path to sqlite database")

	genToken = flag.Bool("gen-token", false,
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
	db, err := sql.Open("sqlite3", *dbPath)
	chkfatal(err)
	chkfatal(db.Ping())

	state, err := NewState(db, driver.Registry{
		"ipmi": ipmi.Driver,

		// TODO: maybe mask this behind a build tag, so it's not there
		// in production builds:
		"dummy": dummy.Driver,
	})
	chkfatal(err)
	srv := makeHandler(&config, NewDaemon(state))
	http.Handle("/", srv)
	chkfatal(http.ListenAndServe(config.ListenAddr, nil))
}
