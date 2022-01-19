# galera-clustercheck ##

Go daemon to get state of MariaDB Galera Cluster.

## Usage ##

Run daemon manually with `./galera-clustercheck`. By default it will connect to MariaDB server using a Unix domain socket at `/run/mysqld/mysqld.sock` and use credentials from a MySQL option file at `/etc/galera-clustercheck/my.cnf`.

See available options with `./galera-clustercheck -help`.

### Example ###

* Start daemon:
```
# ./galera-clustercheck
2022/01/19 10:19:51 Listening...
```
* Query status using curl:
```
# curl -i localhost:8000
HTTP/1.1 200 OK
Date: Wed, 19 Jan 2022 09:19:53 GMT
Content-Length: 21
Content-Type: text/plain; charset=utf-8

Node in Synced state
# curl -i localhost:8000/master
HTTP/1.1 200 OK
Date: Wed, 19 Jan 2022 09:19:55 GMT
Content-Length: 48
Content-Type: text/plain; charset=utf-8

Node in Synced state and 'wsrep_local_index==0'
```

## Setup with systemd ##

Copy `systemd.service` to `/etc/systemd/system/galera-clustercheck.service`, run `systemctl daemon-reload` and start/enable service it as you see fit.
