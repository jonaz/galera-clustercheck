package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

const (
	STATE_JOINING = 1
	STATE_DONOR   = 2
	STATE_JOINED  = 3
	STATE_SYNCED  = 4
)

var (
	username              = flag.String("username", "", "MySQL Username")
	password              = flag.String("password", "", "MySQL Password")
	iniFile               = flag.String("inifile", "/home/clustercheck/.my.cnf", ".my.cnf file")
	socket                = flag.String("socket", "", "Unix domain socket")
	host                  = flag.String("host", "localhost", "MySQL Server")
	port                  = flag.Int("port", 3306, "MySQL Port")
	timeout               = flag.String("timeout", "10s", "MySQL connection timeout")
	availableWhenDonor    = flag.Bool("donor", false, "Cluster available while node is a donor")
	availableWhenReadonly = flag.Bool("readonly", false, "Cluster available while node is read only")
	requireMaster         = flag.Bool("requiremaster", false, "Cluster available only while node is master")
	bindPort              = flag.Int("bindport", 8000, "MySQLChk bind port")
	bindAddr              = flag.String("bindaddr", "", "MySQLChk bind address")
	debug                 = flag.Bool("debug", false, "Debug mode. Will also print successfull 200 HTTP responses to stdout")
	forceFail             = false
	forceUp               = false
	dataSourceName        = ""
)

type Checker struct {
	wsrepLocalIndexStmt *sql.Stmt
	wsrepLocalStateStmt *sql.Stmt
	readOnlyStmt        *sql.Stmt
}

func main() {
	flag.Parse()

	if *username == "" && *password == "" {
		parseConfigFile()
	}

	if *socket != "" {
		dataSourceName = fmt.Sprintf("%s:%s@unix(%s)/?timeout=%s", *username, *password, *socket, *timeout)
	} else {
		dataSourceName = fmt.Sprintf("%s:%s@tcp(%s:%d)/?timeout=%s", *username, *password, *host, *port, *timeout)
	}

	db, err := sql.Open("mysql", dataSourceName)
	if err != nil {
		panic(err.Error())
	}

	db.SetMaxIdleConns(10)

	readOnlyStmt, err := db.Prepare("SHOW GLOBAL VARIABLES LIKE 'read_only'")
	if err != nil {
		log.Fatal(err)
	}

	wsrepLocalStateStmt, err := db.Prepare("SHOW GLOBAL STATUS LIKE 'wsrep_local_state'")
	if err != nil {
		log.Fatal(err)
	}

	wsrepLocalIndexStmt, err := db.Prepare("SHOW GLOBAL STATUS LIKE 'wsrep_local_index'")
	if err != nil {
		log.Fatal(err)
	}

	checker := &Checker{wsrepLocalIndexStmt, wsrepLocalStateStmt, readOnlyStmt}

	log.Println("Listening...")
	http.HandleFunc("/", checker.Root)
	http.HandleFunc("/master", checker.Master)
	http.HandleFunc("/fail", checker.Fail)
	http.HandleFunc("/up", checker.Up)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%d", *bindAddr, *bindPort), nil))
}

func parseConfigFile() {

	content, err := ioutil.ReadFile(*iniFile)
	if err != nil {
		log.Fatalf("error reading config: %v", err)
	}
	lines := strings.Split(string(content), "\n")

	for _, line := range lines {
		if len(line) > 3 && line[0:4] == "user" {
			tmp := strings.Split(line, "=")
			*username = strings.Trim(tmp[1], " ")
		}
		if len(line) > 7 && line[0:8] == "password" {
			tmp := strings.Split(line, "=")
			*password = strings.Trim(tmp[1], " ")
		}
	}
}

func (c *Checker) Root(w http.ResponseWriter, r *http.Request) {
	c.Clustercheck(w, r, *requireMaster, forceFail, forceUp)
}

func (c *Checker) Master(w http.ResponseWriter, r *http.Request) {
	c.Clustercheck(w, r, true, forceFail, forceUp)
}

func (c *Checker) Fail(w http.ResponseWriter, r *http.Request) {
	c.Clustercheck(w, r, *requireMaster, true, forceUp)
}

func (c *Checker) Up(w http.ResponseWriter, r *http.Request) {
	c.Clustercheck(w, r, *requireMaster, forceFail, true)
}

func (c *Checker) Clustercheck(w http.ResponseWriter, r *http.Request, requireMaster, forceFail, forceUp bool) {
	remoteIp, _, _ := net.SplitHostPort(r.RemoteAddr)

	var fieldName, readOnly string
	var wsrepLocalState int
	var wsrepLocalIndex int

	if forceUp {
		if *debug {
			log.Println(remoteIp, "Node OK by forceUp true")
		}
		fmt.Fprint(w, "Node OK by forceUp true\n")
		return
	}

	if forceFail {
		if *debug {
			log.Println(remoteIp, "Node FAIL by forceFail true")
		}
		http.Error(w, "Node FAIL by forceFail true", http.StatusServiceUnavailable)
		return
	}

	readOnlyErr := c.readOnlyStmt.QueryRow().Scan(&fieldName, &readOnly)
	if readOnlyErr != nil {
		log.Println(remoteIp, readOnlyErr.Error())
		http.Error(w, readOnlyErr.Error(), http.StatusInternalServerError)
		return
	}

	if readOnly == "ON" && !*availableWhenReadonly {
		log.Println(remoteIp, "Node is read_only")
		http.Error(w, "Node is read_only", http.StatusServiceUnavailable)
		return
	}

	wsrepLocalStateErr := c.wsrepLocalStateStmt.QueryRow().Scan(&fieldName, &wsrepLocalState)
	if wsrepLocalStateErr != nil {
		log.Println(remoteIp, wsrepLocalStateErr.Error())
		http.Error(w, wsrepLocalStateErr.Error(), http.StatusInternalServerError)
		return
	}

	switch wsrepLocalState {
	case STATE_JOINING:
		if *debug {
			log.Println(remoteIp, "Node in Joining state")
		}
		http.Error(w, "Node in Joining state", http.StatusServiceUnavailable)
		return
	case STATE_DONOR:
		if *availableWhenDonor {
			if *debug {
				log.Println(remoteIp, "Node in Donor state")
			}
			fmt.Fprint(w, "Node in Donor state\n")
			return
		} else {
			if *debug {
				log.Println(remoteIp, "Node in Donor state")
			}
			http.Error(w, "Node in Donor state", http.StatusServiceUnavailable)
			return
		}
	case STATE_JOINED:
		if *debug {
			log.Println(remoteIp, "Node in Joined state")
		}
		http.Error(w, "Node in Joined state", http.StatusServiceUnavailable)
		return
	case STATE_SYNCED:
		if requireMaster {
			wsrepLocalIndexErr := c.wsrepLocalIndexStmt.QueryRow().Scan(&fieldName, &wsrepLocalIndex)
			if wsrepLocalIndexErr != nil {
				log.Println(remoteIp, wsrepLocalIndexErr.Error())
				http.Error(w, wsrepLocalIndexErr.Error(), http.StatusInternalServerError)
				return
			}
			if wsrepLocalIndex == 0 {
				if *debug {
					log.Println(remoteIp, "Node in Synced state and 'wsrep_local_index==0'")
				}
				fmt.Fprintf(w, "Node in Synced state and 'wsrep_local_index==0'\n")
				return
			} else if wsrepLocalIndex != 0 {
				if *debug {
					log.Println(remoteIp, "Node in Synced state but not 'wsrep_local_index==0'")
				}
				http.Error(w, "Node in Synced state but not 'wsrep_local_index==0'", http.StatusServiceUnavailable)
				return
			}
		}
		if *debug {
			log.Println(remoteIp, "Node in Synced state")
		}
		fmt.Fprint(w, "Node in Synced state\n")
		return
	default:
		if *debug {
			log.Println(remoteIp, fmt.Sprintf("Node in an unknown state (%d)", wsrepLocalState))
		}
		http.Error(w, fmt.Sprintf("Node in an unknown state (%d)", wsrepLocalState), http.StatusServiceUnavailable)
		return
	}
}
