package main

/*

TODO:

- Make the accessors, that run off the last output
	- Terminate the function, so that accessors can start, using ".__access."
	- `__sql.dbselect.'SELECT * FROM table WHERE id = 5'.__.0.json_data_field.fieldname.10.anotherfieldname.etc`
- Change the quotes from single to double-quotes, so that we can write raw SQL commands, and still have quoting work in them
- `__query.1.__slice.-5,-1` - get the last 5 elements
- `__query.1.__sort.fieldname1.fieldname2` sort on multiple fieldnames

*/

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/jcasts/gosrv"
	//_ "github.com/mattn/go-sqlite3"
	_ "github.com/lib/pq"
	//"io"
	"container/list"
	"github.com/junhsieh/goexamples/fieldbinding/fieldbinding"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"text/template"
	//"github.com/jacksontj/goUDN"
	//"container/list"
	//  "net/url"
	//  "io"
	//  "bytes"
	//  "path"
	"bytes"
	"strconv"
	"io"
)

type ApiRequest struct {
	// User information
	UserId        int
	UserName      string
	UserAuthToken string

	// Ingress API information
	IngressHostname  string
	IngressProtocol  string
	IngressUri       string
	IngressArgs      string
	IngressHeaderMap map[string]string
	IngressCookieMap map[string]string
	IngressDataMap   map[string]interface{}

	// Database information -- For security reasons, dont put DBs in this map that this user doesnt have access to
	DatabaseList map[int64]*sql.DB
}

const (
	part_unknown  = iota
	part_function = iota
	part_item     = iota
	part_string   = iota
	part_compound = iota
	part_list     = iota
	part_map      = iota
	part_map_key  = iota
)

var PartTypeName map[int]string

func NewUdnPart() UdnPart {
	return UdnPart{
		Children: list.New(),
	}
}

type UdnPart struct {
	Depth          int
	PartType       int

	Value          string

	// List of UdnPart structs, list is easier to use dynamically
	//TODO(g): Switch this to an array.  Lists suck...
	Children       *list.List

	Id             string

	// Puts the data here after it's been evaluated
	ValueFinal     interface{}
	ValueFinalType int

	// Allows casting the type, not sure about this, but seems useful to cast ints from strings for indexing.  We'll see
	CastValue      string

	ParentUdnPart *UdnPart
	NextUdnPart   *UdnPart

	// For block functions (ex: Begin: __iterate, End: __end_iterate).  For each block begin/end, save them during parsing, so we know which __end_ function ends which block, if there are multiple per UDN statement
	BlockBegin	  *UdnPart
	BlockEnd	  *UdnPart
}

func (part *UdnPart) String() string {
	output := ""

	if part.PartType == part_function {
		output += fmt.Sprintf("%s: %s [%s]\n", PartTypeName[part.PartType], part.Value, part.Id)
	} else {
		output += fmt.Sprintf("%s: %s\n", PartTypeName[part.PartType], part.Value)
	}

	return output
}

type UdnResult struct {
	// This is the result
	Result interface{}

	Type int

	// This is the next UdnPart to process.  If nil, the executor will just continue from current UdnPart.NextUdnPart
	NextUdnPart *UdnPart

	// Error messages, we will stop processing if not nil
	Error string
}

const (
	type_int				= iota
	type_float				= iota
	type_string				= iota
	type_string_force		= iota	// This forces it to a string, even if it will be ugly, will print the type of the non-string data too.  Testing this to see if splitting these into 2 yields better results.
	type_array				= iota	// []interface{} - takes: lists, arrays, maps (key/value tuple array, strings (single element array), ints (single), floats (single)
	type_map				= iota	// map[string]interface{}
)

func GetResult(input interface{}, type_value int) interface{} {
	type_str := fmt.Sprintf("%T", input)

	// Unwrap UdnResult, if it is wrapped
	if type_str == "main.UdnResult" {
		input = input.(UdnResult).Result
	} else if type_str == "*main.UdnResult" {
		input = input.(*UdnResult).Result
	}

	switch type_value {
	case type_int:
		switch input.(type) {
		case string:
			result, err := strconv.ParseInt(input.(string), 10, 64)
			if err != nil {
				return nil
			}
			return result
		case int:
			return input
		case int8:
			return input
		case int16:
			return input
		case int32:
			return input
		case int64:
			return input
		case uint:
			return input
		case uint8:
			return input
		case uint16:
			return input
		case uint32:
			return input
		case uint64:
			return input
		case float64:
			return int(input.(float32))
		case float32:
			return int(input.(float64))
		default:
			return nil
		}
	case type_float:
		switch input.(type) {
		case string:
			result, err := strconv.ParseFloat(input.(string), 64)
			if err != nil {
				return nil
			}
			return result
		case float64:
			return input
		case float32:
			return input
		case int:
			return float64(input.(int))
		case int8:
			return float64(input.(int8))
		case int16:
			return float64(input.(int16))
		case int32:
			return float64(input.(int32))
		case int64:
			return float64(input.(int64))
		case uint:
			return float64(input.(uint))
		case uint8:
			return float64(input.(uint8))
		case uint16:
			return float64(input.(uint16))
		case uint32:
			return float64(input.(uint32))
		case uint64:
			return float64(input.(uint64))
		default:
			return nil
		}
	case type_string:
		switch input.(type) {
		case string:
			return input
		default:
			// If this is already an array, return it as-is
			if strings.HasPrefix(type_str, "[]") {
				fmt.Printf("GetResult: Will attempt to coerce to string from []: %s\n", SnippetData(input, 60))
				concat := ""
				all_strings := true
				not_strings := make([]interface{}, 0)

				// If all these are strings, just concatenate them
				for _, arr_value := range input.([]interface{}) {
					switch arr_value.(type) {
					case string:
						concat += arr_value.(string)
					default:
						all_strings = false
						fmt.Printf("GetResult: Coerce failure.  Array element is not a string: %s\n", SnippetData(arr_value, 60))
						not_strings = AppendArray(not_strings, SnippetData(arr_value, 30))
					}
				}

				if all_strings {
					fmt.Printf("GetResult: Concatenated array to string\n")
					return concat
				} else {
					fmt.Printf("GetResult: Array cannot be coerced into a string:  Not all elements are strings: %v\n", not_strings)
				}
			}

			//NOTE(g): Use type_string_force if you want to coerce this into a string, because this destroys too much data I think.  Testing this as 2 things anyways, easy to fold back into 1 if it doesnt work out.
			return input
		}
	case type_string_force:
		switch input.(type) {
		case string:
			return input
		default:
			return fmt.Sprintf("%v", input)
		}
	case type_map:
		//fmt.Printf("GetResult: Map: %s\n", type_str)

		// If this is already a map, return it
		if type_str == "map[string]interface {}" {
			return input
		} else if type_str == "*map[string]interface {}" {
			return *input.(*map[string]interface{})
		} else if type_str == "*list.List" {
			// Else, if this is a list, convert the elements into a map, with keys as string indexes values ("0", "1")
			result := make(map[string]interface{})

			count := 0
			for child := input.(*list.List).Front() ; child != nil ; child = child.Next() {
				count_str := strconv.Itoa(count)

				// Add the index as a string, and the value to the map
				result[count_str] = child.Value
				count++
			}

			return result

		} else if strings.HasPrefix(type_str, "[]") {
			// Else, if this is an array, convert the elements into a map, with keys as string indexes values ("0", "1")
			result := make(map[string]interface{})

			for count, value := range input.([]interface{}) {
				count_str := strconv.Itoa(count)

				// Add the index as a string, and the value to the map
				result[count_str] = value
			}

			return result

		} else {
			// Else, this is not a map yet, so turn it into one, of the key "value"
			result := make(map[string]interface{})
			result["value"] = input
			return result
		}
	case type_array:
		// If this is already an array, return it as-is
		if strings.HasPrefix(type_str, "[]") {
			return input
		} else if type_str == "*list.List" {
			// Else, if this is a List, then create an array and store all the list elements into the array
			result := make([]interface{}, input.(*list.List).Len())

			count := 0
			for child := input.(*list.List).Front() ; child != nil ; child = child.Next() {
				// Add the index as a string, and the value to the map
				result[count] = child.Value
				count++
			}
			return result

		} else if type_str == "map[string]interface {}" {
			// Else, if this is a Map, then create an array and all the key/values as a single item map, with keys: "key", "value"
			result := make([]interface{}, len(input.(map[string]interface{})))

			count := 0
			for key, value := range input.(map[string]interface{}) {
				// Make a tuple array
				item := make(map[string]interface{})
				item["key"] = key
				item["value"] = value

				// Save the tuple to our array
				result[count] = item

				count++
			}

			return result

		} else {
			// Else, just make a single item array and stick it in
			result := make([]interface{}, 1)
			result[0] = input
			return result
		}
	}


	return nil
}

// Execution group allows for Blocks to be run concurrently.  A Group has Concurrent Blocks, which has UDN pairs of strings, so 3 levels of arrays for grouping
type UdnExecutionGroup struct {
	Blocks [][][]string
}

type UdnFunc func(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult

var UdnFunctions = map[string]UdnFunc{}

func DescribeUdnPart(part *UdnPart) string {

	depth_margin := strings.Repeat("  ", part.Depth)

	output := ""

	if part.PartType == part_function {
		output += fmt.Sprintf("%s%s: %-20s [%s]\n", depth_margin, PartTypeName[part.PartType], part.Value, part.Id)
	} else {
		output += fmt.Sprintf("%s%s: %-20s\n", depth_margin, PartTypeName[part.PartType], part.Value)
	}

	if part.BlockBegin != nil {
		output += fmt.Sprintf("%sBlock:  Begin: %s   End: %s\n", depth_margin, part.BlockBegin.Id, part.BlockEnd.Id)
	}

	if part.Children.Len() > 0 {
		output += fmt.Sprintf("%sArgs: %d\n", depth_margin, part.Children.Len())
		for child := part.Children.Front(); child != nil; child = child.Next() {
			output += DescribeUdnPart(child.Value.(*UdnPart))
		}
	}

	if part.NextUdnPart != nil {
		output += fmt.Sprintf("%sNext Command:\n", depth_margin)
		output += DescribeUdnPart(part.NextUdnPart)
	}

	return output
}

type StringFile struct {
	String string
}

func (s *StringFile) Write(ingress []byte) (count int, err error) {
	s.String += string(ingress)

	return len(ingress), nil
}

func NewTextTemplateMap() *TextTemplateMap {
	return &TextTemplateMap{
		Map: make(map[string]interface{}),
	}
}

func NewTextTemplateMapItem() TextTemplateMap {
	return TextTemplateMap{
		Map: make(map[string]interface{}),
	}
}

type TextTemplateMap struct {
	Map map[string]interface{}
}

func InitUdn() {
	UdnFunctions = map[string]UdnFunc{
		"__query":        UDN_QueryById,
		"__debug_output": UDN_DebugOutput,
		"__if":           UDN_IfCondition,
		"__end_if":       nil,
		"__else":         UDN_ElseCondition,
		"__end_else":     nil,
		"__else_if":      UDN_ElseIfCondition,
		"__end_else_if":  nil,
		"__iterate":      UDN_Iterate,
		"__end_iterate":  nil,
		"__access":       UDN_Access,
		"__get":          UDN_Get,
		"__set":          UDN_Set,
		//"__watch": UDN_WatchSyncronization,
		//"__timeout": UDN_WatchTimeout,				//TODO(g): Should this just be an arg to __watch?  I think so...  Like if/else, watch can control the flow...
		"__test_return":           UDN_TestReturn, // Return some data as a result
		"__test":           UDN_Test,
		"__test_different": UDN_TestDifferent,
		// Migrating from old functions
		//TODO(g): These need to be reviewed, as they are not necessarily the best way to do this, this is just the easiest/fastest way to do this
		"__widget": UDN_Widget,
		// New functions for rendering web pages (finally!)
		//"__template": UDN_StringTemplate,					// Does a __get from the args...
		"__template": UDN_StringTemplateFromValue,					// Does a __get from the args...
		"__template_string": UDN_StringTemplateFromValue,	// Templates the string passed in as arg_0
		"__string_append": UDN_StringAppend,
		"__string_clear": UDN_StringClear,		// Initialize a string to empty string, so we can append to it again
		"__concat": UDN_StringConcat,
		"__input": UDN_Input,			//TODO(g): This takes any input as the first arg, and then passes it along, so we can type in new input to go down the pipeline...
		"__function": UDN_StoredFunction,			//TODO(g): This uses the udn_stored_function.name as the first argument, and then uses the current input to pass to the function, returning the final result of the function.		Uses the web_site.udn_stored_function_domain_id to determine the stored function
		"__execute": UDN_Execute,			//TODO(g): Executes ("eval") a UDN string, assumed to be a "Set" type (Target), will use __input as the Source, and the passed in string as the Target UDN

		// New
		//"__format": UDN_MapStringFormat,			//TODO(g): Updates a map with keys and string formats.  Uses the map to format the strings.  Takes N args, doing each arg in sequence, for order control
		//"__template_map": UDN_MapTemplate,		//TODO(g): Like format, for templating.  Takes 3*N args: (key,text,map), any number of times.  Performs template and assigns key into the input map
		//"__map_update": UDN_MapUpdate,			//TODO(g): Sets keys in the map, from the args[0] map

		//"__map_update_prefix": UDN_MapUpdatePrefix,			//TODO(g): Merge a the specified map into the input map, with a prefix, so we can do things like push the schema into the row map, giving us access to the field names and such

		//"__function_domain": UDN_StoredFunctionDomain,			//TODO(g): Just like function, but allows specifying the udn_stored_function_domain.id as well, so we can use different namespaces.
		//"__capitalize": UDN_StringCapitalize,			//TODO(g): This capitalizes words, title-style
		//"__pluralize": UDN_StringPluralize,			//TODO(g): This pluralizes words, or tries to at least
		//"__starts_with": UDN_StringStartsWith,			//TODO(g): Returns bool if a string starts with the specified arg[0] string
		//"__ends_with": UDN_StringEndsWith,			//TODO(g): Returns bool if a string starts with the specified arg[0] string
		//"__split": UDN_StringSplit,			//TODO(g): Split a string on a value, with a maximum number of splits, and the slice of which to use, with a join as optional value.   The args go:  0) separate to split on,  2)  maximum number of times to split (0=no limit), 3) location to write the split out data (ex: `temp.action.fieldname`) , 3) index of the split to pull out (ex: -1, 0, 1, for the last, first or second)  4) optional: the end of the index to split on, which creates an array of items  5) optional: the join value to join multiple splits on (ex: `_`, which would create a string like:  `second_third_forth` out of a [1,4] slice)
		//"__get_session_data": UDN_SessionDataGet,			//TODO(g): Get something from a safe space in session data (cannot conflict with internal data)
		//"__set_session_data": UDN_SessionDataGet,			//TODO(g): Set something from a safe space in session data (cannot conflict with internal data)
		//"__continue": UDN_IterateContinue,		// Skip to next iteration
		// -- Dont think I need this -- //"__break": UDN_IterateBreak,				//TODO(g): Break this iteration, we are done.  Is this needed?  Im not sure its needed, and it might suck

		// Allows safe concurrency operations...
		//"__set_temp": UDN_Set_Temp,		// Sets a temporary variable.  Is safe for this sequence, and cannot conflict with our UDN setting the same names as temp vars in other threads
		//"__get_temp": UDN_Set_Temp,		// Gets a temporary variable.  Is safe for this sequence, and cannot conflict with our UDN setting the same names as temp vars in other threads
	}

	PartTypeName = map[int]string{
		int(part_unknown): "Unknown",
		int(part_function): "Function",
		int(part_item): "Item",
		int(part_string): "String",
		int(part_compound): "Compound",
		int(part_list): "List",
		int(part_map): "Map",
		int(part_map_key): "Map Key",
	}
}

func init() {
	// Initialize UDN
	InitUdn()
}

func main() {
	////DEBUG: Testing
	//TestUdn()

	s, err := gosrv.NewFromFlag()
	if err != nil {
		panic(err)
	}

	s.HandleFunc("/", handler)

	err = s.ListenAndServe()
	if err != nil {
		panic(err)
	}
}

func TestUdn() {
	fmt.Printf("\n\n\n\n\n======================\n======================\n\n----------------------\n\n\n\n     STARTING UDN TEST\n\n\n\n----------------------\n\n======================\n======================\n\n\n\n\n")

	// DB Web
	db_web, err := sql.Open("postgres", "user=postgres dbname=opsdb password='password' host=localhost sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db_web.Close()

	// Test the UDN Processor
	udn_schema := PrepareSchemaUDN(db_web)
	//fmt.Printf("\n\nUDN Schema: %v\n\n", udn_schema)

	// Load the test JSON data from a path, so it can be complex and can iterate quickly
	udn_json_group := ReadPathData("template/test.json")

	// Get UDN starting data values
	uri := "/"
	request_body := strings.NewReader("")
	param_map := make(map[string][]string)
	header_map := make(map[string][]string)
	cookie_array := make([]*http.Cookie, 0)

	//DEBUG: Add params and stuff to here...
	//param_map["id"] = 11

	// Load website
	web_site_id := 1
	sql := fmt.Sprintf("SELECT * FROM web_site WHERE id = %d", web_site_id)
	web_site_result := Query(db_web, sql)
	if web_site_result == nil {
		panic("Failed to load website")
	}

	// Get the path to match from the DB
	sql = fmt.Sprintf("SELECT * FROM web_site_page WHERE web_site_id = %d ORDER BY name LIMIT 1", web_site_id)
	web_site_page_result := Query(db_web, sql)
	//fmt.Printf("\n\nTest Against: Web Page Results: %v\n\n", web_site_page_result)


	// Create the starting UDN data set
	udn_data := GetStartingUdnData(db_web, db_web, web_site_result[0], web_site_page_result[0], uri, request_body, param_map, header_map, cookie_array)

	fmt.Printf("Starting UDN Data: %v\n\n", udn_data)

	// Save all our base web_widgets, so we can access them anytime we want
	sql = fmt.Sprintf("SELECT * FROM web_widget")
	all_widgets := Query(db_web, sql)
	udn_data["base_widget"] = MapArrayToMap(all_widgets, "name")


	_ = ProcessSchemaUDNSet(db_web, udn_schema, udn_json_group, &udn_data)
}

func ReadPathData(path string) string {
	file, err := os.Open(path)
	if err == nil {
		defer file.Close()

		file_info, err := file.Stat()
		if err != nil {
			log.Fatal(err)
		}

		// If this isnt a directory
		if !file_info.IsDir() {
			size := file_info.Size()

			data := make([]byte, size)
			_, err := file.Read(data)
			if err != nil {
				log.Fatal(err)
			}

			return string(data)
		}
	}

	return ""
}

func handler(w http.ResponseWriter, r *http.Request) {

	//url := fmt.Sprintf("%s", r.URL)

	url := r.URL.RequestURI()

	parts := strings.SplitN(url, "?", 2)

	uri := parts[0]

	relative_path := "./web/limitless5" + uri

	//log.Println("Testing path:", relative_path)

	is_static := false

	file, err := os.Open(relative_path)
	if err == nil {
		defer file.Close()

		file_info, err := file.Stat()
		if err != nil {
			log.Fatal(err)
		}

		// If this isnt a directory
		if !file_info.IsDir() {
			is_static = true

			size := file_info.Size()

			data := make([]byte, size)
			_, err := file.Read(data)
			if err != nil {
				log.Fatal(err)
			}

			if strings.HasSuffix(relative_path, ".css") {
				w.Header().Set("Content-Type", "text/css")
			} else if strings.HasSuffix(relative_path, ".js") {
				w.Header().Set("Content-Type", "text/javascript")
			} else if strings.HasSuffix(relative_path, ".jpg") {
				w.Header().Set("Content-Type", "image/jpg")
			} else if strings.HasSuffix(relative_path, ".png") {
				w.Header().Set("Content-Type", "image/png")
			} else if strings.HasSuffix(relative_path, ".woff2") {
				w.Header().Set("Content-Type", "font/woff2")
			} else {
				w.Header().Set("Content-Type", "text/html")
			}

			// Write the file into the body
			w.Write(data)
		}
	}

	// If this is not dynamic, then it's static
	if !is_static {
		// Handle all dynamic pages
		dynamicPage(uri, w, r)
	}
}

func dynamicPage(uri string, w http.ResponseWriter, r *http.Request) {

	// DB
	db, err := sql.Open("postgres", "user=postgres dbname=opsdb password='password' host=localhost sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// DB Web
	db_web, err := sql.Open("postgres", "user=postgres dbname=opsdb password='password' host=localhost sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db_web.Close()

	web_site_id := 1

	//TODO(g): Get the web_site_domain from host header
	//web_site_domain_id := 1

	// Get the path to match from the DB
	sql := fmt.Sprintf("SELECT * FROM web_site WHERE id = %d", web_site_id)
	web_site_result := Query(db_web, sql)
	if web_site_result == nil {
		panic("Failed to load website")
	}

	fmt.Printf("Type: %T\n\n", web_site_result)

	web_site_row := web_site_result[0]
	web_site := web_site_row

	fmt.Printf("\n\nGetting Web Site Page from URI: %s\n\n", uri)

	// Get the path to match from the DB
	sql = fmt.Sprintf("SELECT * FROM web_site_page WHERE web_site_id = %d AND name = '%s'", web_site_id, SanitizeSQL(uri))
	fmt.Printf("\n\nQuery: %s\n\n", sql)
	web_site_page_result := Query(db_web, sql)
	fmt.Printf("\n\nWeb Page Results: %v\n\n", web_site_page_result)

	// Check if this is a match for an API call
	found_api := false
	web_site_api_result := make([]map[string]interface{}, 0)
	if web_site["api_prefix_path"] == nil || strings.HasPrefix(uri, web_site["api_prefix_path"].(string)) {
		short_path := uri
		if web_site["api_prefix_path"] != nil {
			short_path = strings.Replace(uri, web_site["api_prefix_path"].(string), "", -1)
		}

		// Get the path to match from the DB
		sql = fmt.Sprintf("SELECT * FROM web_site_api WHERE web_site_id = %d AND name = '%s'", web_site_id, SanitizeSQL(short_path))
		fmt.Printf("\n\nQuery: %s\n\n", sql)
		web_site_api_result = Query(db_web, sql)

		if len(web_site_api_result) > 0 {
			found_api = true
		}
	}

	// If we found a matching page
	if found_api {
		fmt.Printf("\n\nFound API: %v\n\n", web_site_api_result[0])
		dynamicPage_API(db_web, db, web_site, web_site_api_result[0], uri, w, r)
	} else if len(web_site_page_result) > 0 {
		fmt.Printf("\n\nFound Dynamic Page: %v\n\n", web_site_page_result[0])
		dynamePage_RenderWidgets(db_web, db, web_site, web_site_page_result[0], uri, w, r)
	} else {
		fmt.Printf("\n\nPage not found: 404: %v\n\n", web_site_page_result)

		dynamicPage_404(uri, w, r)
	}
}

func MapListToDict(map_array []map[string]interface{}, key string) *map[string]interface{} {
	// Build a map of all our web site page widgets, so we can
	output_map := make(map[string]interface{})

	for _, map_item := range map_array {
		output_map[map_item[key].(string)] = map_item
	}

	return &output_map
}

func GetStartingUdnData(db_web *sql.DB, db *sql.DB, web_site map[string]interface{}, web_site_page map[string]interface{}, uri string, body io.Reader, param_map map[string][]string,  header_map map[string][]string, cookie_array []*http.Cookie) map[string]interface{} {

	// Data pool for UDN
	udn_data := make(map[string]interface{})

	// Prepare the udn_data with it's fixed pools of data
	//udn_data["widget"] = *NewTextTemplateMap()
	udn_data["data"] = make(map[string]interface{})
	udn_data["temp"] = make(map[string]interface{})
	udn_data["output"] = make(map[string]interface{})			// Staging output goes here, can share them with appending as well.
	//TODO(g): Make args accessible at the start of every ExecuteUdnPart after getting the args!
	udn_data["arg"] = make(map[string]interface{})				// Every function call blows this away, and sets the args in it's data, so it's accessable
	udn_data["function_arg"] = make(map[string]interface{})		// Function arguments, from Stored UDN Function __function, sets the incoming function args
	udn_data["page"] = make(map[string]interface{})				//TODO(g):NAMING: __widget is access here, and not from "widget", this can be changed, since thats what it is...

	udn_data["set_api_result"] = make(map[string]interface{})		// If this is an API call, set values in here, which will be encoded in JSON and sent back to the client on return
	udn_data["set_cookie"] = make(map[string]interface{})			// Set Cookies.  Any data set in here goes into a cookie.  Will use standard expiration and domain for now.
	udn_data["set_header"] = make(map[string]interface{})			// Set HTTP Headers.
	udn_data["set_http_options"] = make(map[string]interface{})		// Any other things we want to control from UDN, we put in here to be processed.  Can be anything, not based on a specific standard.

	//TODO(g): Move this so we arent doing it every page load

	// Get the params: map[string]interface{}
	udn_data["param"] = make(map[string]interface{})
	//TODO(g): Get the POST params too, not just GET...
	for key, value := range param_map {
		//fmt.Printf("\n----KEY: %s  VALUE:  %s\n\n", key, value[0])
		//TODO(g): Decide what to do with the extra headers in the array later, we may not want to allow this ever, but thats not necessarily true.  Think about it, its certainly not the typical case, and isnt required
		udn_data["param"].(map[string]interface{})[key] = value[0]
	}

	// Get the JSON Body, if it exists, from an API-style call in
	udn_data["api_input"] = make(map[string]interface{})
	json_body := make(map[string]interface{})
	decoder := json.NewDecoder(body)
	err := decoder.Decode(&json_body)
	// If we got it, then add all the keys to api_input
	if err == nil {
		for body_key, body_value := range json_body {
			udn_data["api_input"].(map[string]interface{})[body_key] = body_value
		}
	}

	// Get the cookies: map[string]interface{}
	udn_data["cookie"] = make(map[string]interface{})
	for _, cookie := range cookie_array {
		udn_data["cookie"].(map[string]interface{})[cookie.Name] = cookie.Value
	}

	// Get the headers: map[string]interface{}
	udn_data["header"] = make(map[string]interface{})
	for header_key, header_value_array := range header_map {
		//TODO(g): Decide what to do with the extra headers in the array later, these will be useful and are necessary to be correct
		udn_data["header"].(map[string]interface{})[header_key] = header_value_array[0]
	}

	// Verify that this user is logged in, render the login page, if they arent logged in
	udn_data["session"] = make(map[string]interface{})
	udn_data["user"] = make(map[string]interface{})
	udn_data["user_data"] = make(map[string]interface{})
	udn_data["web_site"] = web_site
	udn_data["web_site_page"] = web_site_page
	if session_value, ok := udn_data["cookie"].(map[string]interface{})["opsdb_session"]; ok {
		session_sql := fmt.Sprintf("SELECT * FROM web_user_session WHERE web_site_id = %d AND name = '%s'", web_site["id"], SanitizeSQL(session_value.(string)))
		session_rows := Query(db_web, session_sql)
		if len(session_rows) == 1 {
			session := session_rows[0]
			user_id := session["user_id"]

			fmt.Printf("Found User ID: %d  Session: %v\n\n", user_id, session)

			// Load session from json_data
			target_map := make(map[string]interface{})
			if session["data_json"] != nil {
				err := json.Unmarshal([]byte(session["data_json"].(string)), &target_map)
				if err != nil {
					log.Panic(err)
				}
			}

			fmt.Printf("Session Data: %v\n\n", target_map)

			udn_data["session"] = target_map

			// Load the user data too
			user_sql := fmt.Sprintf("SELECT * FROM \"user\" WHERE id = %d", user_id)
			user_rows := Query(db_web, user_sql)
			target_map_user := make(map[string]interface{})
			if len(user_rows) == 1 {
				// Set the user here
				udn_data["user"] = user_rows[0]

				// Load from user data from json_data
				if user_rows[0]["data_json"] != nil {
					err := json.Unmarshal([]byte(user_rows[0]["data_json"].(string)), &target_map_user)
					if err != nil {
						log.Panic(err)
					}
				}
			}
			fmt.Printf("User Data: %v\n\n", target_map_user)

			udn_data["user_data"] = target_map_user
		}
	}


	return udn_data
}

func SetCookies(cookie_map map[string]interface{}, w http.ResponseWriter, r *http.Request) {
	for key, value := range cookie_map {
		//TODO(g):REMOVE: Testing only...
		new_cookie := http.Cookie{}
		new_cookie.Name = key
		new_cookie.Value = fmt.Sprintf("%v", value)
		new_cookie.Path = "/"
		http.SetCookie(w, &new_cookie)

		fmt.Printf("** Setting COOKIE: %s = %s\n", key, value)
	}
}

func dynamicPage_API(db_web *sql.DB, db *sql.DB, web_site map[string]interface{}, web_site_api map[string]interface{}, uri string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")


	// Get UDN starting data values
	request_body := r.Body
	param_map := r.URL.Query()
	header_map := r.Header
	cookie_array := r.Cookies()

	// Get our starting UDN data
	udn_data := GetStartingUdnData(db_web, db, web_site, web_site_api, uri, request_body, param_map, header_map, cookie_array)

	fmt.Printf("Starting UDN Data: %v\n\n", udn_data)

	// Get UDN schema per request
	//TODO(g): Dont do this every request
	udn_schema := PrepareSchemaUDN(db_web)


	// Process the UDN, which updates the pool at udn_data
	if web_site_api["udn_data_json"] != nil {
		ProcessSchemaUDNSet(db_web, udn_schema, web_site_api["udn_data_json"].(string), &udn_data)
	} else {
		fmt.Printf("UDN Execution: API: %s: None\n\n", web_site_api["name"])
	}

	// Set Cookies
	SetCookies(udn_data["set_cookie"].(map[string]interface{}), w, r)


	// Write whatever is in the API result map, as a JSON result
	var buffer bytes.Buffer
	body, _ := json.Marshal(udn_data["set_api_result"])
	buffer.Write(body)

	fmt.Printf("Writing API body: %s\n\n", body)

	// Write out the final page
	w.Write([]byte(buffer.String()))

}

// Take an array of maps, and make a single map, from one of the keys
func MapArrayToMap(map_array []map[string]interface{}, key string) map[string]interface{} {
	result := make(map[string]interface{})

	for _, item := range map_array {
		//fmt.Printf("MapArrayToMap: %s: %s: %v\n", key, item[key].(string), SnippetData(item, 600))
		result[item[key].(string)] = item
	}

	return result
}

func dynamePage_RenderWidgets(db_web *sql.DB, db *sql.DB, web_site map[string]interface{}, web_site_page map[string]interface{}, uri string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	sql := fmt.Sprintf("SELECT * FROM web_site_page_widget WHERE web_site_page_id = %d ORDER BY priority ASC", web_site_page["id"])
	web_site_page_widgets := Query(db_web, sql)

	// Get the base web site widget
	sql = fmt.Sprintf("SELECT * FROM web_site_page_widget WHERE id = %d", web_site_page["base_page_web_site_page_widget_id"])
	base_page_widgets := Query(db_web, sql)

	// If we couldnt find the page, quit (404)
	if len(base_page_widgets) < 1 {
		dynamicPage_404(uri, w, r)
		return
	}

	base_page_widget := base_page_widgets[0]

	// Get the base widget
	sql = fmt.Sprintf("SELECT * FROM web_widget WHERE id = %d", base_page_widget["web_widget_id"])
	base_widgets := Query(db_web, sql)

	base_page_html, err := ioutil.ReadFile(base_widgets[0]["path"].(string))
	if err != nil {
		log.Panic(err)
	}

	// Get UDN starting data values
	request_body := r.Body
	param_map := r.URL.Query()
	header_map := r.Header
	cookie_array := r.Cookies()

	// Get our starting UDN data
	udn_data := GetStartingUdnData(db_web, db, web_site, web_site_page, uri, request_body, param_map, header_map, cookie_array)

	fmt.Printf("Starting UDN Data: %v\n\n", udn_data)

	// Get the base widget
	sql = fmt.Sprintf("SELECT * FROM web_widget")
	all_widgets := Query(db_web, sql)

	// Save all our base web_widgets, so we can access them anytime we want
	udn_data["base_widget"] = MapArrayToMap(all_widgets, "name")

	fmt.Printf("Base Widget: base_list2_header: %v\n\n", udn_data["base_widget"].(map[string]interface{})["base_list2_header"])

	// We need to use this as a variable, so make it accessible to reduce casting
	page_map := udn_data["page"].(map[string]interface{})


	//TODO(g):HARDCODED: Im just forcing /login for now to make bootstrapping faster, it can come from the data source, think about it
	if uri != "/login" {
		if udn_data["user"].(map[string]interface{})["id"] == nil {
			login_page_id := web_site["login_web_site_page_id"].(int64)
			login_page_sql := fmt.Sprintf("SELECT * FROM web_site_page WHERE id = %d", login_page_id)
			login_page_rows := Query(db_web, login_page_sql)
			if len(login_page_rows) >= 1 {
				login_page := login_page_rows[0]

				// Render the Login Page
				//TODO(g): Verify we can only ever recurse once, this is the only time I do this, so far.  Think out whether this is a good idea...
				dynamePage_RenderWidgets(db_web, db, web_site, login_page, "/login", w, r)

				// Return, as the Login page has been rendered, so we abandon rendering the requested page
				return
			}
		}

	}

	// Get UDN schema per request
	//TODO(g): Dont do this every request
	udn_schema := PrepareSchemaUDN(db_web)


	// Loop over the page widgets, and template them
	for _, site_page_widget := range web_site_page_widgets {
		// Skip it if this is the base page, because we
		if site_page_widget["id"] == web_site_page["base_page_web_site_page_widget_id"] {
			continue
		}

		// Put the Site Page Widget into the UDN Data, so we can operate on it
		udn_data["page_widget"] = site_page_widget

		widget_map := make(map[string]interface{})

		// Put the widget map into the UDN Data too
		udn_data["widget_map"] = widget_map

		// web_widget_id rendering widget -- single widget rendering
		var page_widget map[string]interface{}

		// If we have web_widget specified, use it
		if site_page_widget["web_widget_id"] != nil {

			// Get the base widget
			sql = fmt.Sprintf("SELECT * FROM web_widget WHERE id = %d", site_page_widget["web_widget_id"])
			page_widgets := Query(db_web, sql)
			page_widget = page_widgets[0]

			fmt.Printf("Page Widget: %s: %s\n", site_page_widget["name"], page_widget["name"])

			// wigdet_map has all the UDN operations we will be using to embed child-widgets into this widget
			//TODO(g): We need to use the page_map data here too, because we need to template in the sub-widgets.  Think about this after testing it as-is...
			err = json.Unmarshal([]byte(site_page_widget["data_json"].(string)), &widget_map)
			if err != nil {
				log.Panic(err)
			}

			udn_data["web_widget"] = page_widget



			// Processing UDN: which updates the data pool at udn_data
			if site_page_widget["udn_data_json"] != nil {
				ProcessSchemaUDNSet(db_web, udn_schema, site_page_widget["udn_data_json"].(string), &udn_data)
			} else {
				fmt.Printf("UDN Execution: %s: None\n\n", site_page_widget["name"])
			}


			// Process the Widget's Rendering UDN statements (singles)
			for widget_key, widget_value := range widget_map {
				fmt.Printf("\n\nWidget Key: %s:  Value: %v\n\n", widget_key, widget_value)

				// Force the UDN string into a string
				//TODO(g): Not the best way to do this, fix later, doing now for dev speed/simplicity
				widget_udn_string := fmt.Sprintf("%v", widget_value)

				// Process the UDN with our new method.  Only uses Source, as we are getting, but not setting in this phase
				widget_udn_result := ProcessUDN(db, udn_schema, widget_udn_string, "", &udn_data)

				widget_map[widget_key] = fmt.Sprintf("%v", GetResult(widget_udn_result, type_string))

				fmt.Printf("Widget Key Result: %s   Result: %s\n\n", widget_key, SnippetData(widget_map[widget_key], 600))
			}

			//fmt.Printf("Title: %s\n", widget_map.Map["title"])

			item_html, err := ioutil.ReadFile(page_widget["path"].(string))
			if err != nil {
				log.Panic(err)
			}

			//TODO(g): Replace reading from the "path" above with the "html" stored in the DB, so it can be edited and displayed live
			//item_html := page_widget.Map["html"].(string)

			fmt.Printf("Page Widget: %s   HTML: %s\n", page_widget["name"], SnippetData(page_widget["html"], 600))

			item_template := template.Must(template.New("text").Parse(string(item_html)))

			widget_map_template := NewTextTemplateMap()
			widget_map_template.Map = widget_map

			fmt.Printf("  Templating with data: %v\n\n", SnippetData(widget_map, 600))

			item := StringFile{}
			err = item_template.Execute(&item, widget_map_template)
			if err != nil {
				log.Fatal(err)
			}

			// Append to our total forum_list_string
			key := site_page_widget["name"]

			fmt.Printf("====== Finalized Template: %s\n%s\n\n", key, item.String)

			//fmt.Printf("=-=-=-=-= UDN Data: Output:\n%v\n\n", udn_data["output"])

			page_map[key.(string)] = item.String

		} else if site_page_widget["web_widget_instance_id"] != nil {
			// Get the web_widget_instance data
			sql = fmt.Sprintf("SELECT * FROM web_widget_instance WHERE id = %d", site_page_widget["web_widget_instance_id"])
			web_widget_instance := Query(db_web, sql)[0]

			fmt.Printf("Web Widget Instance: %s\n", web_widget_instance["name"])

			// We are rendering a Web Widget Instance here instead, load the data necessary for the Processing UDN
			// Data for the widget instance goes here (Inputs: data, columns, rows, etc.  These are set from the Processing UDN
			udn_data["widget_instance"] = make(map[string]interface{})
			// Widgets go here (ex: base, row, row_column, header).  We set this here, below.
			udn_data["widget"] = make(map[string]interface{})

			// Set web_widget_instance output location (where the Instance's UDN will string append the output)
			udn_data["widget_instance"].(map[string]interface{})["output_location"] = site_page_widget["web_widget_instance_output"]


			// Get all the web widgets, by their web_widget_instance_widget.name
			sql = fmt.Sprintf("SELECT * FROM web_widget_instance_widget WHERE web_widget_instance_id = %d", site_page_widget["web_widget_instance_id"])
			web_instance_widgets := Query(db_web, sql)
			for _, widget := range web_instance_widgets {
				sql = fmt.Sprintf("SELECT * FROM web_widget WHERE id = %d", widget["web_widget_id"])
				web_widget := Query(db_web, sql)[0]

				udn_data["widget"].(map[string]interface{})[widget["name"]] = web_widget
			}

			// Processing UDN: which updates the data pool at udn_data
			if site_page_widget["udn_data_json"] != nil {
				ProcessSchemaUDNSet(db_web, udn_schema, site_page_widget["udn_data_json"].(string), &udn_data)
			} else {
				fmt.Printf("UDN Execution: %s: None\n\n", site_page_widget["name"])
			}

			// We have prepared the data, we can now execute the Widget Instance UDN, which will string append the output to udn_data["widget_instance"]["output_location"] when done
			if web_widget_instance["udn_data_json"] != nil {
				ProcessSchemaUDNSet(db_web, udn_schema, web_widget_instance["udn_data_json"].(string), &udn_data)
			} else {
				fmt.Printf("Widget Instance UDN Execution: %s: None\n\n", site_page_widget["name"])
			}

		} else {
			panic("No web_widget_id or web_widget_instance_id.  Site Page Widgets need at least one of these.")
		}

	}

	// Get base page widget items.  These were also processed above, as the base_page_widget was included with the page...
	base_page_widget_map := NewTextTemplateMap()
	err = json.Unmarshal([]byte(base_page_widget["data_json"].(string)), &base_page_widget_map.Map)
	if err != nil {
		log.Panic(err)
	}

	// Add base_page_widget entries to page_map, if they dont already exist
	for key, value := range base_page_widget_map.Map {
		if _, ok := page_map[key]; ok {
			// Pass, already has this value
		} else {
			value_str := fmt.Sprintf("%v", value)

			// Process the UDN with our new method.  Only uses Source, as we are getting, but not setting in this phase
			widget_udn_result := ProcessUDN(db, udn_schema, value_str, "", &udn_data)

			if widget_udn_result != nil {
				page_map[key] = fmt.Sprintf("%v", GetResult(widget_udn_result, type_string))
			} else {
				// Use the base page widget, without any processing, because we got back nil
				page_map[key] = value_str
			}

			//// Set the value, static text
			//page_map[key] = value
		}
	}

	fmt.Println("Rendering base page")

	// Put them into the base page
	base_page_template := template.Must(template.New("text").Parse(string(base_page_html)))

	// Set up the TextTemplateMap for page_map, now that it is map[string]interface{}
	page_map_text_template_map := NewTextTemplateMap()
	page_map_text_template_map.Map = page_map

	// Write the base page
	base_page := StringFile{}
	err = base_page_template.Execute(&base_page, page_map_text_template_map)
	if err != nil {
		log.Fatal(err)
	}


	// Set Cookies
	SetCookies(udn_data["set_cookie"].(map[string]interface{}), w, r)


	// Write out the final page
	w.Write([]byte(base_page.String))

}

func GetSelectedDb(db_web *sql.DB, db *sql.DB, db_id int64) *sql.DB {
	// Assume we are using the non-web DB
	selected_db := db

	if db_id == 1 {
		selected_db = db_web
	} else if db_id == 2 {
		selected_db = db
	}

	return selected_db
}

func QueryTTM(db *sql.DB, sql string) []TextTemplateMap {
	// Query
	rs, err := db.Query(sql)
	if err != nil {
		log.Fatal(err)
	}
	defer rs.Close()

	// create a fieldbinding object.
	var fArr []string
	fb := fieldbinding.NewFieldBinding()

	if fArr, err = rs.Columns(); err != nil {
		log.Fatal(err)
	}

	fb.PutFields(fArr)

	// Final output, array of maps
	outArr := []TextTemplateMap{}

	for rs.Next() {
		if err := rs.Scan(fb.GetFieldPtrArr()...); err != nil {
			log.Fatal(err)
		}

		template_map := NewTextTemplateMap()

		for key, value := range fb.GetFieldArr() {
			//fmt.Printf("Found value: %s = %s\n", key, value)

			switch value.(type) {
			case []byte:
				template_map.Map[key] = fmt.Sprintf("%s", value)
			default:
				template_map.Map[key] = value
			}
		}

		outArr = append(outArr, *template_map)
	}

	if err := rs.Err(); err != nil {
		log.Fatal(err)
	}

	return outArr
}

func Query(db *sql.DB, sql string) []map[string]interface{} {
	// Query
	rs, err := db.Query(sql)
	if err != nil {
		log.Fatal(fmt.Sprintf("SQL: %s\nError: %s\n", sql, err))
	}
	defer rs.Close()

	// create a fieldbinding object.
	var fArr []string
	fb := fieldbinding.NewFieldBinding()

	if fArr, err = rs.Columns(); err != nil {
		log.Fatal(fmt.Sprintf("SQL: %s\nError: %s\n", sql, err))
	}

	fb.PutFields(fArr)

	// Final output, array of maps
	outArr := []map[string]interface{}{}

	for rs.Next() {
		if err := rs.Scan(fb.GetFieldPtrArr()...); err != nil {
			log.Fatal(fmt.Sprintf("SQL: %s\nError: %s\n", sql, err))
		}

		template_map := make(map[string]interface{})

		for key, value := range fb.GetFieldArr() {
			//fmt.Printf("Found value: %s = %s\n", key, value)

			switch value.(type) {
			case []byte:
				template_map[key] = fmt.Sprintf("%s", value)
			default:
				template_map[key] = value
			}
		}

		outArr = append(outArr, template_map)
	}

	if err := rs.Err(); err != nil {
		log.Fatal(fmt.Sprintf("SQL: %s\nError: %s\n", sql, err))
	}

	return outArr
}

func SanitizeSQL(text string) string {
	text = strings.Replace(text, "'", "''", -1)

	return text
}

func dynamicPage_404(uri string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	base_html, err := ioutil.ReadFile("web/limitless5/error_404.html")
	if err != nil {
		log.Panic(err)
	}

	w.Write([]byte(base_html))
}

func Lock(lock string) {
	// This must lock things globally.  Global lock server required, only for this Customer though, since "global" can be customer oriented.
	fmt.Printf("Locking: %s\n", lock)

	// Acquire a lock, wait forever until we get it.  Pass in a request UUID so I can see who has the lock.
}

func Unlock(lock string) {
	// This must lock things globally.  Global lock server required, only for this Customer though, since "global" can be customer oriented.
	fmt.Printf("Unlocking: %s\n", lock)

	// Release a lock.  Should we ensure we still had it?  Can do if we gave it our request UUID
}

func ProcessSchemaUDNSet(db *sql.DB, udn_schema map[string]interface{}, udn_data_json string, udn_data *map[string]interface{}) interface{} {
	fmt.Printf("ProcessSchemaUDNSet: JSON:\n%s\n\n", udn_data_json)

	var result interface{}

	if udn_data_json != "" {
		// Extract the JSON into a list of list of lists (2), which gives our execution blocks, and UDN pairs (Source/Target)
		udn_execution_group := UdnExecutionGroup{}

		// Decode the JSON data for the widget data
		err := json.Unmarshal([]byte(udn_data_json), &udn_execution_group.Blocks)
		if err != nil {
			log.Panic(err)
		}

		//fmt.Printf("UDN Execution Group: %v\n\n", udn_execution_group)

		// Process all the UDN Execution blocks
		//TODO(g): Add in concurrency, right now it does it all sequentially
		for _, udn_group := range udn_execution_group.Blocks {
			for _, udn_group_block := range udn_group {
				result = ProcessUDN(db, udn_schema, udn_group_block[0], udn_group_block[1], udn_data)
			}
		}
	} else {
		fmt.Print("UDN Execution Group: None\n\n")
	}

	return result
}

// Prepare UDN processing from schema specification -- Returns all the data structures we need to parse UDN properly
func PrepareSchemaUDN(db *sql.DB) map[string]interface{} {
	// Config
	sql := "SELECT * FROM udn_config ORDER BY name"

	result := Query(db, sql)

	udn_config_map := make(map[string]interface{})

	// Add base_page_widget entries to page_map, if they dont already exist
	for _, value := range result {
		//fmt.Printf("UDN Config: %s = \"%s\"\n", value.Map["name"], value.Map["sigil"])

		// Save the config value and sigil
		//udn_config_map[string(value.Map["name"].(string))] = string(value.Map["sigil"].(string))

		// Create the TextTemplateMap
		udn_config_map[string(value["name"].(string))] = string(value["sigil"].(string))
	}

	//fmt.Printf("udn_config_map: %v\n", udn_config_map)

	// Function
	sql = "SELECT * FROM udn_function ORDER BY name"

	result = Query(db, sql)

	udn_function_map := make(map[string]string)
	udn_function_id_alias_map := make(map[int64]string)
	udn_function_id_function_map := make(map[int64]string)

	// Add base_page_widget entries to page_map, if they dont already exist
	for _, value := range result {
		//fmt.Printf("UDN Function: %s = \"%s\"\n", value.Map["alias"], value.Map["function"])

		// Save the config value and sigil
		udn_function_map[string(value["alias"].(string))] = string(value["function"].(string))
		udn_function_id_alias_map[value["id"].(int64)] = string(value["alias"].(string))
		udn_function_id_function_map[value["id"].(int64)] = string(value["function"].(string))
	}

	//fmt.Printf("udn_function_map: %v\n", udn_function_map)
	//fmt.Printf("udn_function_id_alias_map: %v\n", udn_function_id_alias_map)
	//fmt.Printf("udn_function_id_function_map: %v\n", udn_function_id_function_map)

	// Group
	sql = "SELECT * FROM udn_group ORDER BY name"

	result = Query(db, sql)

	//udn_group_map := make(map[string]*TextTemplateMap)
	udn_group_map := make(map[string]interface{})

	// Add base_page_widget entries to page_map, if they dont already exist
	for _, value := range result {
		//fmt.Printf("UDN Group: %s = Start: \"%s\"  End: \"%s\"  Is Key Value: %v\n", value.Map["name"], value.Map["sigil"])

		udn_group_map[string(value["name"].(string))] = make(map[string]interface{})
	}

	// Load the user functions
	sql = "SELECT * FROM udn_stored_function ORDER BY name"

	result = Query(db, sql)

	//udn_group_map := make(map[string]*TextTemplateMap)
	udn_stored_function := make(map[string]interface{})

	// Add base_page_widget entries to page_map, if they dont already exist
	for _, value := range result {
		udn_stored_function[string(value["name"].(string))] = make(map[string]interface{})
	}


	//fmt.Printf("udn_group_map: %v\n", udn_group_map)

	// Pack a result map for return
	result_map := make(map[string]interface{})

	result_map["function_map"] = udn_function_map
	result_map["function_id_alias_map"] = udn_function_id_alias_map
	result_map["function_id_function_map"] = udn_function_id_function_map
	result_map["group_map"] = udn_group_map
	result_map["config_map"] = udn_config_map
	result_map["stored_function"] = udn_stored_function

	return result_map
}

// Pass in a UDN string to be processed - Takes function map, and UDN schema data and other things as input, as it works stand-alone from the application it supports
func ProcessUDN(db *sql.DB, udn_schema map[string]interface{}, udn_value_source string, udn_value_target string, udn_data *map[string]interface{}) interface{} {
	//fmt.Printf("\n\nProcess UDN: Source:  %s   Target:  %s:   Data:  %v\n\n", udn_value_source, udn_value_target, udn_data)
	fmt.Printf("\n\nProcess UDN: Source:  %s   Target:  %s\n\n", udn_value_source, udn_value_target)

	udn_source := ParseUdnString(db, udn_schema, udn_value_source)
	udn_target := ParseUdnString(db, udn_schema, udn_value_target)

	//fmt.Printf("\n-------DESCRIPTION: SOURCE-------\n\n%s\n", DescribeUdnPart(udn_source))

	fmt.Printf("-------UDN: SOURCE-------\n%s\n", udn_value_source)
	fmt.Printf("-------BEGIN EXECUTION: SOURCE-------\n\n")


	var source_input interface{}

	// Execute the Source UDN
	source_result := ExecuteUdn(db, udn_schema, udn_source, source_input, udn_data)

	fmt.Printf("-------RESULT: SOURCE: %v\n\n", SnippetData(source_result, 300))

	//fmt.Printf("\n-------DESCRIPTION: TARGET-------\n\n%s", DescribeUdnPart(udn_target))

	fmt.Printf("-------UDN: TARGET-------\n%s\n", udn_value_target)
	fmt.Printf("-------BEGIN EXECUTION: TARGET-------\n\n")

	// Execute the Target UDN
	target_result := ExecuteUdn(db, udn_schema, udn_target, source_result, udn_data)

	fmt.Print("\n-------END EXECUTION-------\n\n")

	// If we got something from our target result, return it
	if target_result != nil {
		fmt.Printf("-------RETURNING: TARGET: %v\n\n", SnippetData(target_result, 300))
		return target_result
	} else {
		// Else, return our source result.  It makes more sense to return Target since it ran last, if it exists...
		fmt.Printf("-------RETURNING: SOURCE: %v\n\n", SnippetData(target_result, 300))
		return source_result
	}
}

func SnippetData(data interface{}, size int) string {
	data_str := fmt.Sprintf("%v", data)
	if len(data_str) > size {
		data_str = data_str[0:size] + "..."
	}

	// Get rid of newlines, they make snippets hard to read
	data_str = strings.Replace(data_str,"\n", "", -1)

	data_str = fmt.Sprintf("%s (%T)", data_str, data)

	return data_str
}

func AppendArray(slice []interface{}, data ...interface{}) []interface{} {
	//fmt.Printf("AppendArray: Start: %v\n", slice)
	m := len(slice)
	n := m + len(data)
	if n > cap(slice) { // if necessary, reallocate
		// allocate double what's needed, for future growth.
		newSlice := make([]interface{}, (n+1)*2)
		copy(newSlice, slice)
		slice = newSlice
	}
	slice = slice[0:n]
	copy(slice[m:n], data)
	//fmt.Printf("AppendArray: End: %v (%T)\n", slice, slice[0])
	return slice
}

func ProcessUdnArguments(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, input interface{}, udn_data *map[string]interface{}) []interface{} {
	if udn_start.Children.Len() > 0 {
		fmt.Printf("Processing UDN Arguments: %s [%s]  Starting: Arg Count: %d \n", udn_start.Value, udn_start.Id, udn_start.Children.Len())
	}

	// Argument list
	args := make([]interface{}, 0)

	// Look through the children, adding them to the args, as they are processed.
	//TODO(g): Do the accessors too, but for now, all of them will be functions, so optimizing for that case initially

	for child := udn_start.Children.Front(); child != nil; child = child.Next() {
		arg_udn_start := child.Value.(*UdnPart)

		if arg_udn_start.PartType == part_compound {
			// In a Compound part, the NextUdnPart is the function (currently)
			//TODO(g): This could be anything in the future, but at this point it should always be a function in a compound...  As it's a sub-statement.
			if arg_udn_start.NextUdnPart != nil {
				//fmt.Printf("-=-=-= Args Execute from Compound -=-=-=-\n")
				arg_result := ExecuteUdn(db, udn_schema, arg_udn_start.NextUdnPart, input, udn_data)
				//fmt.Printf("-=-=-= Args Execute from Compound -=-=-=-  RESULT: %T: %v\n", arg_result, arg_result)

				args = AppendArray(args, arg_result)
			} else {
				//fmt.Printf("  UDN Args: Skipping: No NextUdnPart: Children: %d\n\n", arg_udn_start.Children.Len())
				//fmt.Printf("  UDN Args: Skipping: No NextUdnPart: Value: %v\n\n", arg_udn_start.Value)
			}
		} else if arg_udn_start.PartType == part_function {
			//fmt.Printf("-=-=-= Args Execute from Function -=-=-=-\n")
			arg_result := ExecuteUdn(db, udn_schema, arg_udn_start, input, udn_data)

			args = AppendArray(args, arg_result)
		} else if arg_udn_start.PartType == part_map {
			// Take the value as a literal (string, basically, but it can be tested and converted)

			arg_result_result := make(map[string]interface{})

			//fmt.Printf("--Starting Map Arg--\n\n")

			// Then we populate it with data, by processing each of the keys and values
			//TODO(g): Will first assume all keys are strings.  We may want to allow these to be dynamic as well, letting them be set by UDN, but forcing to a string afterwards...
			for child := arg_udn_start.Children.Front(); child != nil; child = child.Next() {
				key := child.Value.(*UdnPart).Value
				//value := child.Value.(*UdnPart).Children.Front().Value.(interface{})
				udn_part_value := child.Value.(*UdnPart).Children.Front().Value.(*UdnPart)

				udn_part_result := ExecuteUdnPart(db, udn_schema, udn_part_value, input, udn_data)

				arg_result_result[key] = udn_part_result.Result
				//fmt.Printf("--  Map:  Key: %s  Value: %v (%T)--\n\n", key, udn_part_result.Result, udn_part_result.Result)
			}
			//fmt.Printf("--Ending Map Arg--\n\n")

			args = AppendArray(args, arg_result_result)
		} else if arg_udn_start.PartType == part_list {
			// Take each list item and process it as UDN, to get the final result for this arg value
			// Populate the list
			list_values := list.New()
			//TODO(g): Convert to an array.  I tried it naively, and it didnt work, so it needs a little more work than just these 2 lines...
			//list_values := make([]interface{}, 0)

			// Then we populate it with data, by processing each of the keys and values
			for child := arg_udn_start.Children.Front(); child != nil; child = child.Next() {
				udn_part_value := child.Value.(*UdnPart)

				//fmt.Printf("List Arg Eval: %v\n", udn_part_value)

				udn_part_result := ExecuteUdnPart(db, udn_schema, udn_part_value, input, udn_data)
				list_values.PushBack(udn_part_result.Result)
				//AppendArray(list_values, udn_part_result.Result)
			}

			//fmt.Printf("  UDN Argument: List: %v\n", SprintList(*list_values))

			args = AppendArray(args, list_values)
		} else {
			args = AppendArray(args, arg_udn_start.Value)
		}
	}

	// Only log if we have something to say, otherwise its just noise
	if len(args) > 0 {
		fmt.Printf("Processing UDN Arguments: %s [%s]  Result: %s\n", udn_start.Value, udn_start.Id, SnippetData(args, 400))
	}
	return args
}

// Execute a single UDN (Soure or Target) and return the result
//NOTE(g): This function does not return UdnPart, because we want to get direct information, so we return interface{}
func ExecuteUdn(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, input interface{}, udn_data *map[string]interface{}) interface{} {
	// Process all our arguments, Executing any functions, at all depths.  Furthest depth first, to meet dependencies

	fmt.Printf("\nExecuteUDN: %s [%s]  Args: %d  Input: %s\n", udn_start.Value, udn_start.Id, udn_start.Children.Len(), SnippetData(input, 40))

	// In case the function is nil, just pass through the input as the result.  Setting it here because we need this declared in function-scope
	var result interface{}

	// If this is a real function (not an end-block nil function)
	if UdnFunctions[udn_start.Value] != nil {
		udn_result := ExecuteUdnPart(db, udn_schema, udn_start, input, udn_data)
		result = udn_result.Result

		// If we have more to process, do it
		if udn_result.NextUdnPart != nil {
			fmt.Printf("ExecuteUdn: Flow Control: JUMPING to NextUdnPart: %s [%s]\n", udn_result.NextUdnPart.Value, udn_result.NextUdnPart.Id)
			// Our result gave us a NextUdnPart, so we can assume they performed some execution flow control themeselves, we will continue where they told us to
			result = ExecuteUdn(db, udn_schema, udn_result.NextUdnPart, result, udn_data)
		} else if udn_start.NextUdnPart != nil {
			fmt.Printf("ExecuteUdn: Flow Control: STEPPING to NextUdnPart: %s [%s]\n", udn_start.NextUdnPart.Value, udn_start.NextUdnPart.Id)
			// We have a NextUdnPart and we didnt recieve a different NextUdnPart from our udn_result, so execute sequentially
			result = ExecuteUdn(db, udn_schema, udn_start.NextUdnPart, result, udn_data)
		}
	} else {
		// Set the result to our input, because we got a nil-function, which doesnt change the result
		result = input
	}

	// If the UDN Result is a list, convert it to an array, as it's easier to read the output
	//TODO(g): Remove all the list.List stuff, so everything is an array.  Better.
	result_type_str := fmt.Sprintf("%T", result)
	if result_type_str == "*list.List" {
		result = GetResult(result, type_array)
	}

	fmt.Printf("ExecuteUDN: End Function: %s [%s]: Result: %s\n\n", udn_start.Value, udn_start.Id, SnippetData(result, 40))

	// Return the result directly (interface{})
	return result
}

// Execute a single UdnPart.  This is necessary, because it may not be a function, it might be a Compound, which has a function inside it.
//		At the top level, this is not necessary, but for flow control, we need to wrap this so that each Block Executor doesnt need to duplicate logic.
//NOTE(g): This function must return a UdnPart, because it is necessary for Flow Control (__iterate, etc)
func ExecuteUdnPart(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, input interface{}, udn_data *map[string]interface{}) UdnResult {
	//fmt.Printf("Executing UDN Part: %s [%s]\n", udn_start.Value, udn_start.Id)

	// Process the arguments
	args := ProcessUdnArguments(db, udn_schema, udn_start, input, udn_data)

	//// The args are in a list, we want them in a slice, and outside the UdnResult wrapper, so we will process them and store them in udn_data["args"] so they are easily available to UDN code
	//arg_slice := make([]interface{}, args.Len())
	//arg_count := 0
	//for item := args.Front(); item != nil; item = item.Next() {
	//	arg_slice[arg_count] = item.Value.(*UdnResult).Result
	//
	//	arg_count++
	//}
	//udn_data["arg"] = arg_slice
	(*udn_data)["arg"] = args


	// What we return, unified return type in UDN
	udn_result := UdnResult{}

	if udn_start.PartType == part_function {
		if UdnFunctions[udn_start.Value] != nil {
			// Execute a function
			fmt.Printf("Executing: %s [%s]   Args: %v\n", udn_start.Value, udn_start.Id, SnippetData(args, 80))

			udn_result = UdnFunctions[udn_start.Value](db, udn_schema, udn_start, args, input, udn_data)
		} else {
			//fmt.Printf("Skipping Execution, nil function, result = input: %s\n", udn_start.Value)
			udn_result.Result = input
		}
	} else if udn_start.PartType == part_compound {
		// Execute the first part of the Compound (should be a function, but it will get worked out)
		udn_result = ExecuteUdnPart(db, udn_schema, udn_start.NextUdnPart, input, udn_data)
	} else {
		// We just store the value, if it is not handled as a special case above
		udn_result.Result = udn_start.Value
	}

	//fmt.Printf("=-=-=-=-= Executing UDN Part: End: %s [%s] Full Result: %v\n\n", udn_start.Value, udn_start.Id, udn_result.Result)	// DEBUG

	return udn_result
}

func UDN_Library_Query(db *sql.DB, sql string) []interface{} {
	// Query
	rs, err := db.Query(sql)
	if err != nil {
		log.Fatal(fmt.Sprintf("SQL: %s\nError: %s\n", sql, err))
	}
	defer rs.Close()

	// create a fieldbinding object.
	var fArr []string
	fb := fieldbinding.NewFieldBinding()

	if fArr, err = rs.Columns(); err != nil {
		log.Fatal(fmt.Sprintf("SQL: %s\nError: %s\n", sql, err))
	}

	fb.PutFields(fArr)

	// Final output, array of maps
	//result_list := list.New()
	result_list := make([]interface{}, 0)

	for rs.Next() {
		if err := rs.Scan(fb.GetFieldPtrArr()...); err != nil {
			log.Fatal(fmt.Sprintf("SQL: %s\nError: %s\n", sql, err))
		}

		template_map := make(map[string]interface{})

		for key, value := range fb.GetFieldArr() {
			//fmt.Printf("Found value: %s = %s\n", key, value)

			switch value.(type) {
			case []byte:
				template_map[key] = fmt.Sprintf("%s", value)
			default:
				template_map[key] = value
			}
		}

		//result_list.PushBack(template_map)
		result_list = AppendArray(result_list, template_map)
	}

	if err := rs.Err(); err != nil {
		log.Fatal(fmt.Sprintf("SQL: %s\nError: %s\n", sql, err))
	}

	return result_list
}

func UDN_QueryById(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	result := UdnResult{}

	fmt.Printf("Query: %v\n", args)

	//arg_0 := args.Front().Value.(*UdnResult)
	arg_0 := args[0]

	// The 2nd arg will be a map[string]interface{}, so ensure it exists, and get it from our args if it was passed in
	arg_1 := make(map[string]interface{})
	if len(args) > 1 {
		//fmt.Printf("Query: %s  Stored Query: %s  Data Args: %v\n", udn_start.Value, arg_0, args[1])

		//TODO(g):VALIDATE: Validation and error handling
		arg_1 = GetResult(args[1], type_map).(map[string]interface{})
	}

	fmt.Printf("Query: %s  Stored Query: %s  Data Args: %v\n", udn_start.Value, arg_0, arg_1)



	query_sql := fmt.Sprintf("SELECT * FROM datasource_query WHERE id = %s", arg_0)


	//TODO(g): Make a new function that returns a list of UdnResult with map.string

	// This returns an array of TextTemplateMap, original method, for templating data
	query_result := Query(db, query_sql)

	sql_parameters := make(map[string]string)
	has_params := false
	if query_result[0]["parameter_json_data"] != nil {
		//fmt.Printf("-- Has params: %v\n", query_result[0]["parameter_data_json"])
		err := json.Unmarshal([]byte(query_result[0]["parameter_json_data"].(string)), &sql_parameters)
		if err != nil {
			log.Panic(err)
		}
		has_params = true
	} else {
		fmt.Printf("-- No params\n")
	}

	result_sql := fmt.Sprintf(query_result[0]["sql"].(string))

	fmt.Printf("Query: SQL: %s   Params: %v\n", result_sql, sql_parameters)

	// If we have params, then format the string for each of them, from our arg map data
	if has_params {
		for param_key, _ := range sql_parameters {
			replace_str := fmt.Sprintf("{{%s}}", param_key)
			//value_str := fmt.Sprintf("%s", param_value)

			// Get the value from the arg_1
			value_str := fmt.Sprintf("%s", arg_1[param_key])

			//fmt.Printf("REPLACE PARAM:  Query: SQL: %s   Replace: %s   Value: %s\n", result_sql, replace_str, value_str)

			result_sql = strings.Replace(result_sql, replace_str, value_str, -1)

			//fmt.Printf("POST-REPLACE PARAM:  Query: SQL: %s   Replace: %s   Value: %s\n", result_sql, replace_str, value_str)
		}

		fmt.Printf("Query: Final SQL: %s\n", result_sql)
	}


	// This query returns a list.List of map[string]interface{}, new method for more-raw data
	result.Result = UDN_Library_Query(db, result_sql)

	fmt.Printf("Query: Result [Items: %d]: %s\n", len(result.Result.([]interface{})), SnippetData(GetResult(result, type_string), 60))

	//// DEBUG
	//result_list := result.Result.(*list.List)
	//for item := result_list.Front(); item != nil; item = item.Next() {
	//	real_item := item.Value.(map[string]interface{})
	//	fmt.Printf("Query Result Value: %v\n", real_item)
	//}


	return result
}

func UDN_DebugOutput(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	result := UdnResult{}
	result.Result = input

	type_str := fmt.Sprintf("%T", input)

	if type_str == "*list.List" {
		fmt.Printf("Debug Output: List: %s: %v\n", type_str, SprintList(*input.(*list.List)))

	} else {
		fmt.Printf("Debug Output: %s: %v\n", type_str, input)
	}

	return result
}

func UDN_TestReturn(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("Test Return data: %s\n", args[0])

	result := UdnResult{}
	result.Result = args[0]

	return result
}

func UDN_Widget(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("Widget: %v\n", args[0])

	udn_data_page := (*udn_data)["page"].(map[string]interface{})

	result := UdnResult{}
	//result.Result = udn_data["widget"].Map[arg_0.Result.(string)]
	result.Result = udn_data_page[args[0].(string)]			//TODO(g): We get this from the page map.  Is this is the best naming?  Check it...

	return result
}

func UDN_StringTemplate(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("String Template: %v\n", SnippetData(args, 60))

	// Get the string we are going to template, using our input data (this is a map[string]interface{})
	access_result := UDN_Get(db, udn_schema, udn_start, args, input, udn_data)

	access_str := access_result.Result.(string)

	fmt.Printf("String Template: Template Input: %v\n", input)

	input_template := NewTextTemplateMap()
	input_template.Map = input.(map[string]interface{})

	item_template := template.Must(template.New("text").Parse(access_str))

	item := StringFile{}
	err := item_template.Execute(&item, input_template)
	if err != nil {
		log.Fatal(err)
	}

	result := UdnResult{}
	result.Result = item.String

	return result
}

func UDN_StringTemplateFromValue(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	// If arg_1 is present, use this as the input instead of input
	actual_input := input
	if len(args) >= 2 {
		actual_input = args[1]
	}

	// If this is an array, convert it to a string, so it is a concatenated string, and then can be properly turned into a map.
	if actual_input != nil {
		if strings.HasPrefix(fmt.Sprintf("%T", actual_input), "[]") {
			fmt.Printf("String Template: Converting from array to string: %s\n", SnippetData(actual_input, 60))
			actual_input = GetResult(actual_input, type_string)
		} else {
			fmt.Printf("String Template: Input is not an array: %s\n", SnippetData(actual_input, 60))
		}
	} else {
		fmt.Printf("String Template: Input is nil\n")
	}

	template_str := GetResult(args[0], type_string).(string)

	fmt.Printf("String Template From Value: Template String: %s Template Input: %v\n\n", SnippetData(actual_input, 60), SnippetData(template_str, 60))

	// Use the actual_input, which may be input or arg_1
	input_template := NewTextTemplateMap()
	input_template.Map = GetResult(actual_input, type_map).(map[string]interface{})

	item_template := template.Must(template.New("text").Parse(template_str))

	item := StringFile{}
	err := item_template.Execute(&item, input_template)
	if err != nil {
		log.Fatal(err)
	}

	result := UdnResult{}
	result.Result = item.String

	return result
}

func UDN_StringAppend(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("String Append: %v\n", args)

	// If we only have 1 argument, and it contains dots, we need to break this into a set of args
	if len(args) == 1 && strings.Contains(args[0].(string), ".") {
		args = SimpleDottedStringToArray(args[0].(string))
	}

	// Get the string we are going to append to
	access_str := ""
	access_result := UDN_Get(db, udn_schema, udn_start, args, input, udn_data)
	if access_result.Result != nil {
		access_str = GetResult(access_result.Result, type_string).(string)
	} else {
		access_str = ""
	}

	fmt.Printf("String Append: %v  Current: %s  Append (%T): %s\n\n", args, SnippetData(access_str, 60), input, SnippetData(input, 60))

	// Append
	access_str = fmt.Sprintf("%s%s", access_str, GetResult(input, type_string).(string))

	//fmt.Printf("String Append: %v  Appended:\n%s\n\n", args, access_str)		//DEBUG

	// Save the appended string
	UDN_Set(db, udn_schema, udn_start, args, access_str, udn_data)

	result := UdnResult{}
	result.Result = access_str

	return result
}

// This function takes a string like "some.elements.here", and makes it into a list of ["some", "elements", here"]
func SimpleDottedStringToUdnResultList(arg_str string) list.List {
	args := list.New()

	arg_array := strings.Split(arg_str, ".")

	for _, arg := range arg_array {
		arg_trimmed := strings.Trim(arg, ".")

		udn_result := UdnResult{}
		udn_result.Result = arg_trimmed

		args.PushBack(&udn_result)
	}

	return *args
}
// This function takes a string like "some.elements.here", and makes it into a list of ["some", "elements", here"]
func SimpleDottedStringToArray(arg_str string) []interface{} {
	args := make([]interface{}, 0)

	arg_array := strings.Split(arg_str, ".")

	for _, arg := range arg_array {
		arg_trimmed := strings.Trim(arg, ".")

		//args.PushBack(&udn_result)
		args = AppendArray(args, arg_trimmed)
	}

	return args
}

func UDN_StringClear(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("String Clear: %v\n", args)

	// arg_0 is always a string that needs to be broken up into a list, so that we can pass it as args to Set
	//arg_0 := args.Front().Value.(*UdnResult).Result.(string)
	arg_0 := GetResult(args[0], type_string).(string)

	// Create a list of UdnResults, so we can pass them as args to the Set command
	udn_result_args := SimpleDottedStringToArray(arg_0)

	// Clear
	result := UdnResult{}
	result.Result = ""

	// Save the appended string
	UDN_Set(db, udn_schema, udn_start, udn_result_args, "", udn_data)

	return result
}

func UDN_StringConcat(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Print("String Concat:\n")

	output := ""

	// Loop over the items in the input
	//for item := input.Result.(*list.List).Front(); item != nil; item = item.Next() {
	for _, item := range input.([]interface{}) {
		output += fmt.Sprintf("%v", item)
	}

	// Input is a pass-through
	result := UdnResult{}
	result.Result = output

	return result
}

func UDN_Input(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	// If we have no arguments, return our input as the result.  This is used for passing our input into a function argument
	if len(args) == 0 {
		result := UdnResult{}
		result.Result = input
		return result
	}

	fmt.Printf("Input: %v\n", args[0])

	result := UdnResult{}
	result.Result = args[0]

	return result
}

// We take an input element, and a count, and will walk the list elements, until the count is complete
func ConvertListToMap(input *list.Element, count int) map[string]interface{} {
	result := make(map[string]interface{})

	index := 0
	for count >= 0 {
		index_str := fmt.Sprintf("%d", index)

		if input != nil {
			result[index_str] = input.Value

			// Go to the next input
			input = input.Next()
		} else {
			fmt.Printf("ConvertListToMap: %d: Input is nil\n", index)
			result[index_str] = nil
		}


		count--
		index++
	}

	return result
}

func SprintMap(map_data map[string]interface{}) string {
	output := ""

	for key, value := range map_data {
		output += fmt.Sprintf("'%s' -> %v\n", key, SnippetData(value, 40))
	}

	return output
}

func UDN_StoredFunction(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("Stored Function: %s\n", SnippetData(args, 80))

	function_name := GetResult(args[0], type_string).(string)

	function_domain_id := (*udn_data)["web_site"].(map[string]interface{})["udn_stored_function_domain_id"]

	sql := fmt.Sprintf("SELECT * FROM udn_stored_function WHERE name = '%s' AND udn_stored_function_domain_id = %d", function_name, function_domain_id)

	function_rows := Query(db, sql)

	// Get all our args, after the first one (which is our function_name)
	(*udn_data)["function_arg"] = GetResult(args[1:], type_map)

	//fmt.Printf("Stored Function: Args: %d: %s\n", len((*udn_data)["function_arg"].(map[string]interface{})), SprintMap((*udn_data)["function_arg"].(map[string]interface{})))

	// Our result, whether we populate it or not
	result := UdnResult{}

	if len(function_rows) > 0 {
		result.Result = ProcessSchemaUDNSet(db, udn_schema, function_rows[0]["udn_data_json"].(string), udn_data)
	}

	return result
}

func UDN_Execute(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	// Assume the input is passed through the execution string
	udn_source := "__input"

	// Assumed the execution string will be a Target UDN string
	arg_0 := args[0]
	udn_target := GetResult(arg_0, type_string).(string)


	fmt.Printf("Execute UDN String As Target: %s\n", udn_target)

	// Execute the Target against the input
	result := UdnResult{}
	result.Result = ProcessUDN(db, udn_schema, udn_source, udn_target, udn_data)

	return result
}

func UDN_Test(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("Test Function!!!\n")

	result := UdnResult{}
	result.Result = "Testing.  123."

	return result
}

func UDN_TestDifferent(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("Different Test Function!!!\n")

	result := UdnResult{}
	result.Result = "Testing.  Differently."

	return result
}

func UDN_Access(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("TBD: UDN Access - navigate through hierarchical data...\n")

	result := UdnResult{}
	result.Result = input

	return result
}

func SprintList(items list.List) string {
	output := ""

	for item := items.Front(); item != nil; item = item.Next() {
		item_str := fmt.Sprintf("'%v'", item.Value)

		if output != "" {
			output += " -> "
		}

		output += item_str
	}

	return output
}

func SprintUdnResultList(items list.List) string {
	output := ""

	for item := items.Front(); item != nil; item = item.Next() {
		item_str := GetResult(item.Value.(*UdnResult), type_string).(string)

		if output != "" {
			output += " -> "
		}

		output += item_str
	}

	return output
}

func GetUdnResultValue(udn_result *UdnResult) interface{} {
	result := udn_result.Result

	// Recurse if this is a UdnResult as well, since they can be packed inside each other, this function opens the box and gets the real answer
	if fmt.Sprintf("%T", result) == "*main.UdnResult" {
		result = GetUdnResultValue(result.(*UdnResult))
	}

	return result
}

func GetUdnResultString(udn_result *UdnResult) string {
	result := GetUdnResultValue(udn_result)

	result_str := fmt.Sprintf("%v", result)

	return result_str
}

func UDN_Get(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("Get: %v\n", SnippetData(args, 80))

	// This is what we will use to Set the data into the last map[string]
	//last_argument := args.Back().Value.(string)
	//last_argument := args.Back().Value.(*UdnResult).Result.(string)
	last_argument := GetResult(args[len(args)-1], type_string).(string)

	// Start at the top of udn_data, and work down
	cur_udn_data := udn_data

	// Go to the last element, so that we can set it with the last arg
	for count := 0; count < len(args) - 1; count++ {
		arg := GetResult(args[count], type_string).(string)

		//fmt.Printf("Get: Cur UDN Data: Before change: %s: %v\n\n", arg, SnippetData(cur_udn_data, 300))

		// Go down the depth of maps
		//TODO(g): If this is an integer, it might be a list/array, but lets assume nothing but map[string] for now...
		cur_udn_data_result := (*cur_udn_data)[arg].(map[string]interface{})
		cur_udn_data = &cur_udn_data_result
	}

	//fmt.Printf("Get: Last Arg data: %s: %s\n\n", last_argument, SnippetData(cur_udn_data, 800))

	// Our result will be a list, of the result of each of our iterations, with a UdnResult per element, so that we can Transform data, as a pipeline
	result := UdnResult{}
	result.Result = (*cur_udn_data)[last_argument]

	//fmt.Printf("Get: %v   Result: %v\n", SnippetData(args, 80), SnippetData(result.Result, 80))

	return result
}

func PrettyPrint(data interface{}) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		panic(err)
	}

	return string(b)
}

func UDN_Set(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("Set: %v   Input: %s\n", SnippetData(args, 80), SnippetData(input, 40))

	// This is what we will use to Set the data into the last map[string]
	//last_argument := args.Back().Value.(*UdnResult).Result.(string)
	last_argument := GetResult(args[len(args)-1], type_string).(string)

	// Start at the top of udn_data, and work down
	cur_udn_data := udn_data

	// Go to the last element, so that we can set it with the last arg
	for count := 0; count < len(args) - 1; count++ {
		arg := GetResult(args[count], type_string).(string)

		// If we dont have this key, create a map[string]interface{} to allow it to be created easily
		if _, ok := (*cur_udn_data)[arg]; !ok {
			(*cur_udn_data)[arg] = make(map[string]interface{})
		}

		// Go down the depth of maps
		//TODO(g): If this is an integer, it might be a list/array, but lets assume nothing but map[string] for now...
		cur_udn_data_result := (*cur_udn_data)[arg].(map[string]interface{})
		cur_udn_data = &cur_udn_data_result
	}

	// Set the last element
	(*cur_udn_data)[last_argument] = input

	//fmt.Printf("Set: %s  To: %s\nResult:\n%s\n\n", last_argument, SnippetData(input, 40), PrettyPrint(udn_data))
	//UDN_Get(db, udn_schema, udn_start, args, input, udn_data)	//TODO:REMOVE:DEBUG: Checking it out using the same udn_data, for sure, because we havent left this function....

	// Input is a pass-through
	result := UdnResult{}
	result.Result = input

	return result
}

func UDN_Iterate(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	// Will loop over all UdnParts until it finds __end_iterate.  It expects input to hold a list.List, which use to iterate and execute the UdnPart blocks
	// It will set a variable that will be accessable by the "__get.current.ARG0"
	// Will return a list.List of each of the loops, which allows for filtering the iteration

	// This is our final input list, as an array, it always works and gets input to pass into the first function
	input_array := GetResult(input, type_array).([]interface{})


	fmt.Printf("Iterate: [%s]  Input: %v\n\n", udn_start.Id, input_array)

	// Our result will be a list, of the result of each of our iterations, with a UdnResult per element, so that we can Transform data, as a pipeline
	result := UdnResult{}
	result_list := make([]interface{}, 0)

	// Loop over the items in the input
	for _, item := range input_array {
		fmt.Printf("\n====== Iterate Loop Start: [%s]  Input: %v\n\n", udn_start.Id, SnippetData(item, 80))

		// Get the input
		current_input := item

		// Variables for looping over functions (flow control)
		udn_current := udn_start

		// Loop over the UdnParts, executing them against the input, allowing it to transform each time
		//TODO(g): Walk our NextUdnPart until we find our __end_if, then stop, so we can skip everything for now, initial flow control
		for udn_current != nil && udn_current.Id != udn_start.BlockEnd.Id && udn_current.NextUdnPart != nil {
			udn_current = udn_current.NextUdnPart

			//fmt.Printf("  Walking ITERATE block [%s]: Current: %s   Current Input: %v\n", udn_start.Id, udn_current.Value, SnippetData(current_input, 60))

			// Execute this, because it's part of the __if block, and set it back into the input for the next function to take
			current_input_result := ExecuteUdnPart(db, udn_schema, udn_current, current_input, udn_data)
			current_input = current_input_result.Result

			// If we are being told to skip to another NextUdnPart, we need to do this, to respect the Flow Control
			if current_input_result.NextUdnPart != nil {
				// Move the current to the specified NextUdnPart
				//NOTE(g): This works because this NextUdnPart will be "__end_iterate", or something like that, so the next for loop test works
				udn_current = current_input_result.NextUdnPart
			}
		}

		// Take the final input (the result of all the execution), and put it into the list.List we return, which is now a transformation of the input list
		result_list = AppendArray(result_list, current_input)

		// Fix the execution stack by setting the udn_current to the udn_current, which is __end_iterate, which means this block will not be executed when UDN_Iterate completes
		result.NextUdnPart = udn_current
	}

	// Send them passed the __end_iterate, to the next one, or nil
	if result.NextUdnPart == nil {
		fmt.Printf("\n====== Iterate Finished: [%s]  NextUdnPart: %v\n\n", udn_start.Id, result.NextUdnPart)
	} else if result.NextUdnPart.NextUdnPart != nil {
		fmt.Printf("\n====== Iterate Finished: [%s]  NextUdnPart: %v\n\n", udn_start.Id, result.NextUdnPart)
	} else {
		fmt.Printf("\n====== Iterate Finished: [%s]  NextUdnPart: End of UDN Parts\n\n", udn_start.Id)
	}

	// Store the result list
	result.Result = result_list

	// Return the
	return result
}

func UDN_IfCondition(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	arg_0 := args[0]

	fmt.Printf("If Condition: %s\n", arg_0)

	// If this is true, all other blocks (else-if, else) will be skipped.  It doesnt matter which block this is, an IF/ELSE-IF/ELSE chain only executes 1 block
	executed_a_block := false
	// Track when we leave the "then" (first) block
	outside_of_then_block := false
	// Used to control when we skip a block
	skip_this_block := false

	// Evaluate whether we will execute the IF-THEN (first) block.  (We dont use a THEN, but thats the saying)
	execute_then_block := true
	if arg_0 != "1" {
		execute_then_block = false
	} else {
		// We will execute the "then" block, so we mark this now, so we skip any ELSE-IF/ELSE blocks
		executed_a_block = true
	}

	// Variables for looping over functions (flow control)
	udn_current := udn_start

	//current_result := UdnResult{}
	//current_result.Result = input
	current_input := input

	// Check the first argument, to see if we should execute the IF-THEN statements, if it is false, we will look for ELSE-IF or ELSE if no ELSE-IF blocks are true.

	//TODO(g): Walk our NextUdnPart until we find our __end_if, then stop, so we can skip everything for now, initial flow control
	for udn_current != nil && udn_current.Value != "__end_if" && udn_current.NextUdnPart != nil {
		udn_current = udn_current.NextUdnPart

		fmt.Printf("Walking IF block: Current: %s   Current Input: %v\n", udn_current.Value, current_input)

		if udn_current.Value == "__else" || udn_current.Value == "__else_if" {
			outside_of_then_block = true
			// Reset this every time we get a new control block start (__else/__else_if), because we havent tested it to be skipped yet
			skip_this_block = false

			if executed_a_block {
				// If we have already executed a block before, then it's time to skip the remaining blocks/parts
				fmt.Printf("Found non-main-if block, skipping: %s\n", udn_current.Value)
				break
			} else {
				// Else, we havent executed a block, so we need to determine if we should start executing.  This is only variable for "__else_if", "else" will always execute if we get here
				if udn_current.Value == "__else_if" {
					udn_current_arg_0 := udn_current.Children.Front().Value.(*UdnPart)
					// If we dont have a "true" value, then skip this next block
					if udn_current_arg_0.Value != "1" {
						skip_this_block = true
					} else {
						fmt.Printf("Executing Else-If Block: %s\n", udn_current_arg_0.Value)
						// Mark block execution, so we wont do any more
						executed_a_block = true
					}
				} else {
					// This is an "__else", and we made it here, so we are executing the else.  Leaving this here to demonstrate that
					fmt.Printf("Executing Else Block\n")
					// Mark block execution, so we wont do any more.  This shouldnt be needed as there should only be one final ELSE, but in case there are more, we will skip them all further ELSE-IF/ELSE blocks
					executed_a_block = true
				}
			}
		} else {
			// Either we are outside the THEN block (because we would skip if not correct), or we want to execute the THEN block
			if outside_of_then_block || execute_then_block {
				if !skip_this_block {
					// Execute this, because it's part of the __if block
					current_result := ExecuteUdnPart(db, udn_schema, udn_current, current_input, udn_data)
					current_input = current_result.Result
				}
			}
		}
	}

	// Skip to the end of the __if block (__end_if)
	for udn_current != nil && udn_current.Value != "__end_if" && udn_current.NextUdnPart != nil {
		udn_current = udn_current.NextUdnPart
	}

	final_result := UdnResult{}
	final_result.Result = current_input
	final_result.NextUdnPart = udn_current

	return final_result
}

func UDN_ElseCondition(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("Else Condition\n")

	result := UdnResult{}
	result.Result = input

	return result
}

func UDN_ElseIfCondition(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data *map[string]interface{}) UdnResult {
	fmt.Printf("Else If Condition\n")

	result := UdnResult{}
	result.Result = input

	return result
}

// Parse a UDN string and return a hierarchy under UdnPart
func ParseUdnString(db *sql.DB, udn_schema map[string]interface{}, udn_value_source string) *UdnPart {

	// First Stage
	next_split := _SplitQuotes(db, udn_schema, udn_value_source)

	//fmt.Printf("\nSplit: Quotes: AFTER: %v\n\n", next_split)

	next_split = _SplitCompoundStatements(db, udn_schema, next_split)

	//fmt.Printf("\nSplit: Compound: AFTER: %v\n\n", next_split)

	next_split = _SplitStatementLists(db, udn_schema, next_split)

	//fmt.Printf("\nSplit: List: AFTER: %v\n\n", next_split)

	// Forth Stage
	next_split = _SplitStatementMaps(db, udn_schema, next_split)

	//fmt.Printf("\nSplit: Compound: Map: %v\n\n", next_split)

	// Fifth Stage
	next_split = _SplitStatementMapKeyValues(db, udn_schema, next_split)

	//fmt.Printf("\nSplit: Compound: Map Key Values: %v\n\n", next_split)

	// Put it into a structure now -- UdnPart
	//
	udn_start := CreateUdnPartsFromSplit_Initial(db, udn_schema, next_split)

	//output := DescribeUdnPart(&udn_start)
	//fmt.Printf("\n===== 0 - Description of UDN Part:\n\n%s\n===== 0 - END\n", output)

	// Put it into a structure now -- UdnPart
	//
	FinalParseProcessUdnParts(db, udn_schema, &udn_start)

	//output = DescribeUdnPart(&udn_start)
	//fmt.Printf("\n===== 1 - Description of UDN Part:\n\n%s\n===== 1 - END\n", output)

	return &udn_start
}

// Take the partially created UdnParts, and finalize the parsing, now that it has a hierarchical structure.  Recusive function
func FinalParseProcessUdnParts(db *sql.DB, udn_schema map[string]interface{}, part *UdnPart) {

	//fmt.Printf("\n** Final Parse **:  Type: %d   Value: %s   Children: %d  Next: %v\n", part.PartType, part.Value, part.Children.Len(), part.NextUdnPart)

	// If this is a map component, make a new Children list with our Map Keys
	if part.PartType == part_map {
		new_children := list.New()

		next_child_is_value := false

		for child := part.Children.Front(); child != nil; child = child.Next() {
			cur_child := *child.Value.(*UdnPart)

			// If this child isn't the value of the last Map Key, then we are expecting a new Map Key, possibly a value
			if next_child_is_value == false {
				map_key_split := strings.Split(cur_child.Value, "=")

				map_key_part := NewUdnPart()
				map_key_part.Value = map_key_split[0]
				map_key_part.PartType = part_map_key
				map_key_part.Depth = part.Depth + 1
				map_key_part.ParentUdnPart = part

				// Add to the new Children
				new_children.PushBack(&map_key_part)

				if map_key_split[1] == "" {
					next_child_is_value = true
				} else {
					// Else, we make a new UdnPart from the second half of this split, and add it as a child to a new Map Key
					key_value_part := NewUdnPart()
					key_value_part.PartType = part_item
					key_value_part.Depth = map_key_part.Depth + 1
					key_value_part.ParentUdnPart = &map_key_part
					key_value_part.Value = map_key_split[1]
					map_key_part.Children.PushBack(&key_value_part)
				}
			} else {
				// Get the last Map Key in new_children
				last_map_key := new_children.Back().Value.(*UdnPart)

				// Add this UdnPart to the Map Key's children
				last_map_key.Children.PushBack(&cur_child)

				// Set this back to false, as we processed this already
				next_child_is_value = false
			}

			//new_children.PushBack(&cur_child)
		}

		// Assign the new children list to be our Map's children
		part.Children = new_children
	}

	// If this is a function, remove any children that are for other functions (once other functions start)
	if part.PartType == part_compound {
		//fmt.Printf("  Compound type!\n\n")
	}

	// If this is a function, remove any children that are for other functions (once other functions start)
	if part.PartType == part_function {
		if part.ParentUdnPart != nil && part.ParentUdnPart.PartType == part_compound {
			// This is a function inside a compound, so dont do what we normally do, as we are already OK!
			//fmt.Printf("\nSkipping: Parent is compound: %s\n\n", part.Value)
		} else {
			// Else, this is not a Compound function (Function Argument)
			if part.ParentUdnPart != nil {
				//fmt.Printf("\nMap Function: %s  Parent:  %s (%d)\n\n", part.Value, part.ParentUdnPart.Value, part.ParentUdnPart.PartType)
			} else {
				//fmt.Printf("\nMap Function: %s  Parent:  NONE\n\n", part.Value)
			}

			// Once this is true, start adding new functions and arguments into the NextUdnPart list
			found_new_function := false

			// New functions we will add after removing elements, into the NextUdnPart chain
			new_function_list := list.New()
			remove_children := list.New()

			// Current new function (this one will always be replaced before being used, but nil wouldnt type cast properly)
			cur_udn_function := UdnPart{}

			for child := part.Children.Front(); child != nil; child = child.Next() {
				if strings.HasPrefix(child.Value.(*UdnPart).Value, "__") {
					// All children from now on will be either a new NextUdnPart, or will be args to those functions
					found_new_function = true

					// Create our new function UdnPart here
					new_udn_function := NewUdnPart()
					new_udn_function.Value = child.Value.(*UdnPart).Value
					new_udn_function.Depth = part.Depth
					new_udn_function.PartType = part_function
					new_udn_function.Children = child.Value.(*UdnPart).Children

					new_function_list.PushBack(&new_udn_function)
					remove_children.PushBack(child)

					cur_udn_function = new_udn_function

					//fmt.Printf("Adding to new_function_list: %s\n", new_udn_function.Value)

				} else if child.Value.(*UdnPart).PartType == part_compound {
					//SKIP: If this is a compount function, we dont need to do anything...
					//fmt.Printf("-=-=-= Found Compound!\n -=-=-=-\n")
				} else if found_new_function == true {
					new_udn := NewUdnPart()
					new_udn.Value = child.Value.(*UdnPart).Value
					new_udn.ValueFinal = child.Value.(*UdnPart).ValueFinal
					new_udn.Depth = cur_udn_function.Depth + 1
					new_udn.PartType = child.Value.(*UdnPart).PartType
					new_udn.ParentUdnPart = &cur_udn_function
					new_udn.Children = child.Value.(*UdnPart).Children

					// Else, if we are taking
					cur_udn_function.Children.PushBack(&new_udn)
					remove_children.PushBack(child)

					//fmt.Printf("  Adding new function Argument/Child: %s\n", new_udn.Value)
				}
			}

			// Remove these children from the current part.Children
			for child := remove_children.Front(); child != nil; child = child.Next() {

				//fmt.Printf("Removing: %v\n", child.Value.(*list.Element).Value)

				_ = part.Children.Remove(child.Value.(*list.Element))
				//removed := part.Children.Remove(child.Value.(*list.Element))
				//fmt.Printf("  Removed: %v\n", removed)
			}

			// Find the last UdnPart, that doesnt have a NextUdnPart, so we can add all the functions onto this
			last_udn_part := part
			for last_udn_part.NextUdnPart != nil {
				last_udn_part = last_udn_part.NextUdnPart
				//
				//
				//TODO(g): This is probably where this goes wrong for Compound, because it is assuming it will find this, but this is put of the primary function chain?
				//
				//...
				//
				//fmt.Printf("Moving forward: %s   Next: %v\n", last_udn_part.Value, last_udn_part.NextUdnPart)
			}

			//fmt.Printf("Elements in new_function_list: %d\n", new_function_list.Len())

			// Add all the functions to the NextUdnPart, starting from last_udn_part
			for new_function := new_function_list.Front(); new_function != nil; new_function = new_function.Next() {
				// Get the UdnPart for the next function
				add_udn_function := *new_function.Value.(*UdnPart)

				// Set at the next item, and connect parrent
				last_udn_part.NextUdnPart = &add_udn_function
				add_udn_function.ParentUdnPart = last_udn_part

				//fmt.Printf("Added NextUdnFunction: %s\n", add_udn_function.Value)

				// Update our new last UdnPart, which continues the Next trail
				last_udn_part = &add_udn_function
			}
		}

	}

	// Process all this part's children
	for child := part.Children.Front(); child != nil; child = child.Next() {
		FinalParseProcessUdnParts(db, udn_schema, child.Value.(*UdnPart))
	}

	// Process any next parts (more functions)
	if part.NextUdnPart != nil {
		FinalParseProcessUdnParts(db, udn_schema, part.NextUdnPart)
	}
}

// Returns a function that starts with the value string, which doesnt have a BlockBegin/BlockEnd set yet
func (start_udn_part *UdnPart) FindBeginBlock(value string) *UdnPart {
	cur_udn := start_udn_part

	// Go up parents of parts, until we find a matching value, with no BlockBegin set, return in-place
	done := false
	for done == false {
		// If this is a matching function value, and it isnt already assigned to a Block
		if cur_udn.PartType == part_function && cur_udn.Value == value && cur_udn.BlockBegin == nil {
			return cur_udn
		}

		// If we are out of parents to go up to, we are done
		if cur_udn.ParentUdnPart == nil {
			done = true
		} else {
			// Else, go up to our parent
			cur_udn = cur_udn.ParentUdnPart
		}
	}

	// Failed to find the correct part, returning the first part we were given (which is ignored, because its not the right part)
	return start_udn_part
}

// Returns the new Function, added to the previous function chain
func (udn_parent *UdnPart) AddFunction(value string) *UdnPart {
	//fmt.Printf("UdnPart: Add Function: Parent: %s   Function: %s\n", udn_parent.Value, value)

	new_part := NewUdnPart()
	new_part.ParentUdnPart = udn_parent

	new_part.Depth = udn_parent.Depth

	new_part.PartType = part_function
	new_part.Value = value

	new_part.Id = fmt.Sprintf("%p", &new_part)


	// Because this is a function, it is the NextUdnPart, which is how flow control is performed
	udn_parent.NextUdnPart = &new_part

	// If this is an End Block "__end_" function, mark it and find it's matching Being and mark that
	if strings.HasPrefix(value, "__end_") {
		// We are the end of ourselves
		new_part.BlockEnd = &new_part

		// Walk backwards and find the Begin Block which doesnt have an End Block yet
		start_function_arr := strings.Split(value, "__end_")
		start_function := "__" + start_function_arr[1]
		//fmt.Printf("  Starting function: %v\n", start_function)

		// Find the begin block, if this is the block we were looking for, tag it
		begin_block_part := udn_parent.FindBeginBlock(start_function)
		if begin_block_part.Value == start_function && begin_block_part.BlockBegin == nil {
			// Set the begin block to this new function's BlockBegin
			new_part.BlockBegin = begin_block_part

			// Set the Begin and End on the being block as well, so both parts are tagged
			begin_block_part.BlockBegin = begin_block_part
			begin_block_part.BlockEnd = &new_part
		} else {
			panic(fmt.Sprintf("ERROR: Incorrect grammar.  Missing open function for: %s\n", value))
		}
	}

	return &new_part
}

// Returns the new Child, added to the udn_parent
func (udn_parent *UdnPart) AddChild(part_type int, value string) *UdnPart {
	//fmt.Printf("UdnPart: Add Child: Parent: %s   Child: %s (%d)\n", udn_parent.Value, value, part_type)

	new_part := NewUdnPart()
	new_part.ParentUdnPart = udn_parent

	new_part.Depth = udn_parent.Depth + 1

	new_part.PartType = part_type
	new_part.Value = value

	// Add to current chilidren
	udn_parent.Children.PushBack(&new_part)

	return &new_part
}

// Take partially split text, and start putting it into the structure we need
func CreateUdnPartsFromSplit_Initial(db *sql.DB, udn_schema map[string]interface{}, source_array []string) UdnPart {
	udn_start := NewUdnPart()
	udn_current := &udn_start

	// We start at depth zero, and descend with sub-statements, lists, maps, etc
	udn_current.Depth = 0

	is_open_quote := false

	//fmt.Printf("Create UDN Parts: Initial: %v\n\n", source_array)

	// Traverse into the data, and start storing everything
	for _, cur_item := range source_array {
		//fmt.Printf("  Create UDN Parts: UDN Current: %-20s    Cur Item: %v\n", udn_current.Value, cur_item)

		// If this is a Underscore, make a new piece, unless this is the first one
		if strings.HasPrefix(cur_item, "__") {

			// Split any dots that may be connected to this still (we dont split on them before this), so we do it here and the part_item test, to complete that
			dot_split_array := strings.Split(cur_item, ".")

			// In the beginning, the udn_start (first part) is part_unknown, but we can use that for the first function, so we just set it here, instead of AddFunction()
			if udn_current.PartType == part_unknown {
				// Set the first function value and part
				udn_current.Value = dot_split_array[0]
				udn_current.PartType = part_function
				// Manually set this first one, as it isnt done through AddFunction()
				udn_current.Id = fmt.Sprintf("%p", &udn_current)
				//fmt.Printf("Create UDN: Function Start: %s\n", cur_item)
			} else {
				// Else, this is not the first function, so add it to the current function
				udn_current = udn_current.AddFunction(dot_split_array[0])
			}

			// Add any of the remaining dot_split_array as children
			for dot_count, doc_split_child := range dot_split_array {
				// Skip the 1st element, which is the function name we stored above
				if dot_count >= 1 {
					if doc_split_child != "" {
						if strings.HasPrefix(doc_split_child, "__") {
							udn_current = udn_current.AddFunction(doc_split_child)
						} else {
							udn_current.AddChild(part_item, doc_split_child)
						}
					}
				}
			}

		} else if cur_item == "'" {
			// Enable and disable our quoting, it is simple enough we can just start/stop it.  Lists, maps, and subs cant be done this way.
			if !is_open_quote {
				is_open_quote = true
				//fmt.Printf("Create UDN: Starting Quoted String\n")
			} else if is_open_quote {
				is_open_quote = false
				//fmt.Printf("Create UDN: Closing Quoted String\n")
			}
		} else if is_open_quote {
			udn_current.AddChild(part_string, cur_item)

		} else if cur_item == "(" {
			//fmt.Printf("Create UDN: Starting Compound\n")

			////TODO(g): Is this the correct way to do this?  Im not sure it is...  Why is it different than other children?  Add as a child, then become the current...
			//// Get the last child, which we will become a child of (because we are on argument) -- Else, we are already in our udn_current...
			//if udn_current.Children.Len() > 0 {
			//	last_udn_current := udn_current.Children.Back().Value.(*UdnPart)
			//	// Set the last child to be the current item, and we are good!
			//	udn_current = last_udn_current
			//}

			// Make this compound current, so we continue to add into it, until it closes
			udn_current = udn_current.AddChild(part_compound, cur_item)


		} else if cur_item == ")" {
			//fmt.Printf("Create UDN: Closing Compound\n")

			// Walk backwards until we are done
			done := false
			for done == false {
				if udn_current.ParentUdnPart == nil {
					// If we have no more parents, we are done because there is nothing left to come back from
					//TODO(g): This could be invalid grammar, need to test for that (extra closing sigils)
					done = true
					//fmt.Printf("COMPOUND: No more parents, finished\n")
				} else if udn_current.PartType == part_compound {
					// Else, if we are already currently on the map, just move off once
					udn_current = udn_current.ParentUdnPart

					done = true
					//fmt.Printf("COMPOUND: Moved out of the Compound\n")
				} else {
					//fmt.Printf("COMPOUND: Updating UdnPart to part: %v --> %v\n", udn_current, *udn_current.ParentUdnPart)
					udn_current = udn_current.ParentUdnPart
					//fmt.Printf("  Walking Up the Compound:  Depth: %d\n", udn_current.Depth)
				}

			}
		} else if cur_item == "[" {
			// Make this list current, so we continue to add into it, until it closes
			udn_current = udn_current.AddChild(part_list, cur_item)

		} else if cur_item == "]" {
			//fmt.Printf("Create UDN: Closing List\n")

			// Walk backwards until we are done
			done := false
			for done == false {
				if udn_current.ParentUdnPart == nil {
					// If we have no more parents, we are done because there is nothing left to come back from
					//TODO(g): This could be invalid grammar, need to test for that (extra closing sigils)
					done = true
					//fmt.Printf("LIST: No more parents, finished\n")
				} else if udn_current.PartType == part_list {
					// Else, if we are already currently on the map, just move off once
					udn_current = udn_current.ParentUdnPart

					done = true
					//fmt.Printf("LIST: Moved out of the List\n")
				} else {
					//fmt.Printf("LIST: Updating UdnPart to part: %v --> %v\n", udn_current, *udn_current.ParentUdnPart)
					udn_current = udn_current.ParentUdnPart
					//fmt.Printf("  Walking Up the List:  Depth: %d\n", udn_current.Depth)
				}

			}
		} else if cur_item == "{" {
			// Make this list current, so we continue to add into it, until it closes
			udn_current = udn_current.AddChild(part_map, cur_item)

		} else if cur_item == "}" {
			//fmt.Printf("Create UDN: Closing Map\n")

			// Walk backwards until we are done
			done := false
			for done == false {
				if udn_current.ParentUdnPart == nil {
					// If we have no more parents, we are done because there is nothing left to come back from
					//TODO(g): This could be invalid grammar, need to test for that (extra closing sigils)
					done = true
					fmt.Printf("MAP: No more parents, finished\n")
				} else if udn_current.PartType == part_map {
					// Else, if we are already currently on the map, just move off once
					udn_current = udn_current.ParentUdnPart

					done = true
					//fmt.Printf("MAP: Moved out of the Map\n")
				} else {
					//fmt.Printf("MAP: Updating UdnPart to part: %v --> %v\n", udn_current, *udn_current.ParentUdnPart)
					udn_current = udn_current.ParentUdnPart
					//fmt.Printf("  Walking Up the Map:  Depth: %d\n", udn_current.Depth)
				}
			}
		} else {
			// If this is not a separator we are going to ignore, add it as Children (splitting on commas)
			if cur_item != "" && cur_item != "." {
				children_array := strings.Split(cur_item, ",")

				// Add basic elements as children
				for _, comma_child_item := range children_array {
					dot_children_array := strings.Split(comma_child_item, ".")

					for _, new_child_item := range dot_children_array {
						if strings.TrimSpace(new_child_item) != "" {
							//udn_current.AddChild(part_item, new_child_item)

							if strings.HasPrefix(new_child_item, "__") {
								udn_current = udn_current.AddFunction(new_child_item)
							} else {
								udn_current.AddChild(part_item, new_child_item)
							}

						}
					}
				}
			}
		}
	}

	//fmt.Printf("Finished Create UDN Parts: Initial\n\n")

	return udn_start
}

func _SplitStringAndKeepSeparator(value string, separator string) []string {
	split_array := strings.Split(value, separator)

	final_array := make([]string, (len(split_array)*2)-1)

	for pos, item := range split_array {
		cur_pos := pos * 2

		final_array[cur_pos] = item

		if cur_pos+1 < len(final_array) {
			final_array[cur_pos+1] = separator
		}
	}

	// Fix the stupid trailing empty item, if it exists.  Will just increase with splits.
	if final_array[len(final_array)-1] == "" {
		final_array = final_array[0 : len(final_array)-1]
	}

	//fmt.Printf("Split: %s  Sep: %s  Result: %s\n", value, separator, final_array)

	return final_array
}

func _SplitStringArray(value_array []string, separator string) []string {
	total_count := 0

	work_list := list.New()

	// Split all the string arrays, keep track of the new total, and put them into the work_list
	for _, item := range value_array {
		split_array := _SplitStringAndKeepSeparator(item, separator)

		total_count += len(split_array)

		work_list.PushBack(split_array)
	}

	// Our final array
	final_array := make([]string, total_count)

	// Iterate over the work list, and add them to our final array by index
	append_count := 0

	for item := work_list.Front(); item != nil; item = item.Next() {
		cur_string_array := item.Value.([]string)

		for _, item_string := range cur_string_array {
			final_array[append_count] = item_string

			append_count++
		}
	}

	return final_array
}

// FIRST STAGE: Recursive function, tracked by depth int.  Inserts sequentially into next_processing_udn_list (list[string]), each of the compound nested items, starting with the inner-most first, and then working out, so that all compound statements can be sequentially processed, with the inner-most getting processed before their immediate next-outer layer, which is the proper order
func _SplitQuotes(db *sql.DB, udn_schema map[string]interface{}, udn_value string) []string {
	//fmt.Printf("\nSplit: Quotes: %v\n\n", udn_value)

	split_result := _SplitStringAndKeepSeparator(udn_value, "'")

	return split_result
}

// SECOND STAGE: Recursive function, tracked by depth int.  Inserts sequentially into next_processing_udn_list (list[string]), each of the compound nested items, starting with the inner-most first, and then working out, so that all compound statements can be sequentially processed, with the inner-most getting processed before their immediate next-outer layer, which is the proper order
func _SplitCompoundStatements(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//fmt.Printf("\nSplit: Compound: %v\n\n", source_array)

	// Split Open Compound
	split_result := _SplitStringArray(source_array, "(")

	// Split Close Compound
	split_result = _SplitStringArray(split_result, ")")

	return split_result
}

// THIRD STAGE: Linear function, iterating over the THIRD STAGE's list[string], list values are collected as argument variables for their respective UDN processing sections
func _SplitStatementLists(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//fmt.Printf("\nSplit: Lists: %v\n\n", source_array)

	// Split Open Compound
	split_result := _SplitStringArray(source_array, "[")

	// Split Close Compound
	split_result = _SplitStringArray(split_result, "]")

	return split_result
}

// FOURTH STAGE: Linear function, iterating over the SECOND STAGE's list[string], map values are collected as argument variables for their respective UDN processing sections
func _SplitStatementMaps(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//fmt.Printf("\nSplit: Maps: %v\n\n", source_array)

	// Split Open Compound
	split_result := _SplitStringArray(source_array, "{")

	// Split Close Compound
	split_result = _SplitStringArray(split_result, "}")

	return split_result
}

// FIFTH STAGE: Linear function, iterating over the THIRD STAGE's list[string], list values are collected as argument variables for their respective UDN processing sections
func _SplitStatementMapKeyValues(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//fmt.Printf("\nSplit: Map Key Values: %v\n\n", source_array)

	return source_array
}

// SIXTH STAGE: Linear function, iterating over the FIRST STAGE's list[string] sequence of compound-nested-items.  This populates a new list[string] which now includes the split items at each compound-layer, which means we have a full set of UDN statements that will be processed at the end of this function
func _SplitStatementItems(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//fmt.Printf("\nSplit: Items: %v\n\n", source_array)

	// Split Open Compound
	split_result := _SplitStringArray(source_array, ".")

	return split_result
}

// SEVENTH STAGE: Linear function, iterating over the THIRD STAGE's list[string], list values are collected as argument variables for their respective UDN processing sections
func _DepthTagList(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//fmt.Printf("\nSplit: Lists: %v\n\n", source_array)

	return source_array
}

// Need to pass in all the Widget data as well, so we have it as a pool of data to be accessed from UDN

// Cookies, Headers, URI Params, JSON Body Payload, etc, must be passed in also, so we have access to all of it

// All of this data should be passed in through 'udn_data', which will be the storage system for all of these

/*

Concurrency:

[
	[
		[SourceA1, TargetA1]
		[SourceA2, TargetA2]
	],
	[
		[SourceB1, TargetB1]
	]
]


__query.1.{username=(__get.header.user.username)}  -->  __set_.userstuff.{id=__hash.(__get.header.user.username), other=...}



*/
