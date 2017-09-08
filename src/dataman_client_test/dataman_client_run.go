package main

import (
	"github.com/ghowland/govote/src/dataman_client"
	"github.com/ghowland/govote/src/utility"
	"fmt"
)

func main() {
	dataman_client.InitDataman(utility.ReadPathData("data/opsdb.connect"))

	filter := make(map[string]interface{})
	options := make(map[string]interface{})
	users := dataman_client.DatamanFilter("user", filter, options)

	fmt.Printf("Users: %v\n\n", users)
}

