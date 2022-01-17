package main

import "testing"

func TestParseIni(t *testing.T) {
	*iniFile = "./my.cnf"

	parseConfigFile()
	if *username == "username123" && *password == "password1234" {
		return
	}
	t.Errorf("Username and password wrong. Expected: username123:password1234 got %s:%s", *username, *password)
}
