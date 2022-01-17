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

var (
	username              = flag.String("username", "", "MySQL Username")
	password              = flag.String("password", "", "MySQL Password")
	iniFile               = flag.String("inifile", "/home/clustercheck/.my.cnf", ".my.cnf file")
	host                  = flag.String("host", "localhost", "MySQL Server")
	port                  = flag.Int("port", 3306, "MySQL Port")
	timeout               = flag.String("timeout", "10s", "MySQL connection timeout")
	availableWhenDonor    = flag.Bool("donor", false, "Cluster available while node is a donor")
	availableWhenReadonly = flag.Bool("readonly", false, "Cluster available while node is read only")
	requireMaster         = flag.Bool("requiremaster", false, "Cluster available only while node is master")
	bindPort              = flag.Int("bindport", 8000, "MySQLChk bind port")
	bindAddr              = flag.String("bindaddr", "", "MySQLChk bind address")
	forceFail             = false
	forceUp               = false
	debug                 = flag.Bool("debug", false, "Debug mode. Will also print successfull 200 HTTP responses to stdout")
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

	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/?timeout=%s", *username, *password, *host, *port, *timeout))
	if err != nil {
		panic(err.Error())
	}

	db.SetMaxIdleConns(10)

	readOnlyStmt, err := db.Prepare("show global variables like 'read_only'")
	if err != nil {
		log.Fatal(err)
	}

	wsrepLocalStateStmt, err := db.Prepare("show global status like 'wsrep_local_state'")
	if err != nil {
		log.Fatal(err)
	}

	wsrepLocalIndexStmt, err := db.Prepare("show global status like 'wsrep_local_index'")
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

func (c *Checker) Fail(w http.ResponseWriter, r *http.Request) {
	c.check(w, r, *requireMaster, true, forceUp)
}

func (c *Checker) Up(w http.ResponseWriter, r *http.Request) {
	c.check(w, r, *requireMaster, forceFail, true)
}

func (c *Checker) Master(w http.ResponseWriter, r *http.Request) {
	c.check(w, r, true, forceFail, forceUp)
}

func (c *Checker) Root(w http.ResponseWriter, r *http.Request) {
	c.check(w, r, *requireMaster, forceFail, forceUp)
}

func (c *Checker) check(w http.ResponseWriter, r *http.Request, requireMaster, forceFail, forceUp bool) {
	remoteIp, _, _ := net.SplitHostPort(r.RemoteAddr)

	var fieldName, readOnly string
	var wsrepLocalState int
	var wsrepLocalIndex int

	if forceUp {
		if *debug {
			log.Println(remoteIp, "Cluster node OK by forceUp true")
		}
		fmt.Fprint(w, "Cluster node OK by forceUp true\n")
		return
	}

	if forceFail {
		if *debug {
			log.Println(remoteIp, "Cluster node FAIL by forceFail true")
		}
		http.Error(w, "Cluster node FAIL by forceFail true\n", http.StatusServiceUnavailable)
		return
	}

	err := c.wsrepLocalStateStmt.QueryRow().Scan(&fieldName, &wsrepLocalState)
	if err != nil {
		log.Println(remoteIp, err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if wsrepLocalState == 2 && *availableWhenDonor {
		if *debug {
			log.Println(remoteIp, "Cluster node in Donor mode")
		}
		fmt.Fprint(w, "Cluster node in Donor mode\n")
		return
	}

	if wsrepLocalState != 4 {
		if *debug {
			log.Println(remoteIp, "Cluster node is unavailable")
		}
		http.Error(w, "Cluster node is unavailable", http.StatusServiceUnavailable)
		return
	}

	if !*availableWhenReadonly {
		err = c.readOnlyStmt.QueryRow().Scan(&fieldName, &readOnly)
		if err != nil {
			log.Println(remoteIp, "Unable to determine read only setting")
			http.Error(w, "Unable to determine read only setting", http.StatusInternalServerError)
			return
		} else if readOnly == "ON" {
			log.Println(remoteIp, "Cluster node is read only")
			http.Error(w, "Cluster node is read only", http.StatusServiceUnavailable)
			return
		}
	}

	if requireMaster {
		err := c.wsrepLocalIndexStmt.QueryRow().Scan(&fieldName, &wsrepLocalIndex)
		if err != nil {
			log.Println(remoteIp, err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if wsrepLocalIndex != 0 {
			if *debug {
				log.Println(remoteIp, "Cluster node is not 'wsrep_local_index==0'")
			}
			http.Error(w, "Cluster node is not 'wsrep_local_index==0'", http.StatusServiceUnavailable)
			return
		}
		if *debug {
			log.Println(remoteIp, "Cluster node OK")
		}
		fmt.Fprint(w, "Cluster node is SYNCED and wsrep_local_index==0\n")
		return
	}

	if *debug {
		log.Println(remoteIp, "Cluster node OK")
	}

	fmt.Fprint(w, "Cluster node OK\n")
}
