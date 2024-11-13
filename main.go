package main

import (
	"m4s-converter/common"
)

func main() {
	var c common.Config
	c.InitLog()
	c.InitConfig()
	go c.PanicHandler()
	c.Synthesis()
}
