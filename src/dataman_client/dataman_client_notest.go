package main

import (
	"github.com/ghowland/govote/src/dataman_client"
	"github.com/ghowland/govote/src/utility"
)

func main() {
	dataman_client.InitDataman(utility.ReadPathData("data/opsdb.connect"))

	users := dataman_client.DatamanFilter("user", make(map[string]interface{},, make(map[string]interface{})})

	fmt.Printf("Users: %v\n\n", users)
}

