package main

import (
	_ "shFresh/routers"
	"github.com/astaxie/beego"
	_ "shFresh/models"
)

func main() {
	beego.Run()
}

