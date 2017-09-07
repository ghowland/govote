package main

import (
	"fmt"

//"encoding/json"
//"sort"
//"github.com/jcasts/gosrv"
//_ "github.com/lib/pq"
//"container/list"
//"github.com/junhsieh/goexamples/fieldbinding/fieldbinding"
//"io/ioutil"
//"log"
//"net/http"
//"os"
//"strings"
//"text/template"
//"bytes"
//"strconv"
//"io"
//"github.com/segmentio/ksuid"
//"context"
//"os/user"
	"time"
	//"github.com/jacksontj/dataman/src/storage_node"
	//"io/ioutil"
	//"log"
	//"encoding/json"

	"github.com/ghowland/govote/src/utility/utility"
)


var PgConnect = string

func RunWorkerPools() {
	fmt.Printf("Run Worker Pools\n")

}


func init() {cd 
	PgConnect = utility.ReadPathData("data/opsdb.connect")

}


func main() {
	fmt.Printf("Starting Job Worker\n")

	go RunWorkerPools()

	for true {
		time.Sleep(time.Second)
	}
}

