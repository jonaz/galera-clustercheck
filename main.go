package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"

	_ "github.com/go-sql-driver/mysql"
)

var (
	username              = flag.String("username", "clustercheckuser", "MySQL Username")
	password              = flag.String("password", "clustercheckpassword!", "MySQL Password")
	host                  = flag.String("host", "localhost", "MySQL Server")
	port                  = flag.Int("port", 3306, "MySQL Port")
	timeout               = flag.String("timeout", "10s", "MySQL connection timeout")
	availableWhenDonor    = flag.Bool("donor", false, "Cluster available while node is a donor")
	availableWhenReadonly = flag.Bool("readonly", false, "Cluster available while node is read only")
	forceFailFile         = flag.String("failfile", "/dev/shm/proxyoff", "Create this file to manually fail checks")
	forceUpFile           = flag.String("upfile", "/dev/shm/proxyon", "Create this file to manually pass checks")
	bindPort              = flag.Int("bindport", 9200, "MySQLChk bind port")
	bindAddr              = flag.String("bindaddr", "", "MySQLChk bind address")
)

func init() {
	flag.Parse()
}

func main() {
	flag.Parse()

	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/?timeout=%s", *username, *password, *host, *port, *timeout))
	if err != nil {
		panic(err.Error())
	}

	db.SetMaxIdleConns(10)

	readOnlyStmt, err := db.Prepare("show global variables like 'read_only'")
	if err != nil {
		log.Fatal(err)
	}

	wsrepStmt, err := db.Prepare("show global status like 'wsrep_local_state'")
	if err != nil {
		log.Fatal(err)
	}

	checker := &Checker{wsrepStmt, readOnlyStmt}

	log.Println("Listening...")
	http.HandleFunc("/", checker.Handler)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%d", *bindAddr, *bindPort), nil))
}

type Checker struct {
	//Db           *sql.DB
	WsRepStmt    *sql.Stmt
	ReadOnlyStmt *sql.Stmt
}

func (c *Checker) Handler(w http.ResponseWriter, r *http.Request) {

	var fieldName, readOnly string
	var wsrepState int

	err := c.WsRepStmt.QueryRow().Scan(&fieldName, &wsrepState)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if wsrepState == 2 && *availableWhenDonor == true {
		fmt.Fprint(w, "Cluster node in Donor mode\n")
		return
	} else if wsrepState != 4 {
		http.Error(w, "Cluster node is unavailable", http.StatusServiceUnavailable)
		return
	}

	if *availableWhenReadonly == false {
		err = c.ReadOnlyStmt.QueryRow().Scan(&fieldName, &readOnly)
		if err != nil {
			http.Error(w, "Unable to determine read only setting", http.StatusInternalServerError)
			return
		} else if readOnly == "ON" {
			http.Error(w, "Cluster node is read only", http.StatusServiceUnavailable)
			return
		}
	}

	fmt.Fprint(w, "Cluster node OK\n")
}
