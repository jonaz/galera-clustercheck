package main

import (
	"fmt"
	"testing"
)

func TestParseIni(t *testing.T) {
	*iniFile = "./my.cnf"

	parseConfigFile()
	fmt.Println(*username)
	fmt.Println(*password)
}
