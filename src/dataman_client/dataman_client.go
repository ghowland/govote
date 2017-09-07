package dataman_client

import (
	"github.com/jacksontj/dataman/src/query"
	"github.com/jacksontj/dataman/src/storage_node"
	"github.com/jacksontj/dataman/src/storage_node/metadata"

	"encoding/json"
	"io/ioutil"
	"log"
	"fmt"
	"context"
	"strconv"

	"github.com/ghowland/govote/src/utility"

)

var DatasourceInstance = map[string]*storagenode.DatasourceInstance{}

func InitDataman(PgConnect string) {
	config := storagenode.DatasourceInstanceConfig{
		StorageNodeType: "postgres",
		StorageConfig: map[string]interface{}{
			"pg_string": PgConnect,
		},
	}

	schema_str, err := ioutil.ReadFile("./data/schema.json")
	if err != nil {
		log.Panic(err)
	}

	//fmt.Printf("Schema STR: %s\n\n", schema_str)

	var meta metadata.Meta
	err = json.Unmarshal(schema_str, &meta)
	if err != nil {
		panic(err)
	}

	if datasource, err := storagenode.NewLocalDatasourceInstance(&config, &meta); err == nil {
		DatasourceInstance["opsdb"] = datasource
	} else {
		panic(err)
	}
}


func DatamanGet(collection_name string, record_id int, options map[string]interface{}) map[string]interface{} {
	fmt.Printf("DatamanGet: %s: %d\n", collection_name, record_id)

	get_map :=  map[string]interface{} {
		"db":             "opsdb",
		"shard_instance": "public",
		"collection":     collection_name,
		//"_id":            record_id,
		"pkey":           map[string]interface{}{"_id": record_id},
		"join":			  options["join"],
	}

	fmt.Printf("Dataman Get: %v\n\n", get_map)

	dataman_query := &query.Query{query.Get, get_map}

	result := DatasourceInstance["opsdb"].HandleQuery(context.Background(), dataman_query)

	fmt.Printf("Dataman GET: ERRORS: %v\n", result.Error)
	fmt.Printf("Dataman GET: %v\n", result.Return[0])

	return result.Return[0]
}

func DatamanSet(collection_name string, record map[string]interface{}) map[string]interface{} {
	// Remove the _id field, if it is nil.  This means it should be new/insert
	if record["_id"] == nil || record["_id"] == "<nil>" || record["_id"] == "\u003cnil\u003e" {
		fmt.Printf("DatamanSet: Removing _id key: %s\n", record["_id"])
		delete(record, "_id")
	} else {
		fmt.Printf("DatamanSet: Not Removing _id: %s\n", record["_id"])
	}

	// Duplicate this map, because we are messing with a live map, that we dont expect to change in this function...
	//TODO(g):REMOVE: Once I dont need to manipulate the map in this function anymore...
	record = utility.MapCopy(record)

	// Fix data manually, for now
	for k, v := range record {
		if v == "true" {
			record[k] = true
		} else if v == "false" {
			record[k] = false
		}
	}

	// Fixup the record, if its not a new one, by getti
	// ng any values
	//TODO(g):REMOVE: This is fixing up implementation problems in Dataman, which Thomas will fix...
	if record["_id"] != nil && record["_id"] != "" {
		fmt.Printf("Ensuring all fields are present (HACK): %s: %v\n", collection_name, record["_id"])

		// Record ID will be an integer
		var record_id int64
		var err interface{}
		switch record["_id"].(type) {
		case string:
			record_id, err = strconv.ParseInt(record["_id"].(string), 10, 32)
			if err != nil {
				panic(err)
			}
		default:
			record_id = utility.GetResult(record["_id"], type_int).(int64)
		}

		options := make(map[string]interface{})

		record_current := DatamanGet(collection_name, int(record_id), options)

		//// Set all the fields we have in the existing record, into our new record, if they dont exist, which defeats Thomas' current bug not allowing me to save data unless all fields are present
		//for k, v := range record_current {
		//	if record[k] == nil {
		//		record[k] = v
		//		fmt.Printf("Adding field: %s: %s: %v\n", collection_name, k, v)
		//	}
		//}

		// Remove any fields that arent present in the record_current
		for k, _ := range record {
			if _, has_key := record_current[k]; !has_key {
				fmt.Printf("Removing field: %s: %s: %v\n", collection_name, k, record[k])
				delete(record, k)
			}
		}
	} else {
		// This is a new record, we just tested for it above, remove empty string _id
		delete(record, "_id")
	}

	// Remove fields I know I put in here, that I dont want to go in
	//TODO(g):REMOVE: Same as the others
	delete(record, "_table")
	delete(record, "_web_data_widget_instance_id")

	// Form the Dataman query
	dataman_query := &query.Query{
		query.Set,
		map[string]interface{} {
			"db":             "opsdb",
			"shard_instance": "public",
			"collection":     collection_name,
			"record":         record,
		},
	}

	fmt.Printf("Dataman SET: Record: %v\n", record)
	fmt.Printf("Dataman SET: Record: JSON: %v\n", utility.JsonDump(record))

	result := DatasourceInstance["opsdb"].HandleQuery(context.Background(), dataman_query)

	//result_bytes, _ := json.Marshal(result)
	//fmt.Printf("Dataman SET: %s\n", result_bytes)

	fmt.Printf("Dataman SET: ERROR: %v\n", result.Error)

	return result.Return[0]
}

func DatamanFilter(collection_name string, filter map[string]interface{}, options map[string]interface{}) []map[string]interface{} {

	fmt.Printf("DatamanFilter: %s:  Filter: %v  Join: %v\n\n", collection_name, filter, options["join"])
	//fmt.Printf("Sort: %v\n", options["sort"])


	filter_map := map[string]interface{} {
		"db":             "opsdb",
		"shard_instance": "public",
		"collection":     collection_name,
		"filter":         filter,
		"join":			  options["join"],
		//"sort":			  options["sort"],
		//"sort_reverse":	  []bool{true},
	}

	fmt.Printf("Dataman Filter: %v\n\n", filter_map)

	dataman_query := &query.Query{query.Filter, filter_map}

	result := DatasourceInstance["opsdb"].HandleQuery(context.Background(), dataman_query)

	fmt.Printf("Dataman FILTER: %v\n", result.Return)
	fmt.Printf("Dataman ERROR: %v\n", result.Error)

	return result.Return
}

