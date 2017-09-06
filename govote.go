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
	"sort"
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
	"github.com/jacksontj/dataman/src/storage_node"
	"github.com/jacksontj/dataman/src/storage_node/metadata"
	"github.com/jacksontj/dataman/src/query"
	"github.com/segmentio/ksuid"
	"context"
	"os/user"
	"gopkg.in/ldap.v2"
)

var PgConnect string

var Debug_Udn bool
var Debug_Udn_Api bool

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

type DynamicResult struct {
	// This is the result
	Result interface{}

	Type int

	// Error messages
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

type LdapUser struct {
	IsAuthenticated bool
	Error string

	Username string
	Groups []string

	FirstName string
	FullName string
	Email string

	HomeDir string
	Uid int
}

func LdapLogin(username string, password string) LdapUser {
	// Set up return value, we can return any time
	ldap_user := LdapUser{}
	ldap_user.Username = username

	// Get all LDAP auth from config file...  JSON is fine...

	usr, _ := user.Current()
	homedir := usr.HomeDir

	server_port := ReadPathData(fmt.Sprintf("%s/secure/ldap_connect_port.txt", homedir))	// Should contain contents, no newlines: host.domain.com:389
	server_port = strings.Trim(server_port, " \n")

	fmt.Printf("LDAP: %s\n", server_port)

	l, err := ldap.Dial("tcp", server_port)
	if err != nil {
		ldap_user.IsAuthenticated = false
		ldap_user.Error = err.Error()
		return ldap_user
	}
	defer l.Close()

	fmt.Printf("Dial complete\n")

	ldap_password := ReadPathData(fmt.Sprintf("%s/secure/notcleartextpasswords.txt", homedir))	// Should contain exact password, no newlines.
	ldap_password = strings.Trim(ldap_password, " \n")

	sbr := ldap.SimpleBindRequest{}

	ldap_userconnect := ReadPathData(fmt.Sprintf("%s/secure/ldap_userconnectstring.txt", homedir))	// Should contain connection string, no newlines: "dc=example,dc=com"
	ldap_userconnect = strings.Trim(ldap_userconnect, " \n")

	sbr.Username = ldap_userconnect
	sbr.Password = ldap_password
	_, err = l.SimpleBind(&sbr)
	if err != nil {
		ldap_user.IsAuthenticated = false
		ldap_user.Error = err.Error()
		return ldap_user
	}

	fmt.Printf("Bind complete\n")

	// Get User account

	filter := fmt.Sprintf("(uid=%s)", username)
	fmt.Printf("Filter: %s\n", filter)

	//TODO(g): Get these from JSON or something?  Not sure...  Probably JSON.  This is all ghetto, but it keeps things mostly anonymous and flexible
	attributes := []string{"cn", "gidNumber", "givenName", "homeDirectory", "loginShell", "mail", "sn", "uid", "uidNumber", "userPassword"}

	ldap_usersearch := ReadPathData(fmt.Sprintf("%s/secure/ldap_usersearch.txt", homedir))	// Should contain connection string, no newlines: "dc=example,dc=com"
	ldap_usersearch = strings.Trim(ldap_usersearch, " \n")

	sr := ldap.NewSearchRequest(ldap_usersearch, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, filter, attributes, nil)

	user_result, err := l.Search(sr)
	if err != nil {
		ldap_user.IsAuthenticated = false
		ldap_user.Error = err.Error()
		return ldap_user
	}

	fmt.Printf("User Search complete: %d\n", len(user_result.Entries))

	for count, first := range user_result.Entries {

		//username = first.GetAttributeValue("sn")

		fmt.Printf("User %d: %s\n", count, first.DN)

		// Populate the result
		ldap_user.FirstName = first.GetAttributeValue("givenName")
		ldap_user.Email = first.GetAttributeValue("mail")
		ldap_user.FullName = first.GetAttributeValue("cn")
		ldap_user.Uid, _ = strconv.Atoi(first.GetAttributeValue("uidNumber"))


		for _, attr := range attributes {
			fmt.Printf("    %s == %v\n", attr, first.GetAttributeValue(attr))
		}

	}

	// Get group info for User

	filter = "(cn=*)"
	fmt.Printf("Group Filter: %s\n", filter)

	//TODO(g): Get these from JSON or something?  Not sure...  Probably JSON.  This is all ghetto, but it keeps things mostly anonymous and flexible
	attributes = []string{"cn", "gidNumber", "memberUid"}

	ldap_groupsearch := ReadPathData(fmt.Sprintf("%s/secure/ldap_groupsearch.txt", homedir))	// Should contain connection string, no newlines: "ou=groups,dc=example,dc=com"
	ldap_groupsearch = strings.Trim(ldap_groupsearch, " \n")

	sr = ldap.NewSearchRequest(ldap_groupsearch, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, filter, attributes, nil)

	group_result, err := l.Search(sr)
	if err != nil {
		ldap_user.IsAuthenticated = false
		ldap_user.Error = err.Error()
		return ldap_user
	}

	fmt.Printf("Group Search complete: %d\n", len(group_result.Entries))

	user_groups := make([]string, 0)

	for count, first := range group_result.Entries {

		fmt.Printf("Group %d: %s\n", count, first.DN)

		group := first.GetAttributeValue("cn")
		group_users := first.GetAttributeValues("memberUid")

		for _, group_user := range group_users {
			if group_user == username {
				user_groups = append(user_groups, group)
			}
		}
	}

	fmt.Printf("User: %s  Groups: %v\n", username, user_groups)

	// Testing password
	err = l.Bind(fmt.Sprintf("uid=%s,%s", username, ldap_usersearch), password)
	if err != nil {
		ldap_user.IsAuthenticated = false
		ldap_user.Error = err.Error()
		return ldap_user
	}


	fmt.Printf("Password is correct\n")

	//TODO(g): make a struct and pack this data into it:  LdapUser{}
	ldap_user.IsAuthenticated = true
	ldap_user.Groups = user_groups


	return ldap_user
}

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
				fmt.Printf("\nGetResult: int: ERROR: %v (%T): %s\n\n", input, input, err)
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
			return int(input.(float64))
		case float32:
			return int(input.(float32))
		default:
			fmt.Printf("\nGetResult: int: default: %v (%T)\n\n", input, input)
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
			if input == nil {
				return ""
			}

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

					json_output, _ := json.MarshalIndent(input, "", "  ")
					return string(json_output)
				}
			} else {
				return fmt.Sprintf("%v", input)
			}

			//NOTE(g): Use type_string_force if you want to coerce this into a string, because this destroys too much data I think.  Testing this as 2 things anyways, easy to fold back into 1 if it doesnt work out.
			return input
		}
	case type_string_force:
		switch input.(type) {
		case string:
			return input
		default:
			if input == nil {
				return ""
			}

			return fmt.Sprintf("%v", input)
		}
	case type_map:
		//fmt.Printf("GetResult: Map: %s\n", type_str)

		// If this is already a map, return it
		if type_str == "map[string]interface {}" {
			return input
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
		if type_str == "[]map[string]interface {}" {
			new_array := make([]interface{}, 0)
			for _, item := range input.([]map[string]interface{}) {
				new_array = AppendArray(new_array, item)
			}
			return new_array
		} else if strings.HasPrefix(type_str, "[]") {
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
			if input != nil {
				// Just make a single item array and stick it in
				result := make([]interface{}, 1)
				result[0] = input
				return result
			} else {
				// Empty array
				result := make([]interface{}, 0)
				return result
			}
		}
	}


	return nil
}

// Execution group allows for Blocks to be run concurrently.  A Group has Concurrent Blocks, which has UDN pairs of strings, so 3 levels of arrays for grouping
type UdnExecutionGroup struct {
	Blocks [][][]string
}

type UdnFunc func(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult

var UdnFunctions = map[string]UdnFunc{}

var DatasourceInstance = map[string]*storagenode.DatasourceInstance{}

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
	Debug_Udn_Api = true
	Debug_Udn = false

	UdnFunctions = map[string]UdnFunc{
		"__query":        UDN_QueryById,
		"__debug_output": UDN_DebugOutput,
		"__if":           UDN_IfCondition,
		"__end_if":       nil,
		"__else":         UDN_ElseCondition,
		"__end_else":     nil,
		"__else_if":      UDN_ElseIfCondition,
		"__end_else_if":  nil,
		"__not":          UDN_Not,
		"__iterate":      UDN_Iterate,
		"__end_iterate":  nil,
		"__access":       UDN_Access,
		"__get":          UDN_Get,
		"__set":          UDN_Set,
		"__get_first":          UDN_GetFirst,		// Takes N strings, which are dotted for udn_data accessing.  The first value that isnt nil is returned.  nil is returned if they all are
		"__temp_get":          UDN_GetTemp,			//TODO(g): Test these.  Use them.
		"__temp_set":          UDN_SetTemp,
		"__temp_label":          UDN_GetTempAccessor,		// This takes a string as an arg, like "info", then returns "temp.info".  Later we will make temp data concurrency safe, so when you need accessors as a string, to a temp (like __string_clear), use this
		//"__temp_clear":          UDN_ClearTemp,
		//"__watch": UDN_WatchSyncronization,
		//"___watch_timeout": UDN_WatchTimeout,				//TODO(g): Should this just be an arg to __watch?  I think so...  Like if/else, watch can control the flow...
		//"__end_watch": nil,
		"__test_return":           UDN_TestReturn, // Return some data as a result
		"__test":           UDN_Test,
		"__test_different": UDN_TestDifferent,
		// Migrating from old functions
		//TODO(g): These need to be reviewed, as they are not necessarily the best way to do this, this is just the easiest/fastest way to do this
		"__widget": UDN_Widget,
		// New functions for rendering web pages (finally!)
		//"__template": UDN_StringTemplate,					// Does a __get from the args...
		"__template": UDN_StringTemplateFromValue,					// Does a __get from the args...
		"__template_wrap": UDN_StringTemplateMultiWrap,					// Takes N-2 tuple args, after 0th arg, which is the wrap_key, (also supports a single arg templating, like __template, but not the main purpose).  For each N-Tuple, the new map data gets "value" set by the previous output of the last template, creating a rolling "wrap" function.
		"__template_map": UDN_MapTemplate,		//TODO(g): Like format, for templating.  Takes 3*N args: (key,text,map), any number of times.  Performs template and assigns key into the input map
		"__format": UDN_MapStringFormat,			//TODO(g): Updates a map with keys and string formats.  Uses the map to format the strings.  Takes N args, doing each arg in sequence, for order control
		"__template_short": UDN_StringTemplateFromValueShort,		// Like __template, but uses {{{fieldname}}} instead of {{index .Max "fieldname"}}, using strings.Replace instead of text/template


		//TODO(g): DEPRICATE.  Longer name, same function.
		"__template_string": UDN_StringTemplateFromValue,	// Templates the string passed in as arg_0

		"__string_append": UDN_StringAppend,
		"__string_clear": UDN_StringClear,		// Initialize a string to empty string, so we can append to it again
		"__concat": UDN_StringConcat,
		"__input": UDN_Input,			//TODO(g): This takes any input as the first arg, and then passes it along, so we can type in new input to go down the pipeline...
		"__input_get": UDN_InputGet,			// Gets information from the input, accessing it like __get
		"__function": UDN_StoredFunction,			//TODO(g): This uses the udn_stored_function.name as the first argument, and then uses the current input to pass to the function, returning the final result of the function.		Uses the web_site.udn_stored_function_domain_id to determine the stored function
		"__execute": UDN_Execute,			//TODO(g): Executes ("eval") a UDN string, assumed to be a "Set" type (Target), will use __input as the Source, and the passed in string as the Target UDN

		"__html_encode": UDN_HtmlEncode,		// Encode HTML symbols so they are not taken as literal HTML


		"__array_append": UDN_ArrayAppend,			// Appends the input into the specified target location (args)

		"__array_divide": UDN_ArrayDivide,			//TODO(g): Breaks an array up into a set of arrays, based on a divisor.  Ex: divide=4, a 14 item array will be 4 arrays, of 4/4/4/2 items each.
		"__array_map_remap": UDN_ArrayMapRemap,			//TODO(g): Takes an array of maps, and makes a new array of maps, based on the arg[0] (map) mapping (key_new=key_old)


		"__map_key_delete": UDN_MapKeyDelete,			// Each argument is a key to remove
		"__map_key_set": UDN_MapKeySet,			// Sets N keys, like __format, but with no formatting
		"__map_copy": UDN_MapCopy,			// Make a copy of the current map, in a new map
		"__map_update": UDN_MapUpdate,			// Input map has fields updated with arg0 map

		"__render_data": UDN_RenderDataWidgetInstance,			// Renders a Data Widget Instance:  arg0 = web_data_widget_instance.id, arg1 = widget_instance map update

		"__json_decode": UDN_JsonDecode,			// Decode JSON
		"__json_encode": UDN_JsonEncode,			// Encode JSON

		"__data_get": UDN_DataGet,					// Dataman Get
		"__data_set": UDN_DataSet,					// Dataman Set
		"__data_filter": UDN_DataFilter,			// Dataman Filter

		"__compare_equal": UDN_CompareEqual,		// Compare equality, takes 2 args and compares them.  Returns 1 if true, 0 if false.  For now, avoiding boolean types...
		"__compare_not_equal": UDN_CompareNotEqual,		// Compare equality, takes 2 args and compares them.  Returns 1 if true, 0 if false.  For now, avoiding boolean types...

		"__ddd_render": UDN_DddRender,			// DDD Render.current: the JSON Dialog Form data for this DDD position.  Uses __ddd_get to get the data, and ___ddd_move to change position.

		"__login": UDN_Login,				// Login through LDAP

		//TODO(g): I think I dont need this, as I can pass it to __ddd_render directly
		//"__ddd_move": UDN_DddMove,				// DDD Move position.current.x.y:  Takes X/Y args, attempted to move:  0.1.1 ^ 0.1.0 < 0.1 > 0.1.0 V 0.1.1
		//"__ddd_get": UDN_DddGet,					// DDD Get.current.{}
		//"__ddd_set": UDN_DddSet,					// DDD Set.current.{}
		//"__ddd_delete": UDN_DddDelete,			// DDD Delete.current: Delete the current item (and all it's sub-items).  Append will be used with __ddd_set/move

		//"__increment": UDN_Increment,				// Increment value
		//"__decrement": UDN_Decrement,				// Decrement value
		//"__split": UDN_StringSplit,				// Split a string into an array on a separator string
		//"__join": UDN_StringJoin,					// Join an array into a string on a separator string
		//"__render_page": UDN_RenderPage,			// Render a page, and return it's widgets so they can be dynamically updated

		// New

		//"__array_append": UDN_ArrayAppend,			//TODO(g): Appends a element onto an array.  This can be used to stage static content so its not HUGE on one line too...

		//"__map_update_prefix": UDN_MapUpdatePrefix,			//TODO(g): Merge a the specified map into the input map, with a prefix, so we can do things like push the schema into the row map, giving us access to the field names and such
		//"__map_clear": UDN_MapClear,			//TODO(g): Clears everything in a map "bucket", like: __map_clear.'temp'

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

func InitDataman() {
	config := storagenode.DatasourceInstanceConfig{
		StorageNodeType: "postgres",
		StorageConfig:  map[string]interface{} {
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

	/*
	result := DatamanGet("web_site_page", 2)

	DatamanSet("web_site_page", result)

	filter := map[string]interface{} {"_id": 2}

	_ = DatamanFilter("web_site_page", filter)
	*/
}

func init() {
	PgConnect = ReadPathData("data/opsdb.connect")

	// Initialize UDN
	InitUdn()

	// Initialize Dataman
	InitDataman()
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
	db_web, err := sql.Open("postgres", PgConnect)
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
	sql := fmt.Sprintf("SELECT * FROM web_site WHERE _id = %d", web_site_id)
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


	_ = ProcessSchemaUDNSet(db_web, udn_schema, udn_json_group, udn_data)
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
	db, err := sql.Open("postgres", PgConnect)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// DB Web
	db_web, err := sql.Open("postgres", PgConnect)
	if err != nil {
		log.Fatal(err)
	}
	defer db_web.Close()

	web_site_id := 1

	//TODO(g): Get the web_site_domain from host header
	//web_site_domain_id := 1

	// Get the path to match from the DB
	sql := fmt.Sprintf("SELECT * FROM web_site WHERE _id = %d", web_site_id)
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

func MapListToDict(map_array []map[string]interface{}, key string) map[string]interface{} {
	// Build a map of all our web site page widgets, so we can
	output_map := make(map[string]interface{})

	for _, map_item := range map_array {
		output_map[map_item[key].(string)] = map_item
	}

	return output_map
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
		session_sql := fmt.Sprintf("SELECT * FROM web_user_session WHERE web_site_id = %d AND name = '%s'", web_site["_id"], SanitizeSQL(session_value.(string)))
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

			fmt.Printf( "Session Data: %v\n\n", target_map)

			udn_data["session"] = target_map

			// Load the user data too
			user_sql := fmt.Sprintf("SELECT * FROM \"user\" WHERE _id = %d", user_id)
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

	// Get the UUID for this request
	id := ksuid.New()
	udn_data["uuid"] = id.String()

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

	fmt.Printf("Params: %v\n\n", param_map)

	// Get the base widget
	sql := fmt.Sprintf("SELECT * FROM web_widget")
	all_widgets := Query(db_web, sql)

	// Save all our base web_widgets, so we can access them anytime we want
	udn_data["base_widget"] = MapArrayToMap(all_widgets, "name")

	// Get UDN schema per request
	//TODO(g): Dont do this every request
	udn_schema := PrepareSchemaUDN(db_web)

	// If we are being told to debug, do so
	if param_map["__debug"] != nil {
		udn_schema["udn_debug"] = true
	} else if Debug_Udn_Api == true {
		// API calls are harder to change than web page requests, so made a separate in code var to toggle debugging
		udn_schema["udn_debug"] = true
	}


	// Process the UDN, which updates the pool at udn_data
	if web_site_api["udn_data_json"] != nil {
		ProcessSchemaUDNSet(db_web, udn_schema, web_site_api["udn_data_json"].(string), udn_data)
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

	// Write out our output as HTML
	html_path := UdnDebugWriteHtml(udn_schema)
	fmt.Printf("UDN Debug HTML Log: %s\n", html_path)

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

	sql := fmt.Sprintf("SELECT * FROM web_site_page_widget WHERE web_site_page_id = %d ORDER BY priority ASC", web_site_page["_id"])
	web_site_page_widgets := Query(db_web, sql)

	// Get the base web site widget
	sql = fmt.Sprintf("SELECT * FROM web_site_page_widget WHERE _id = %d", web_site_page["base_page_web_site_page_widget_id"])
	base_page_widgets := Query(db_web, sql)

	// If we couldnt find the page, quit (404)
	if len(base_page_widgets) < 1 {
		fmt.Printf("No base page widgets found, going 404\n")

		dynamicPage_404(uri, w, r)
		return
	}

	base_page_widget := base_page_widgets[0]

	// Get the base widget
	sql = fmt.Sprintf("SELECT * FROM web_widget WHERE _id = %d", base_page_widget["web_widget_id"])
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

	//fmt.Printf("Base Widget: base_list2_header: %v\n\n", udn_data["base_widget"].(map[string]interface{})["base_list2_header"])

	// We need to use this as a variable, so make it accessible to reduce casting
	page_map := udn_data["page"].(map[string]interface{})


	//TODO(g):HARDCODED: Im just forcing /login for now to make bootstrapping faster, it can come from the data source, think about it
	if uri != "/login" {
		if udn_data["user"].(map[string]interface{})["_id"] == nil {
			login_page_id := web_site["login_web_site_page_id"].(int64)
			login_page_sql := fmt.Sprintf("SELECT * FROM web_site_page WHERE _id = %d", login_page_id)
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

	// If we are being told to debug, do so
	if param_map["__debug"] != nil {
		udn_schema["udn_debug"] = true
	}


	// Loop over the page widgets, and template them
	for _, site_page_widget := range web_site_page_widgets {
		// Skip it if this is the base page, because we
		if site_page_widget["_id"] == web_site_page["base_page_web_site_page_widget_id"] {
			continue
		}

		// Put the Site Page Widget into the UDN Data, so we can operate on it
		udn_data["page_widget"] = site_page_widget

		widget_map := make(map[string]interface{})

		// Put the widget map into the UDN Data too
		udn_data["widget_map"] = widget_map

		// web_widget_id rendering widget -- single widget rendering
		var page_widget map[string]interface{}

		// Get any static content associated with this page widget.  Then we dont need to worry about quoting or other stuff
		widget_static := make(map[string]interface{})
		udn_data["widget_static"] = widget_static
		if site_page_widget["static_data_json"] != nil {
			err = json.Unmarshal([]byte(site_page_widget["static_data_json"].(string)), &widget_static)
			if err != nil {
				log.Panic(err)
			}
		}

		// If we have web_widget specified, use it
		if site_page_widget["web_widget_id"] != nil {

			// Get the base widget
			sql = fmt.Sprintf("SELECT * FROM web_widget WHERE _id = %d", site_page_widget["web_widget_id"])
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
				ProcessSchemaUDNSet(db_web, udn_schema, site_page_widget["udn_data_json"].(string), udn_data)
			} else {
				fmt.Printf("UDN Execution: %s: None\n\n", site_page_widget["name"])
			}


			// Process the Widget's Rendering UDN statements (singles)
			for widget_key, widget_value := range widget_map {
				//fmt.Printf("\n\nWidget Key: %s:  Value: %v\n\n", widget_key, widget_value)

				// Force the UDN string into a string
				//TODO(g): Not the best way to do this, fix later, doing now for dev speed/simplicity
				widget_udn_string := fmt.Sprintf("%v", widget_value)

				// Process the UDN with our new method.  Only uses Source, as we are getting, but not setting in this phase
				widget_udn_result := ProcessUDN(db, udn_schema, widget_udn_string, "", udn_data)

				widget_map[widget_key] = fmt.Sprintf("%v", GetResult(widget_udn_result, type_string))

				//fmt.Printf("Widget Key Result: %s   Result: %s\n\n", widget_key, SnippetData(widget_map[widget_key], 600))
			}

			//fmt.Printf("Title: %s\n", widget_map.Map["title"])

			item_html, err := ioutil.ReadFile(page_widget["path"].(string))
			if err != nil {
				log.Panic(err)
			}

			//TODO(g): Replace reading from the "path" above with the "html" stored in the DB, so it can be edited and displayed live
			//item_html := page_widget.Map["html"].(string)

			//fmt.Printf("Page Widget: %s   HTML: %s\n", page_widget["name"], SnippetData(page_widget["html"], 600))

			item_template := template.Must(template.New("text").Parse(string(item_html)))

			widget_map_template := NewTextTemplateMap()
			widget_map_template.Map = widget_map

			//fmt.Printf("  Templating with data: %v\n\n", SnippetData(widget_map, 600))

			item := StringFile{}
			err = item_template.Execute(&item, widget_map_template)
			if err != nil {
				log.Fatal(err)
			}

			// Append to our total forum_list_string
			key := site_page_widget["name"]

			//fmt.Printf("====== Finalized Template: %s\n%s\n\n", key, item.String)

			//fmt.Printf("=-=-=-=-= UDN Data: Output:\n%v\n\n", udn_data["output"])

			page_map[key.(string)] = item.String

		} else if site_page_widget["web_widget_instance_id"] != nil {
			// Render the Widget Instance
			udn_update_map := make(map[string]interface{})
			RenderWidgetInstance(db_web, udn_schema, udn_data, site_page_widget, udn_update_map)

		} else if site_page_widget["web_data_widget_instance_id"] != nil {
			// Render the Widget Instance, from the web_data_widget_instance
			udn_update_map := make(map[string]interface{})
			RenderWidgetInstance(db_web, udn_schema, udn_data, site_page_widget, udn_update_map)

		} else {
			panic("No web_widget_id, web_widget_instance_id, web_data_widget_instance_id.  Site Page Widgets need at least one of these.")
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
			widget_udn_result := ProcessUDN(db, udn_schema, value_str, "", udn_data)

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

	// Write out our output as HTML
	html_path := UdnDebugWriteHtml(udn_schema)
	fmt.Printf("UDN Debug HTML Log: %s\n", html_path)

	// Write out the final page
	w.Write([]byte(base_page.String))

}

func JsonDump(value interface{}) string {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	err := encoder.Encode(value)
	if err != nil {
		panic(err)
	}

	return buffer.String()
}

func RenderWidgetInstance(db_web *sql.DB, udn_schema map[string]interface{}, udn_data map[string]interface{}, site_page_widget map[string]interface{}, udn_update_map map[string]interface{}) {
	// Render a Widget Instance


	// data_static  --  data_instance_static --  Available for default data...



	// We are rendering a Web Widget Instance here instead, load the data necessary for the Processing UDN
	// Data for the widget instance goes here (Inputs: data, columns, rows, etc.  These are set from the Processing UDN
	//udn_data["widget_instance"] = make(map[string]interface{})
	// If we dont have this bucket yet, make it
	if udn_data["widget_instance"] == nil {
		udn_data["widget_instance"] = make(map[string]interface{})
	}

	// Get the UUID for this widget instance
	id := ksuid.New()
	udn_data["widget_instance"].(map[string]interface{})["uuid"] = id.String()


	// Widgets go here (ex: base, row, row_column, header).  We set this here, below.
	udn_data["widget"] = make(map[string]interface{})

	// Set web_widget_instance output location (where the Instance's UDN will string append the output)
	udn_data["widget_instance"].(map[string]interface{})["output_location"] = site_page_widget["web_widget_instance_output"]


	// Use this to abstract between site_page_widget and web_data_widget_instance
	widget_instance := site_page_widget

	if site_page_widget["web_data_widget_instance_id"] != nil {
		// Get the web_data_widget_instance data
		sql := fmt.Sprintf("SELECT * FROM web_data_widget_instance WHERE _id = %d", site_page_widget["web_data_widget_instance_id"])
		web_data_widget_instance := Query(db_web, sql)[0]

		// Set this as the new widget instance data, since it supercedes the site_page_widget
		widget_instance = web_data_widget_instance

		// Save the widget instance ID too, so we can put it in our hidden field for re-rendering
		udn_data["widget_instance"].(map[string]interface{})["_web_data_widget_instance_id"] = web_data_widget_instance["_id"]

		fmt.Printf("Web Data Widget Instance: %s\n", web_data_widget_instance["name"])

		// If we havent overridden this already, then get it
		if udn_update_map["widget_static"] == nil {
			// Get any static content associated with this page widget.  Then we dont need to worry about quoting or other stuff
			widget_static := make(map[string]interface{})
			udn_data["widget_static"] = widget_static
			if web_data_widget_instance["static_data_json"] != nil {
				err := json.Unmarshal([]byte(web_data_widget_instance["static_data_json"].(string)), &widget_static)
				if err != nil {
					log.Panic(err)
				}
			}
		}
	}

	// Get the web_widget_instance data
	sql := fmt.Sprintf("SELECT * FROM web_widget_instance WHERE _id = %d", widget_instance["web_widget_instance_id"])
	web_widget_instance := Query(db_web, sql)[0]

	fmt.Printf("Web Widget Instance: %s\n", web_widget_instance["name"])
	fmt.Printf("Web Widget Instance Data: %s\n", JsonDump(udn_data["widget_instance"]))

	// Get any static content associated with this page widget.  Then we dont need to worry about quoting or other stuff
	widget_static := make(map[string]interface{})
	udn_data["static_instance"] = widget_static
	if web_widget_instance["static_data_json"] != nil {
		err := json.Unmarshal([]byte(web_widget_instance["static_data_json"].(string)), &widget_static)
		if err != nil {
			log.Panic(err)
		}
	}

	fmt.Printf("Web Widget Instance Data Static: %s\n", JsonDump(udn_data["data_static"]))

	// Get all the web widgets, by their web_widget_instance_widget.name
	sql = fmt.Sprintf("SELECT * FROM web_widget_instance_widget WHERE web_widget_instance_id = %d", widget_instance["web_widget_instance_id"])
	web_instance_widgets := Query(db_web, sql)
	for _, widget := range web_instance_widgets {
		sql = fmt.Sprintf("SELECT * FROM web_widget WHERE _id = %d", widget["web_widget_id"])
		web_widgets := Query(db_web, sql)
		web_widget := web_widgets[0]

		udn_data["widget"].(map[string]interface{})[widget["name"].(string)] = web_widget["html"]
	}

	// Processing UDN: which updates the data pool at udn_data
	if widget_instance["udn_data_json"] != nil {
		ProcessSchemaUDNSet(db_web, udn_schema, widget_instance["udn_data_json"].(string), udn_data)
	} else {
		fmt.Printf("UDN Execution: %s: None\n\n", site_page_widget["name"])
	}

	// We have prepared the data, we can now execute the Widget Instance UDN, which will string append the output to udn_data["widget_instance"]["output_location"] when done
	if web_widget_instance["udn_data_json"] != nil {
		ProcessSchemaUDNSet(db_web, udn_schema, web_widget_instance["udn_data_json"].(string), udn_data)
	} else {
		fmt.Printf("Widget Instance UDN Execution: %s: None\n\n", site_page_widget["name"])
	}
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

func Query(db *sql.DB, sql string) []map[string]interface{} {
	fmt.Printf("Query: %s\n", sql)

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

func MapCopy(input map[string]interface{}) map[string]interface{} {
	new_map := make(map[string]interface{})

	for k, v := range input {
		new_map[k] = v
	}

	return new_map
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
		delete(record, "_id")

		fmt.Printf("DatamanSet: Removing _id key\n")
	} else {
		fmt.Printf("DatamanSet: Not Removing _id: %s\n", record["_id"])
	}

	// Duplicate this map, because we are messing with a live map, that we dont expect to change in this function...
	//TODO(g):REMOVE: Once I dont need to manipulate the map in this function anymore...
	record = MapCopy(record)

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
			record_id = GetResult(record["_id"], type_int).(int64)
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
	fmt.Printf("Dataman SET: Record: JSON: %v\n", JsonDump(record))

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

func ProcessSchemaUDNSet(db *sql.DB, udn_schema map[string]interface{}, udn_data_json string, udn_data map[string]interface{}) interface{} {
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
		udn_function_id_alias_map[value["_id"].(int64)] = string(value["alias"].(string))
		udn_function_id_function_map[value["_id"].(int64)] = string(value["function"].(string))
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

	// By default, do not debug this request
	result_map["udn_debug"] = false

	// Debug information, for rendering the debug output
	UdnDebugReset(result_map)

	fmt.Printf("=-=-=-=-= UDN Schema Created =-=-=-=-=\n")

	return result_map
}

// Pass in a UDN string to be processed - Takes function map, and UDN schema data and other things as input, as it works stand-alone from the application it supports
func ProcessUDN(db *sql.DB, udn_schema map[string]interface{}, udn_value_source string, udn_value_target string, udn_data map[string]interface{}) interface{} {
	UdnLog(udn_schema, "\n\nProcess UDN: Source:  %s   Target:  %s\n\n", udn_value_source, udn_value_target)

	udn_source := ParseUdnString(db, udn_schema, udn_value_source)
	udn_target := ParseUdnString(db, udn_schema, udn_value_target)

	//UdnLog(udn_schema, "\n-------DESCRIPTION: SOURCE-------\n\n%s\n", DescribeUdnPart(udn_source))

	UdnDebugIncrementChunk(udn_schema)
	UdnLogHtml(udn_schema, "-------UDN: SOURCE-------\n%s\n", udn_value_source)
	UdnLog(udn_schema, "-------BEGIN EXECUTION: SOURCE-------\n\n")


	var source_input interface{}

	// Execute the Source UDN
	source_result := ExecuteUdn(db, udn_schema, udn_source, source_input, udn_data)

	UdnLog(udn_schema, "-------RESULT: SOURCE: %v\n\n", SnippetData(source_result, 300))

	//UdnLog(udn_schema, "\n-------DESCRIPTION: TARGET-------\n\n%s", DescribeUdnPart(udn_target))

	UdnDebugIncrementChunk(udn_schema)
	UdnLogHtml(udn_schema, "-------UDN: TARGET-------\n%s\n", udn_value_target)
	UdnLog(udn_schema, "-------BEGIN EXECUTION: TARGET-------\n\n")

	// Execute the Target UDN
	target_result := ExecuteUdn(db, udn_schema, udn_target, source_result, udn_data)

	UdnLog(udn_schema, "\n-------END EXECUTION: TARGET-------\n\n")

	// If we got something from our target result, return it
	if target_result != nil {
		UdnLog(udn_schema, "-------RETURNING: TARGET: %v\n\n", SnippetData(target_result, 300))
		return target_result
	} else {
		// Else, return our source result.  It makes more sense to return Target since it ran last, if it exists...
		UdnLog(udn_schema, "-------RETURNING: SOURCE: %v\n\n", SnippetData(target_result, 300))
		return source_result
	}
}

func ProcessSingleUDNTarget(db *sql.DB, udn_schema map[string]interface{}, udn_value_target string, input interface{}, udn_data map[string]interface{}) interface{} {
	UdnLog(udn_schema, "\n\nProcess Single UDN: Target:  %s  Input: %s\n\n", udn_value_target, SnippetData(input, 80))

	udn_target := ParseUdnString(db, udn_schema, udn_value_target)

	target_result := ExecuteUdn(db, udn_schema, udn_target, input, udn_data)

	UdnLog(udn_schema, "-------RETURNING: TARGET: %v\n\n", SnippetData(target_result, 300))
	return target_result
}

func MakeArray(args ...interface{}) []interface{} {
	return args
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

func ProcessUdnArguments(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, input interface{}, udn_data map[string]interface{}) []interface{} {
	if udn_start.Children.Len() > 0 {
		UdnLog(udn_schema, "Processing UDN Arguments: %s [%s]  Starting: Arg Count: %d \n", udn_start.Value, udn_start.Id, udn_start.Children.Len())
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
				//UdnLog(udn_schema, "-=-=-= Args Execute from Compound -=-=-=-\n")
				arg_result := ExecuteUdn(db, udn_schema, arg_udn_start.NextUdnPart, input, udn_data)
				//UdnLog(udn_schema, "-=-=-= Args Execute from Compound -=-=-=-  RESULT: %T: %v\n", arg_result, arg_result)
				//fmt.Printf("Compound Part: %s\n", DescribeUdnPart(arg_udn_start.NextUdnPart))
				//fmt.Printf("Compound Parent: %s\n", DescribeUdnPart(arg_udn_start))

				args = AppendArray(args, arg_result)
			} else {
				//UdnLog(udn_schema, "  UDN Args: Skipping: No NextUdnPart: Children: %d\n\n", arg_udn_start.Children.Len())
				//UdnLog(udn_schema, "  UDN Args: Skipping: No NextUdnPart: Value: %v\n\n", arg_udn_start.Value)
			}
		} else if arg_udn_start.PartType == part_function {
			//UdnLog(udn_schema, "-=-=-= Args Execute from Function -=-=-=-\n")
			arg_result := ExecuteUdn(db, udn_schema, arg_udn_start, input, udn_data)

			args = AppendArray(args, arg_result)
		} else if arg_udn_start.PartType == part_map {
			// Take the value as a literal (string, basically, but it can be tested and converted)

			arg_result_result := make(map[string]interface{})

			//UdnLog(udn_schema, "--Starting Map Arg--\n\n")

			// Then we populate it with data, by processing each of the keys and values
			//TODO(g): Will first assume all keys are strings.  We may want to allow these to be dynamic as well, letting them be set by UDN, but forcing to a string afterwards...
			for child := arg_udn_start.Children.Front(); child != nil; child = child.Next() {
				key := child.Value.(*UdnPart).Value

				//ORIGINAL:
				udn_part_value := child.Value.(*UdnPart).Children.Front().Value.(*UdnPart)
				//udn_part_result := ExecuteUdnPart(db, udn_schema, udn_part_value, input, udn_data)
				udn_part_result := ExecuteUdnCompound(db, udn_schema, udn_part_value, input, udn_data)
				arg_result_result[key] = udn_part_result.Result


				UdnLog(udn_schema, "--  Map:  Key: %s  Value: %v (%T)--\n\n", key, udn_part_result.Result, udn_part_result.Result)
			}
			//UdnLog(udn_schema, "--Ending Map Arg--\n\n")

			args = AppendArray(args, arg_result_result)
		} else if arg_udn_start.PartType == part_list {
			// Take each list item and process it as UDN, to get the final result for this arg value
			// Populate the list
			//list_values := list.New()
			array_values := make([]interface{}, 0)

			//TODO(g): Convert to an array.  I tried it naively, and it didnt work, so it needs a little more work than just these 2 lines...
			//list_values := make([]interface{}, 0)

			// Then we populate it with data, by processing each of the keys and values
			for child := arg_udn_start.Children.Front(); child != nil; child = child.Next() {
				udn_part_value := child.Value.(*UdnPart)

				UdnLog(udn_schema, "List Arg Eval: %v\n", udn_part_value)

				udn_part_result := ExecuteUdnPart(db, udn_schema, udn_part_value, input, udn_data)
				//list_values.PushBack(udn_part_result.Result)
				array_values = AppendArray(array_values, udn_part_result.Result)
			}

			//UdnLog(udn_schema, "  UDN Argument: List: %v\n", SprintList(*list_values))

			//args = AppendArray(args, list_values)
			args = AppendArray(args, array_values)
		} else {
			args = AppendArray(args, arg_udn_start.Value)
		}
	}

	// Only log if we have something to say, otherwise its just noise
	if len(args) > 0 {
		UdnLog(udn_schema, "Processing UDN Arguments: %s [%s]  Result: %s\n", udn_start.Value, udn_start.Id, SnippetData(args, 400))
	}

	return args
}

func UdnDebugWriteHtml(udn_schema map[string]interface{}) string {
	fmt.Printf("\n\n\n\n-=-=-=-=-=- UDN Debug Write HTML -=-=-=-=-=-\n\n\n\n")

	//TODO(g): Make this unique, time in milliseconds should be OK (and sequential), so we can have more than one.  Then deal with cleanup.  And make a sub directory...
	output_path := "/tmp/udn_debug_log.html"

	// Process any remaining HTML chunk as well
	UdnDebugIncrementChunk(udn_schema)

	err := ioutil.WriteFile(output_path, []byte(udn_schema["debug_output_html"].(string)), 0644)
	if err != nil {
		panic(err)
	}

	// Clear the schema info
	//TODO(g): This only works for concurrency at the moment because I get the udn_schema every request, which is wasteful.  So work that out...
	UdnDebugReset(udn_schema)

	return output_path
}

func UdnDebugReset(udn_schema map[string]interface{}) {
	fmt.Printf("\n\n\n\n-=-=-=-=-=- UDN Debug Reset -=-=-=-=-=-\n\n\n\n")

	udn_schema["debug_log"] = ""
	udn_schema["debug_log_count"] = 0
	udn_schema["debug_html_chunk_count"] = 0
	udn_schema["debug_html_chunk"] = ""
	udn_schema["debug_output"] = ""
	udn_schema["debug_output_html"] = `
		<head>
			<script src="https://ajax.googleapis.com/ajax/libs/jquery/3.2.1/jquery.min.js">
			</script>
			<script>
			function ToggleDisplay(element_id) {
				var current_display = $('#'+element_id).css('display');
				if (current_display == 'none') {
					$('#'+element_id).css('display', 'block');
					//alert('Setting ' + element_id + ' to BLOCK == Current: ' + current_display)
				}
				else {
					$('#'+element_id).css('display', 'none');
					//alert('Setting ' + element_id + ' to NONE == Current: ' + current_display)
				}
			}
			</script>
		</head>
		`

}

func UdnDebugIncrementChunk(udn_schema map[string]interface{}) {
	current := udn_schema["debug_html_chunk_count"].(int)
	current++
	udn_schema["debug_html_chunk_count"] = current

	// Update the output with the current Debug Log (and clear it, as it's temporary).  This ensures anything previously undated, gets updated.
	UdnDebugUpdate(udn_schema)

	// Wrap anything we have put into our current HTML chunk, and write it to the HTML Output
	if udn_schema["debug_html_chunk"] != "" {
		// Render our HTML chunk in a hidden DIV, with a button to toggle visibility
		html_output := fmt.Sprintf("<button onclick=\"ToggleDisplay('debug_chunk_%d')\">Statement %d</button><br><br><div id=\"debug_chunk_%d\" style=\"display: none\">%s</div>\n", current, current, current, udn_schema["debug_html_chunk"])

		udn_schema["debug_output_html"] = udn_schema["debug_output_html"].(string) + html_output

		// Clear the chunk
		udn_schema["debug_html_chunk"] = ""
	}
}

func UdnDebug(udn_schema map[string]interface{}, input interface{}, button_label string, message string) {
	if Debug_Udn || udn_schema["udn_debug"] == true {
		// Increment the number of times we have done this, so we have unique debug log sections
		debug_log_count := udn_schema["debug_log_count"].(int)
		debug_log_count++
		udn_schema["debug_log_count"] = debug_log_count

		// Update the output with the current Debug Log (and clear it, as it's temporary)
		UdnDebugUpdate(udn_schema)
		// Render our input, and current UDN Data as well
		html_output := fmt.Sprintf("<pre>%s</pre><button onclick=\"ToggleDisplay('debug_state_%d')\">%s</button><br><br><div id=\"debug_state_%d\" style=\"display: none\">\n", HtmlClean(message), debug_log_count, button_label, debug_log_count)
		udn_schema["debug_html_chunk"] = udn_schema["debug_html_chunk"].(string) + html_output

		// Input
		switch input.(type) {
		case string:
			udn_schema["debug_html_chunk"] = udn_schema["debug_html_chunk"].(string) + "<pre>" + HtmlClean(input.(string)) + "</pre>"
		default:
			input_output, _ := json.MarshalIndent(input, "", "  ")
			//input_output := fmt.Sprintf("%v", input)	// Tried this to increase performance, this is not the bottleneck...
			udn_schema["debug_html_chunk"] = udn_schema["debug_html_chunk"].(string) + "<pre>" + HtmlClean(string(input_output)) + "</pre>"
		}

		// Close the DIV tag
		udn_schema["debug_html_chunk"] = udn_schema["debug_html_chunk"].(string) + "</div>"

	}
}

func UdnDebugUpdate(udn_schema map[string]interface{}) {
	debug_log_count := udn_schema["debug_log_count"].(int)

	// If we have anything in our UDN Debug Log, lets put it into a DIV we can hide, and clear it out, so we collect them in pieces
	if udn_schema["debug_log"] != "" {
		// Append to our raw output
		udn_schema["debug_output"] = udn_schema["debug_output"].(string) + udn_schema["debug_log"].(string)

		// Append to our HTML output
		html_output := fmt.Sprintf("<button onclick=\"ToggleDisplay('debug_log_%d')\">Debug</button><br><pre id=\"debug_log_%d\" style=\"display: none\">%s</pre>\n", debug_log_count, debug_log_count, HtmlClean(udn_schema["debug_log"].(string)))
		udn_schema["debug_html_chunk"] = udn_schema["debug_html_chunk"].(string) + html_output

		// Clear the debug log, as we have put it into the debug_output and debug_output_html
		udn_schema["debug_log"] = ""
	}
}

func HtmlClean(html string) string {
	html = strings.Replace(html, "<", "&lt;", -1)
	html = strings.Replace(html, ">", "&gt;", -1)
	html = strings.Replace(html, "&", "&amp;", -1)
	html = strings.Replace(html, " ", "&nbsp;", -1)

	return html
}

func UdnLog(udn_schema map[string]interface{}, format string, args ...interface{}) {
	// Format the incoming Printf args, and print them
	output := fmt.Sprintf(format, args...)
	if Debug_Udn || udn_schema["udn_debug"] == true {
		fmt.Print(output)
	}

	// Append the output into our udn_schema["debug_log"], where we keep raw logs, before wrapping them up for debugging visibility purposes
	udn_schema["debug_log"] = udn_schema["debug_log"].(string) + output
}

func UdnLogHtml(udn_schema map[string]interface{}, format string, args ...interface{}) {
	// Format the incoming Printf args, and print them
	output := fmt.Sprintf(format, args...)
	fmt.Print(output)

	// Append the output into our udn_schema["debug_log"], where we keep raw logs, before wrapping them up for debugging visibility purposes
	udn_schema["debug_log"] = udn_schema["debug_log"].(string) + output
	// Append to HTML as well, so it shows up.  This is a convenience function for this reason.  Headers and stuff.
	udn_schema["debug_output_html"] = udn_schema["debug_output_html"].(string) + "<pre>" + HtmlClean(output) + "</pre>"
}

// Execute a single UDN (Soure or Target) and return the result
//NOTE(g): This function does not return UdnPart, because we want to get direct information, so we return interface{}
func ExecuteUdn(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, input interface{}, udn_data map[string]interface{}) interface{} {
	// Process all our arguments, Executing any functions, at all depths.  Furthest depth first, to meet dependencies

	UdnLog(udn_schema, "\nExecuteUDN: %s [%s]  Args: %d  Input: %s\n", udn_start.Value, udn_start.Id, udn_start.Children.Len(), SnippetData(input, 40))

	// In case the function is nil, just pass through the input as the result.  Setting it here because we need this declared in function-scope
	var result interface{}

	// If this is a real function (not an end-block nil function)
	if UdnFunctions[udn_start.Value] != nil {
		udn_result := ExecuteUdnPart(db, udn_schema, udn_start, input, udn_data)
		result = udn_result.Result

		// If we have more to process, do it
		if udn_result.NextUdnPart != nil {
			UdnLog(udn_schema, "ExecuteUdn: Flow Control: JUMPING to NextUdnPart: %s [%s]\n", udn_result.NextUdnPart.Value, udn_result.NextUdnPart.Id)
			// Our result gave us a NextUdnPart, so we can assume they performed some execution flow control themeselves, we will continue where they told us to
			result = ExecuteUdn(db, udn_schema, udn_result.NextUdnPart, result, udn_data)
		} else if udn_start.NextUdnPart != nil {
			UdnLog(udn_schema, "ExecuteUdn: Flow Control: STEPPING to NextUdnPart: %s [%s]\n", udn_start.NextUdnPart.Value, udn_start.NextUdnPart.Id)
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

	UdnLog(udn_schema, "ExecuteUDN: End Function: %s [%s]: Result: %s\n\n", udn_start.Value, udn_start.Id, SnippetData(result, 40))

	// Return the result directly (interface{})
	return result
}

// Execute a single UdnPart.  This is necessary, because it may not be a function, it might be a Compound, which has a function inside it.
//		At the top level, this is not necessary, but for flow control, we need to wrap this so that each Block Executor doesnt need to duplicate logic.
//NOTE(g): This function must return a UdnPart, because it is necessary for Flow Control (__iterate, etc)
func ExecuteUdnPart(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, input interface{}, udn_data map[string]interface{}) UdnResult {
	//UdnLog(udn_schema, "Executing UDN Part: %s [%s]\n", udn_start.Value, udn_start.Id)

	// Process the arguments
	args := ProcessUdnArguments(db, udn_schema, udn_start, input, udn_data)

	UdnDebug(udn_schema, input, "View Input", fmt.Sprintf("Execute UDN Part: %s: %v", udn_start.Value, SnippetData(args, 300)))

	// Store this so we can access it if we want
	//TODO(g): Is this required?  Is it the best place for it?  Investiage at some point...  Not sure re-reading it.
	udn_data["arg"] = args


	// What we return, unified return type in UDN
	udn_result := UdnResult{}

	if udn_start.PartType == part_function {
		if UdnFunctions[udn_start.Value] != nil {
			// Execute a function
			UdnLog(udn_schema, "Executing: %s [%s]   Args: %v\n", udn_start.Value, udn_start.Id, SnippetData(args, 80))

			udn_result = UdnFunctions[udn_start.Value](db, udn_schema, udn_start, args, input, udn_data)
		} else {
			//UdnLog(udn_schema, "Skipping Execution, nil function, result = input: %s\n", udn_start.Value)
			udn_result.Result = input
		}
	} else if udn_start.PartType == part_compound {
		// Execute the first part of the Compound (should be a function, but it will get worked out)
		udn_result = ExecuteUdnPart(db, udn_schema, udn_start.NextUdnPart, input, udn_data)
	} else if udn_start.PartType == part_map {
		map_result := make(map[string]interface{})

		for child := udn_start.Children.Front(); child != nil; child = child.Next() {
			cur_child := *child.Value.(*UdnPart)

			key := cur_child.Value
			value := cur_child.Children.Front().Value.(*UdnPart).Value

			map_result[key] = value
		}

		udn_result.Result = map_result

	} else {
		// We just store the value, if it is not handled as a special case above
		udn_result.Result = udn_start.Value
	}

	//UdnLog(udn_schema, "=-=-=-=-= Executing UDN Part: End: %s [%s] Full Result: %v\n\n", udn_start.Value, udn_start.Id, udn_result.Result)	// DEBUG

	UdnDebug(udn_schema, udn_result.Result, "View Output", "")

	return udn_result
}


// Execute a UDN Compound
func ExecuteUdnCompound(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, input interface{}, udn_data map[string]interface{}) UdnResult {
	udn_result := UdnResult{}

	if udn_start.NextUdnPart != nil {
		// If this is a Compound, process it
		udn_current := udn_start.NextUdnPart

		done := false


		for !done {
			//fmt.Printf("Execute UDN Compound: %s\n", DescribeUdnPart(udn_current))
			//fmt.Printf("Execute UDN Compound: Input: %s\n", SnippetData(input, 60))

			udn_result = ExecuteUdnPart(db, udn_schema, udn_current, input, udn_data)
			input = udn_result.Result

			if udn_current.NextUdnPart == nil {
				done = true
				//fmt.Print("  UDN Compound: Finished\n")
			} else {
				udn_current = udn_current.NextUdnPart
				//fmt.Printf("  Next UDN Compound: %s\n", udn_current.Value)
			}
		}
	} else {
		// If we arent a compount, return the value
		udn_result.Result = udn_start.Value
	}

	return udn_result
}

/*

func DddSet(position_location string, data_location string, save_data map[string]interface{}, ddd_id int, udn_data map[string]interface{}) {
	ddd := DatamanGet("ddd", ddd_id)

	// Get our positional info
	position_info := _DddGetPositionInfo(position_location, udn_data)

}

func DddValidate(data_location string, ddd_id int, udn_data map[string]interface{}) []map[string]interface{} {
	ddd := DatamanGet("ddd", ddd_id)

	result := make([]map[string]interface{}, 0)
	return result
}

func DddDelete(position_location string, data_location string, ddd_id int, udn_data map[string]interface{}) {
	ddd := DatamanGet("ddd", ddd_id)

	// Get our positional info
	position_info := _DddGetPositionInfo(position_location, udn_data)

}
*/

/*
func _DddGetPositionInfo(position_location string, udn_data map[string]interface{}) map[string]interface{} {
	current_info := MapGet(MakeArray(position_location), udn_data).(map[string]interface{})

	// If current_info is not set up properly, set it up
	if current_info == nil || current_info["x"] == nil {
		current_info = make(map[string]interface{})

		current_info["location"] = "0"
		current_info["x"] = 0
		current_info["y"] = 0
	}

	return current_info
}
*/

func DddMove(position_location string, move_x int64, move_y int64) string {
	//NOTE(g): This function doesnt check if the new position is valid, that is done by DddGet() which returns the DDD info at the current position (if valid, or nil

	parts := strings.Split(position_location, ".")

	fmt.Printf("DDD Move: Parts: %v\n", parts)

	// Only allow X or Y movement, not both.  This isnt a video game.
	if move_x != 0 {
		if move_x == 1 {
			fmt.Printf("DDD Move: RIGHT\n")
			// Moving to the right, we just add a .0 to the current location
			return fmt.Sprintf("%s.0", position_location)
		} else {
			fmt.Printf("DDD Move: LEFT\n")
			if len(parts) > 1 {
				parts = parts[0:len(parts)-1]
				return strings.Join(parts, ".")
			} else {
				fmt.Printf("DDD Move: Cant move left\n")
				// Else, we only have 1 location part, we cant reduce this, so return the initial location
				return position_location
			}
		}
	} else if move_y != 0 {
		last_part := parts[len(parts)-1]
		last_part_int, _ := strconv.Atoi(last_part)

		if move_y == 1 {
			fmt.Printf("DDD Move: DOWN\n")
			// Moving down, increment the last_part_int
			last_part_int++
			parts[len(parts)-1] = strconv.Itoa(last_part_int)

			return strings.Join(parts, ".")
		} else {
			fmt.Printf("DDD Move: UP\n")
			// Moving up, decrement the last_part_int
			last_part_int--
			//if last_part_int < 0 {
			//	last_part_int = 0
			//}
			parts[len(parts)-1] = strconv.Itoa(last_part_int)

			return strings.Join(parts, ".")
		}
	}

	fmt.Printf("DDD Move: No Change\n")

	// No change in position, return the same string we received
	return position_location
}

func DddGet(position_location string, data_location string, ddd_data map[string]interface{}, udn_data map[string]interface{}) interface{} {
	// Get the DDD Node that describes this position
	//ddd_node := DddGetNode(position_location, ddd_data, udn_data)

	//TODO(g): SECOND!    We know the DDD information, so we navigate the same way we did DDD, but we get the data
	//
	//	What if it isnt available?  We return an error.  How?
	//
	//	??	How		??
	//		???
	//
	// Copy the looping code into all the functions, dont worry about generalizing initially, just get it working.
	//


	result := 1
	return result
}

func MapKeys(data map[string]interface{}) []string {
	// Get the slice of keys
	keys := make([]string, len(data))
	i := 0
	for k := range data {
		keys[i] = k
		i++
	}

	sort.Strings(keys)

	return keys
}

func _DddGetNodeCurrent(cur_data map[string]interface{}, cur_record_data interface{}, cur_pos int, processed_parts []int, cur_parts []string) (string, map[string]interface{}, interface{}) {
	if cur_data["keydict"] != nil {
		// The cur_pos will be selected based on the sorted values, because they are map-keys, they are out of order.  Once sorted, they are accessed as an array index

		keys := MapKeys(cur_data["keydict"].(map[string]interface{}))

		fmt.Printf("DddGetNodeCurrent: keydict: Keys: %v\n", keys)

		// We didnt find it, so return nil
		if cur_pos >= len(keys) || cur_pos < 0 {
			return "nil", nil, nil
		}

		selected_key := keys[cur_pos]

		fmt.Printf("DddGetNodeCurrent: keydict: Selected Key: %s\n", selected_key)

		result_cur_data := cur_data["keydict"].(map[string]interface{})[selected_key].(map[string]interface{})

		cur_record_data_map := GetResult(cur_record_data, type_map).(map[string]interface{})

		result_cur_record_data := make(map[string]interface{})
		if cur_record_data_map[selected_key] != nil {
			result_cur_record_data = GetResult(cur_record_data_map[selected_key], type_map).(map[string]interface{})
		}

		return fmt.Sprintf("Key: %s", selected_key), result_cur_data, result_cur_record_data

	} else if cur_data["rowdict"] != nil {
		// The rowdict is inside a list, but must be further selected based on the selection field, which will determine the node
		//TODO(g): ...
		return "RowDict", cur_data, cur_record_data

	} else if cur_data["list"] != nil {
		fmt.Printf("DDDGET:LIST: %T\n", cur_data["list"])
		cur_data_list := cur_data["list"].([]interface{})

		// Using the cur_pos as the index offset, this works up until the "variadic" node (if present)
		if cur_pos >= 0 && cur_pos < len(cur_data_list) {
			result_cur_data := cur_data_list[cur_pos].(map[string]interface{})

			var result_cur_record_data interface{}

			cur_record_data_array := GetResult(cur_record_data, type_array).([]interface{})

			if len(cur_record_data_array) > cur_pos {
				result_cur_record_data = cur_record_data_array[cur_pos]
			} else {
				result_cur_record_data = nil
			}

			return fmt.Sprintf("Index: %d", cur_pos), result_cur_data, result_cur_record_data
		} else {
			return "nil", nil, nil
		}

	} else if cur_data["type"] != nil {
		// This is a raw data node, and should not have any indexing, only "0" for it's location position
		if cur_pos == 0 {
			return "TBD: Get Label", cur_data, cur_record_data
		} else {
			return "nil", nil, nil
		}

	} else if cur_data["variadic"] != nil {
		// I think I have to backtrack to a previous node then?  Parent node?
		if cur_pos == 0 {
			return fmt.Sprintf("Variadic: %d", cur_pos), cur_data, cur_record_data
		} else {
			return "nil", nil, nil
		}

	} else {
		//TODO(g): Replace this panic with a non-fatal error...  But the DDD is bad, so report it?
		//panic(fmt.Sprintf("Unknown DDD node: %v", cur_data))
		return "nil", nil, nil
	}

	return "Unknown", cur_data, cur_record_data
}

func DddGetNode(position_location string, ddd_data map[string]interface{}, data_record interface{}, udn_data map[string]interface{}) (string, map[string]interface{}, interface{}) {
	cur_parts := strings.Split(position_location, ".")
	cur_label := ""
	fmt.Printf("DDD Get Node: Parts: %s: %v\n", position_location, cur_parts)

	// Current position starts from ddd_data, and then we navigate it, and return it when we find the node
	cur_data := ddd_data
	cur_record_data := data_record

	processed_parts := make([]int, 0)

	// The first "0" is always "0", and is the base cur_data, so let's pop it off
	if len(cur_parts) > 1 {
		// Add the part we just processed to our processed_parts slice to keep track of them
		cur_pos, _ := strconv.Atoi(cur_parts[0])
		processed_parts = append(processed_parts, cur_pos)

		fmt.Printf("DddGetNode: Removing first part: %v\n", cur_parts)
		cur_parts = cur_parts[1:len(cur_parts)]
		fmt.Printf("DddGetNode: Removed first part: %v\n", cur_parts)
	} else {
		if position_location == "0" {
			// There are no other parts, so we have the data
			fmt.Printf("DddGetNode: First part is '0': %s\n", position_location)
			return "The Beginninging", cur_data, cur_record_data
		} else {
			// Asking for data which cannot exist.  The first part can only be 0
			fmt.Printf("DddGetNode: First part is only part, and isnt '0': %s\n", position_location)
			return "The Somethingelseinging", nil, nil
		}
	}



	// As long as we still have cur_parts, keep going.  If we dont return in this block, we will have an empty result
	for len(cur_parts) > 0 {
		cur_pos, _ := strconv.Atoi(cur_parts[0])
		fmt.Printf("DDD Move: Step: Parts: %v   Current: %d  Cur Node: %s  Cursor Data: %s\n", cur_parts, cur_pos, SnippetData(cur_data, 80), SnippetData(cur_record_data, 80))

		cur_label, cur_data, cur_record_data = _DddGetNodeCurrent(cur_data, cur_record_data, cur_pos, processed_parts, cur_parts)

		// Add the part we just processed to our processed_parts slice to keep track of them
		processed_parts = append(processed_parts, cur_pos)

		// Pop off the first element, so we keep going
		if len(cur_parts) > 1 {
			cur_parts = cur_parts[1:len(cur_parts)]
		} else {
			cur_parts = make([]string, 0)
		}

		// If we have nothing left to process, return the result
		if len(cur_parts) == 0 {
			fmt.Printf("DddGetNode: Result: %s: Node Data: %s  Cursor Data: %s\n", position_location, SnippetData(cur_data, 80), SnippetData(cur_record_data, 80))
			return cur_label, cur_data, cur_record_data
		} else if cur_data["type"] != nil || cur_data["variadic"] != nil || cur_data["rowdict"] != nil {
			return cur_label, nil, nil
		}
	}

	// No data at this location, or we would have returned it already
	fmt.Printf("DddGetNode: No result, returning nil: %v\n", cur_parts)
	return "nil", nil, nil
}

func GetDddNodeSummary(cur_label string, cur_data map[string]interface{}) string {
	// This is our result, setting to unknown, which should never be displayed
	summary := "Unknown: FIX"

	if cur_data["keydict"] != nil {
		keys := MapKeys(cur_data["keydict"].(map[string]interface{}))

		summary = fmt.Sprintf("%s: KeyDict: %v", cur_label, strings.Join(keys, ", "))

	} else if cur_data["rowdict"] != nil {
		keys := MapKeys(cur_data["rowdict"].(map[string]interface{})["switch_rows"].(map[string]interface{}))

		summary = fmt.Sprintf("%s: RowDict: Rows: %d:  %v", cur_label, len(cur_data["rowdict"].(map[string]interface{})), strings.Join(keys, ", "))

	} else if cur_data["list"] != nil {
		cur_list := cur_data["list"].([]interface{})

		item_summary := make([]string, 0)
		for _, item := range cur_data["list"].([]interface{}) {
			item_summary = append(item_summary, GetDddNodeSummary("", item.(map[string]interface{})))
		}
		item_summary_str := strings.Join(item_summary, ", ")


		summary = fmt.Sprintf("%s: List (%d): %s", cur_label, len(cur_list), item_summary_str)

	} else if cur_data["type"] != nil {
		summary = fmt.Sprintf("%s: Data Item: Type: %s", cur_label, cur_data["type"])

	} else if cur_data["variadic"] != nil {
		summary = fmt.Sprintf("%s: Variadic", cur_label)
	}

	// Crop long summaries
	if len(summary) > 60 {
		summary = summary[0:60]
	}

	return summary
}

func GetFieldMapFromSpec(data map[string]interface{}, label string, name string) map[string]interface{} {
	field_map := make(map[string]interface{})

	if data["type"] == "string" || data["type"] == "int" || data["type"] == "boolean" {
		icon := "icon-make-group"
		if data["icon"] != nil {
			icon = data["icon"].(string)
		}

		size := 12
		if data["size"] != nil {
			size = int(data["size"].(float64))
		}

		field_map = map[string]interface{}{
			"color": "primary",
			"icon": icon,
			"info": "",
			"label": label,
			"name": name,
			"placeholder": "",
			"size": size,
			"type": "text",
			"value": "",
		}
	}

	return field_map
}

func DddRenderNode(position_location string, ddd_id int64, temp_id int64, ddd_label string, ddd_node map[string]interface{}, ddd_cursor_data interface{}) []interface{} {
	rows := make([]interface{}, 0)

	//// Add the current row, so we work with them
	//cur_row := make([]interface{}, 0)
	//rows = append(rows, cur_row)

	if ddd_node["type"] != nil {
		field_name := fmt.Sprintf("ddd_node_%s", position_location)
		new_html_field := GetFieldMapFromSpec(ddd_node, ddd_label, field_name)
		rows = AppendArray(rows, new_html_field)
	} else if ddd_node["keydict"] != nil {
		html_element_name := fmt.Sprintf("ddd_node_%s", position_location)

		// Keydict select fields, navs to them, so we dont have to button nav
		new_html_field := map[string]interface{}{
			"color": "primary",
			"icon": "icon-make-group",
			"info": "",
			"label": ddd_label,
			"name": html_element_name,
			"placeholder": "",
			"size": "12",
			"type": "select",
			"value": "",
			"value_match":"select_option_match",
			"value_nomatch":"select_option_nomatch",
			"null_message": "- Select to Navigate -",
			"items": fmt.Sprintf("__input.%s", MapKeysToUdnMapForHtmlSelect(position_location, ddd_node["keydict"].(map[string]interface{}))),
			"onchange": fmt.Sprintf("$(this).closest('.ui-dialog-content').dialog('close'); RPC('/api/dwi_render_ddd', {'move_x': 0, 'move_y': 0, 'position_location': $(this).val(), 'ddd_id': %d, 'is_delete': 0, 'web_data_widget_instance_id': '{{{_id}}}', 'web_widget_instance_id': '{{{web_widget_instance_id}}}', '_web_data_widget_instance_id': 34, 'dom_target_id':'dialog_target', 'temp_id': %d})", ddd_id, temp_id),
		}
		rows = AppendArray(rows, new_html_field)
	} else if ddd_node["list"] != nil {
		map_values := make([]string, 0)

		for index, data := range ddd_node["list"].([]interface{}) {
			summary := GetDddNodeSummary(ddd_label, data.(map[string]interface{}))

			new_position := fmt.Sprintf("%s.%d", position_location, index)

			map_values = append(map_values, fmt.Sprintf("{name='%s',value='%s'}", summary, new_position))
		}

		map_value_str := strings.Join(map_values, ",")

		udn_final := fmt.Sprintf("[%s]", map_value_str)

		html_element_name := fmt.Sprintf("ddd_node_%s", position_location)

		// Keydict select fields, navs to them, so we dont have to button nav
		new_html_field := map[string]interface{}{
			"color": "primary",
			"icon": "icon-make-group",
			"info": "",
			"label": ddd_label,
			"name": html_element_name,
			"placeholder": "",
			"size": "12",
			"type": "select",
			"value": "",
			"value_match":"select_option_match",
			"value_nomatch":"select_option_nomatch",
			"null_message": "- Select to Navigate -",
			"items": fmt.Sprintf("__input.%s", udn_final),
			"onchange": fmt.Sprintf("$(this).closest('.ui-dialog-content').dialog('close'); RPC('/api/dwi_render_ddd', {'move_x': 0, 'move_y': 0, 'position_location': $(this).val(), 'ddd_id': %d, 'is_delete': 0, 'web_data_widget_instance_id': '{{{_id}}}', 'web_widget_instance_id': '{{{web_widget_instance_id}}}', '_web_data_widget_instance_id': 34, 'dom_target_id':'dialog_target', 'temp_id': %d})", ddd_id, temp_id),
		}
		rows = AppendArray(rows, new_html_field)
	} else if ddd_node["rowdict"] != nil {
		// Sort by rows and columns, if available, if not, sort them and put them at the end, 1 per row
		unsorted := make([]map[string]interface{}, 0)

		layout := make(map[int]map[int]map[string]interface{})

		//TODO(g): We will assume data initially, so we can start up
		data_switch_field := "text"

		// Select the spec from the switch_field
		selected_row_dict_spec := ddd_node["rowdict"].(map[string]interface{})["switch_rows"].(map[string]interface{})[data_switch_field].(map[string]interface{})

		for key, value := range selected_row_dict_spec {
			value_map := value.(map[string]interface{})

			new_item := make(map[string]interface{})
			new_item[key] = value

			if value_map["x"] != nil && value_map["y"] != nil {
				// Put them in Y first, because we care about ordering by rows first, then columns once in a specific row
				if layout[int(value_map["y"].(float64))] == nil {
					layout[int(value_map["y"].(float64))] = make(map[int]map[string]interface{})
				}
				layout[int(value_map["y"].(float64))][int(value_map["x"].(float64))] = new_item
			} else {
				unsorted = append(unsorted, new_item)
			}
		}

		fmt.Printf("DddRenderNode: RowDict: Layout: %s\n\n", JsonDump(layout))

		// Get the Y keys
		y_keys := make([]int, len(layout))
		i := 0
		for key := range layout {
			y_keys[i] = key
			i++
		}
		sort.Ints(y_keys)

		// Loop over our rows
		max_y := ArrayIntMax(y_keys)
		for cur_y := 0 ; cur_y <= max_y ; cur_y++ {
			//fmt.Printf("DddRenderNode: RowDict: Y: %d\n", cur_y)

			if layout[cur_y] != nil {
				layout_row := layout[cur_y]

				// Get the Y keys
				x_keys := make([]int, len(layout_row))
				i := 0
				for key := range layout_row {
					x_keys[i] = key
					i++
				}
				sort.Ints(x_keys)

				// Loop over our cols
				max_x := ArrayIntMax(x_keys)
				for cur_x := 0 ; cur_x <= max_x ; cur_x++ {
					//fmt.Printf("DddRenderNode: RowDict: Y: %d  X: %d\n", cur_y, cur_x)

					if layout_row[cur_x] != nil {
						layout_item := layout_row[cur_x]

						field_name := fmt.Sprintf("ddd_node_%s__%d_%d", position_location, cur_x, cur_y)

						for layout_key, layout_data := range layout_item {
							layout_data_map := layout_data.(map[string]interface{})
							new_html_field := GetFieldMapFromSpec(layout_data_map, layout_data_map["label"].(string), field_name)

							fmt.Printf("DddRenderNode: RowDict: Y: %d  X: %d:  %s: %v\n", cur_y, cur_x, layout_key, layout_data_map)
							fmt.Printf("%s\n", JsonDump(new_html_field))

							rows = AppendArray(rows, new_html_field)
						}
					} else {
						//TODO(g): Put in empty columns, once I figure it out.  Empty HTML?  That could work...
						fmt.Printf("DddRenderNode: RowDict: Y: %d  X: %d:  No data here, missing column\n", cur_y, cur_x)
					}
				}
			} else {
				//TODO(g): Skip rows?  Or just ignore them?  I dont think I need to render empty rows...
			}

			//// Add the current row, so we work with them
			//cur_row := make([]interface{}, 0)
			//rows = append(rows, cur_row)
		}

		if len(unsorted) > 0 {
			// Sort them based on their ["name"] field
			//TODO(g): TBD...  Then render them after the others in rows
		}
	}

	return rows
}

func ArrayIntMax(ints []int) int {
	highest := ints[0]

	for _, cur_int := range ints {
		if cur_int > highest {
			highest = cur_int
		}
	}

	return highest
}

func MapKeysToUdnMapForHtmlSelect(position_location string, data map[string]interface{}) string {
	keys := MapKeys(data)

	fmt.Printf("MapKeysToUdnMapForHtmlSelect: %v\n  Keys: %v\n", data, keys)

	map_values := make([]string, 0)

	for index, key := range keys {
		new_position := fmt.Sprintf("%s.%d", position_location, index)

		map_values = append(map_values, fmt.Sprintf("{name='%s',value='%s'}", key, new_position))
	}

	map_value_str := strings.Join(map_values, ",")

	udn_final := fmt.Sprintf("[%s]", map_value_str)

	fmt.Printf("MapKeysToUdnMapForHtmlSelect: Result: %s\n", udn_final)

	return udn_final
}

func MapKeysToUdnMap(data map[string]interface{}) string {
	keys := MapKeys(data)

	fmt.Printf("MapKeysToUdnMap: %v\n  Keys: %v\n", data, keys)

	map_values := make([]string, 0)

	for _, key := range keys {
		map_values = append(map_values, fmt.Sprintf("%s='%s'", key, key))
	}

	map_value_str := strings.Join(map_values, ",")

	udn_final := fmt.Sprintf("{%s}", map_value_str)

	fmt.Printf("MapKeysToUdnMap: Result: %s\n", udn_final)

	return udn_final
}

func MapArrayToToUdnMap(map_array []map[string]interface{}, key_key string, value_key string) string {
	map_values := make([]string, 0)

	for _, data := range map_array {
		map_values = append(map_values, fmt.Sprintf("%s='%s'", data[key_key], data[value_key]))
	}

	map_value_str := strings.Join(map_values, ",")

	udn_final := fmt.Sprintf("{%s}", map_value_str)

	return udn_final
}

func UDN_Login(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	result := UdnResult{}

	username := GetResult(args[0], type_string).(string)
	password := GetResult(args[1], type_string).(string)

	ldap_user := LdapLogin(username, password)

	user_map := make(map[string]interface{})

	// Get the user data, if they authed
	if ldap_user.IsAuthenticated == true {
		user_map["first_name"] = ldap_user.FirstName
		user_map["full_name"] = ldap_user.FullName
		user_map["email"] = ldap_user.Email
		user_map["home_dir"] = ldap_user.HomeDir
		user_map["uid"] = ldap_user.Uid
		user_map["username"] = ldap_user.Username
		user_map["groups"] = ldap_user.Groups
		user_map["error"] = ""

		// Store it in UDN global as well
		//TODO(g): Save into the DB as our User Session...
		udn_data["ldap_user"] = user_map
	} else {
		user_map["error"] = ldap_user.Error

		result.Result = user_map
		result.Error = ldap_user.Error
		return result
	}


	// Get the user (if it exists)
	filter := map[string]interface{} {}

	filter_options := make(map[string]interface{})
	user_data_result := DatamanFilter("user", filter, filter_options)

	fmt.Printf("DatamanFilter: RESULT: %v\n", user_data_result)

	var user_data map[string]interface{}

	if len(user_data_result) == 0 {
		// Need to create this user
		user_data = make(map[string]interface{})
		user_data["name"] = ldap_user.Username
		user_data["email"] = ldap_user.Email
		user_data["name_full"] = ldap_user.FullName
		user_data["user_domain_id"] = 1		//TODO(g): Make dynamic

		// Save the LDAP data
		user_map_json, err := json.Marshal(user_map)
		if err != nil {
			UdnLog(udn_schema, "Cannot marshal User Map data: %s\n", err)
		}
		user_data["ldap_data_json"] = string(user_map_json)

		// Save the new user into the DB
		user_data = DatamanSet("user", user_data)

	} else {
		//TODO(g): Remove once I can use filters...
		for _, user_data_item := range user_data_result {
			if user_data_item["name"] == ldap_user.Username {
				// Save this user
				user_data = user_data_item

			}
		}
	}


	// Get the web_user_session
	web_user_session := make(map[string]interface{})
	filter = make(map[string]interface{})
	filter["user_id"] = user_data["_id"]
	filter["web_site_id"] = 1		//TODO(g): Make dynamic
	filter_options = make(map[string]interface{})
	web_user_session_filter := DatamanFilter("web_user_session", filter, filter_options)

	if len(web_user_session_filter) == 0 {
		// If we dont have a session, create one
		web_user_session["user_id"] = user_data["_id"]
		web_user_session["web_site_id"] = 1		//TODO(g): Make dynamic

		//TODO(g): Something better than a UUID here?  I think its the best option actually.  Could salt it with a digest...
		id := ksuid.New()
		web_user_session["name"] = id.String()

		// Save the new user session
		web_user_session = DatamanSet("web_user_session", web_user_session)

	} else {
		// Save the session information
		web_user_session = web_user_session_filter[0]
	}



	//TODO(g): Ensure they have a user account in our DB, save the ldap_user data, update UDN with their session data...

	// Trying to update the fetch code
	/*
	get_options := make(map[string]interface{})
	get_options["web_site_id"] = web_site["_id"]
	get_options["name"] = session_id_value
	user_session := DatamanGet("web_user_session", get_options)

	get_options = make(map[string]interface{})
	get_options["_id"] = user_session["user_id"]
	user_data := DatamanGet("user", get_options)
	*/

	// What we are doing currently...
	/*
	// Verify that this user is logged in, render the login page, if they arent logged in
	udn_data["session"] = make(map[string]interface{})
	udn_data["user"] = make(map[string]interface{})
	udn_data["user_data"] = make(map[string]interface{})
	udn_data["web_site"] = web_site
	udn_data["web_site_page"] = web_site_page
	if session_value, ok := udn_data["cookie"].(map[string]interface{})["opsdb_session"]; ok {
		session_sql := fmt.Sprintf("SELECT * FROM web_user_session WHERE web_site_id = %d AND name = '%s'", web_site["_id"], SanitizeSQL(session_value.(string)))
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

			fmt.Printf( "Session Data: %v\n\n", target_map)

			udn_data["session"] = target_map

			// Load the user data too
			user_sql := fmt.Sprintf("SELECT * FROM \"user\" WHERE _id = %d", user_id)
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
	*/


	//TODO(g): Login returns the SESSION_ID

	result.Result = web_user_session["name"]

	return result
}

func UDN_DddRender(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "DDD Render: %v\n\nInput: %s\n\n", args, JsonDump(input))

	position_location := GetResult(args[0], type_string).(string)
	move_x := GetResult(args[1], type_int).(int64)
	move_y := GetResult(args[2], type_int).(int64)
	is_delete := GetResult(args[3], type_int).(int64)
	ddd_id := GetResult(args[4], type_int).(int64)
	data_location := GetResult(args[5], type_string).(string)			// The data (record) we are operating on should be at this location
	save_data := GetResult(args[6], type_map).(map[string]interface{})	// This is incoming data, and will be only for the position_location's data, not the complete record
	temp_id := GetResult(args[7], type_int).(int64)						// Initial value is passed in as 0, not empty string or nil

	UdnLog(udn_schema, "\nDDD Render: Position: %s  Move X: %d  Y: %d  Is Delete: %d  DDD: %d  Data Location: %s\nSave Data:\n%s\n\n", position_location, move_x, move_y, is_delete, ddd_id, data_location, JsonDump(save_data))


	//TEST: Add some static rows...
	input_map := input.(map[string]interface{})
	input_map_rows := input_map["form"].([]interface{})

	//TODO(g): Process the move_x/y with position location.  Get a new position location.  Do this same thing with the buttons, and test each one for validity to see if we should add that button
	//		Just update the string with the move, then do the get.  Makes it simple, no working 2 things at once.  String is manipulated, and get.  That's it.




	// -- Do work here to change stuff

	// Move, if we need to
	position_location = DddMove(position_location, move_x, move_y)
	fmt.Printf("DDD Render: After move: %s\n", position_location)

	// Get the DDD Data (record data) from our stored location (first time) or from the temp table subsequent times
	ddd_data := make(map[string]interface{})

	// Get our DDD data, so we can cache it and use it without having to query it many times
	ddd_options := make(map[string]interface{})
	ddd_data_record := DatamanGet("ddd", int(ddd_id), ddd_options)
	ddd_data = ddd_data_record["data_json"].(map[string]interface{})

	var data_record interface{}

	// If we dont have a temp_id, then we will get the data from data_location and store it into the temp table
	if temp_id == 0 {
		// Get the data we are working on
		data_record_args := make([]interface{}, 0)
		data_record_args = append(data_record_args, data_location)
		data_record = MapGet(data_record_args, udn_data)
		fmt.Printf("DddRender: Data Record: %s: %s\n\n", data_location, JsonDump(data_record))

		// Put this data into the temp table, and get our temp_id
		temp_data := make(map[string]interface{})
		temp_data["data_json"] = JsonDump(data_record)
		temp_data_result := DatamanSet("temp", temp_data)
		fmt.Printf("Temp data result: %v\n\n", temp_data_result)
		temp_id = temp_data_result["_id"].(int64)
	} else {
		// Get the ddd_data from the temp table
		temp_options := make(map[string]interface{})
		temp_record := DatamanGet("temp", int(temp_id), temp_options)

		err := json.Unmarshal([]byte(temp_record["data_json"].(string)), &data_record)
		if err != nil {
			panic(err)
		}
	}
	//fmt.Printf("DDD Data Record: (%d): %s\n\n", temp_id, JsonDump(data_record))


	// Get the DDD node, which has our
	ddd_label, ddd_node, ddd_cursor_data := DddGetNode(position_location, ddd_data, data_record, udn_data)

	fmt.Printf("DDD Node: %s\n\n", JsonDump(ddd_node))
	fmt.Printf("DDD Cursor Data: %s\n\n", JsonDump(ddd_cursor_data))


	// -- Done changing stuff, time to RENDER!

	// Render this DDD Spec Node
	ddd_spec_render_nodes := DddRenderNode(position_location, ddd_id, temp_id, ddd_label, ddd_node, ddd_cursor_data)
	if ddd_spec_render_nodes != nil {
		input_map_rows = append(input_map_rows, ddd_spec_render_nodes)
	}



	// New Row
	new_row_html := make([]interface{}, 0)

	// HTML Descriptive content  -- Showing the position so I can test it...
	new_html_field := map[string]interface{}{
		"color": "primary",
		"icon": "icon-make-group",
		"info": "",
		"label": "Position Location",
		"name": "position_location",
		"placeholder": "",
		"size": "12",
		"type": "html",
		"value":  fmt.Sprintf("<b>Cursor:</b> %s<br>%s", position_location, SnippetData(JsonDump(ddd_node), 80)),
	}
	new_row_html = AppendArray(new_row_html, new_html_field)

	// Add buttons
	input_map_rows = AppendArray(input_map_rows, new_row_html)

	// New Row
	new_row_buttons := make([]interface{}, 0)


	// Add buttons
	new_button := map[string]interface{}{
		"color": "primary",
		"icon": "icon-arrow-up8",
		"info": "",
		"label": "Move Up",
		"name": "move_up",
		"placeholder": "",
		"size": "2",
		"type": "button",
		"onclick": fmt.Sprintf("$(this).closest('.ui-dialog-content').dialog('close'); RPC('/api/dwi_render_ddd', {'move_x': 0, 'move_y': -1, 'position_location': '%s', 'ddd_id': %d, 'is_delete': 0, 'web_data_widget_instance_id': '{{{_id}}}', 'web_widget_instance_id': '{{{web_widget_instance_id}}}', '_web_data_widget_instance_id': 34, 'dom_target_id':'dialog_target', 'temp_id': %d})", position_location, ddd_id, temp_id),
		"value":  "",
	}
	// Check if the button is valid, by getting an item from this
	if _, test_node, _ := DddGetNode(DddMove(position_location, 0, -1), ddd_data, data_record, udn_data) ; test_node == nil {
		new_button["color"] = ""
		new_button["onclick"] = ""
	}
	new_row_buttons = AppendArray(new_row_buttons, new_button)

	new_button = map[string]interface{}{
		"color": "primary",
		"icon": "icon-arrow-down8",
		"info": "",
		"label": "Move Down",
		"name": "move_down",
		"placeholder": "",
		"size": "2",
		"type": "button",
		"onclick": fmt.Sprintf("$(this).closest('.ui-dialog-content').dialog('close'); RPC('/api/dwi_render_ddd', {'move_x': 0, 'move_y': 1, 'position_location': '%s', 'ddd_id': %d, 'is_delete': 0, 'web_data_widget_instance_id': '{{{_id}}}', 'web_widget_instance_id': '{{{web_widget_instance_id}}}', '_web_data_widget_instance_id': 34, 'dom_target_id':'dialog_target', 'temp_id': %d})", position_location, ddd_id, temp_id),
		"value":  "",
	}
	// Check if the button is valid, by getting an item from this
	if _, test_node, _ := DddGetNode(DddMove(position_location, 0, 1), ddd_data, data_record, udn_data) ; test_node == nil {
		new_button["color"] = ""
		new_button["onclick"] = ""
	}
	new_row_buttons = AppendArray(new_row_buttons, new_button)

	new_button = map[string]interface{}{
		"color": "primary",
		"icon": "icon-arrow-left8",
		"info": "",
		"label": "Move Left",
		"name": "move_left",
		"placeholder": "",
		"size": "2",
		"type": "button",
		"onclick": fmt.Sprintf("$(this).closest('.ui-dialog-content').dialog('close'); RPC('/api/dwi_render_ddd', {'move_x': -1, 'move_y': 0, 'position_location': '%s', 'ddd_id': %d, 'is_delete': 0, 'web_data_widget_instance_id': '{{{_id}}}', 'web_widget_instance_id': '{{{web_widget_instance_id}}}', '_web_data_widget_instance_id': 34, 'dom_target_id':'dialog_target', 'temp_id': %d})", position_location, ddd_id, temp_id),
		"value":  "",
	}
	// Check if the button is valid, by getting an item from this
	if len(position_location) == 1 {
		new_button["color"] = ""
		new_button["onclick"] = ""
	}
	new_row_buttons = AppendArray(new_row_buttons, new_button)

	new_button = map[string]interface{}{
		"color": "primary",
		"icon": "icon-arrow-right8",
		"info": "",
		"label": "Move Right",
		"name": "move_right",
		"placeholder": "",
		"size": "2",
		"type": "button",
		"onclick": fmt.Sprintf("$(this).closest('.ui-dialog-content').dialog('close'); RPC('/api/dwi_render_ddd', {'move_x': 1, 'move_y': 0, 'position_location': '%s', 'ddd_id': %d, 'is_delete': 0, 'web_data_widget_instance_id': '{{{_id}}}', 'web_widget_instance_id': '{{{web_widget_instance_id}}}', '_web_data_widget_instance_id': 34, 'dom_target_id':'dialog_target', 'temp_id': %d})", position_location, ddd_id, temp_id),
		"value":  "",
	}
	// Check if the button is valid, by getting an item from this
	if _, test_node, _ := DddGetNode(DddMove(position_location, 1, 0), ddd_data, data_record, udn_data) ; test_node == nil {
		new_button["color"] = ""
		new_button["onclick"] = ""
	}
	new_row_buttons = AppendArray(new_row_buttons, new_button)

	// Add buttons
	input_map_rows = AppendArray(input_map_rows, new_row_buttons)


	// Add static JSON field
	new_item := make(map[string]interface{})
	new_item["color"] = ""
	new_item["icon"] = "icon-file-text"
	new_item["info"] = ""
	new_item["label"] = "Static JSON"
	new_item["name"] = "static_data_json"
	new_item["placeholder"] = ""
	new_item["size"] = "12"
	new_item["type"] = "ace"
	new_item["format"] = "json"
	new_item["udn_process"] = "__json_encode"
	new_item["value"] = ""


	new_row := make([]interface{}, 0)
	new_row = AppendArray(new_row, new_item)

	input_map_rows = AppendArray(input_map_rows, new_row)

	input_map["form"] = input_map_rows


	/*

		if is_delete == 1 {
			// If we are deleting this element
			DddDelete(position_location, data_location, ddd_id, udn_data)

		} else if len(save_data) > 0 {
			// Else, If we are saving this data
			DddSet(position_location, data_location, save_data, ddd_id, udn_data)
		}

		// Is this valid data?  Returns array of validation error locations
		validation_errors := DddValidate(data_location, ddd_id, udn_data)

		// If we have validation errors, move there
		if len(validation_errors) > 0 {
			error := validation_errors[0]

			// Update the location information to the specified first location
			MapSet(MakeArray(position_location), error["location"], udn_data)
		}
	*/

	//// Get the data at our current location
	//data := DddGet(position_location, data_location, ddd_id, udn_data)
	//
	//// Get DDD node, which explains our data
	//ddd_node := DddGetNode(position_location, ddd_id, udn_data)

	result := UdnResult{}
	result.Result = input_map		//TODO(g): Need to modify this, which is the point of this function...

	fmt.Printf("\nDDD Render: Result:\n%s\n\n", JsonDump(input_map))

	return result
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
			//UdnLog(udn_schema, "Found value: %s = %s\n", key, value)

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

func UDN_QueryById(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	result := UdnResult{}

	UdnLog(udn_schema, "Query: %v\n", args)

	//arg_0 := args.Front().Value.(*UdnResult)
	arg_0 := args[0]

	// The 2nd arg will be a map[string]interface{}, so ensure it exists, and get it from our args if it was passed in
	arg_1 := make(map[string]interface{})
	if len(args) > 1 {
		//UdnLog(udn_schema, "Query: %s  Stored Query: %s  Data Args: %v\n", udn_start.Value, arg_0, args[1])

		//TODO(g):VALIDATE: Validation and error handling
		arg_1 = GetResult(args[1], type_map).(map[string]interface{})
	}

	UdnLog(udn_schema, "Query: %s  Stored Query: %s  Data Args: %v\n", udn_start.Value, arg_0, arg_1)



	query_sql := fmt.Sprintf("SELECT * FROM datasource_query WHERE _id = %s", arg_0)


	//TODO(g): Make a new function that returns a list of UdnResult with map.string

	// This returns an array of TextTemplateMap, original method, for templating data
	query_result := Query(db, query_sql)

	sql_parameters := make(map[string]string)
	has_params := false
	if query_result[0]["parameter_json_data"] != nil {
		//UdnLog(udn_schema, "-- Has params: %v\n", query_result[0]["parameter_data_json"])
		err := json.Unmarshal([]byte(query_result[0]["parameter_json_data"].(string)), &sql_parameters)
		if err != nil {
			log.Panic(err)
		}
		has_params = true
	} else {
		UdnLog(udn_schema, "-- No params\n")
	}

	result_sql := fmt.Sprintf(query_result[0]["sql"].(string))

	UdnLog(udn_schema, "Query: SQL: %s   Params: %v\n", result_sql, sql_parameters)

	// If we have params, then format the string for each of them, from our arg map data
	if has_params {
		for param_key, _ := range sql_parameters {
			replace_str := fmt.Sprintf("{{%s}}", param_key)
			//value_str := fmt.Sprintf("%s", param_value)

			// Get the value from the arg_1
			value_str := fmt.Sprintf("%v", arg_1[param_key])

			//UdnLog(udn_schema, "REPLACE PARAM:  Query: SQL: %s   Replace: %s   Value: %s\n", result_sql, replace_str, value_str)

			result_sql = strings.Replace(result_sql, replace_str, value_str, -1)

			//UdnLog(udn_schema, "POST-REPLACE PARAM:  Query: SQL: %s   Replace: %s   Value: %s\n", result_sql, replace_str, value_str)
		}

		UdnLog(udn_schema, "Query: Final SQL: %s\n", result_sql)
	}


	// This query returns a list.List of map[string]interface{}, new method for more-raw data
	result.Result = UDN_Library_Query(db, result_sql)

	UdnLog(udn_schema, "Query: Result [Items: %d]: %s\n", len(result.Result.([]interface{})), SnippetData(GetResult(result, type_string), 60))

	//// DEBUG
	//result_list := result.Result.(*list.List)
	//for item := result_list.Front(); item != nil; item = item.Next() {
	//	real_item := item.Value.(map[string]interface{})
	//	UdnLog(udn_schema, "Query Result Value: %v\n", real_item)
	//}


	return result
}

func UDN_DebugOutput(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	result := UdnResult{}
	result.Result = input

	type_str := fmt.Sprintf("%T", input)

	if type_str == "*list.List" {
		UdnLog(udn_schema, "Debug Output: List: %s: %v\n", type_str, SprintList(*input.(*list.List)))

	} else {
		UdnLog(udn_schema, "Debug Output: %s: %s\n", type_str, JsonDump(input))
	}

	return result
}

func UDN_TestReturn(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Test Return data: %s\n", args[0])

	result := UdnResult{}
	result.Result = args[0]

	return result
}

func UDN_Widget(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Widget: %v\n", args[0])

	udn_data_page := udn_data["page"].(map[string]interface{})

	result := UdnResult{}
	//result.Result = udn_data["widget"].Map[arg_0.Result.(string)]
	result.Result = udn_data_page[args[0].(string)]			//TODO(g): We get this from the page map.  Is this is the best naming?  Check it...

	return result
}

func UDN_StringTemplateFromValueShort(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {

	//UdnLog(udn_schema, "\n\nShort Template: %v  Input: %v\n\n", SnippetData(args, 60), SnippetData(input, 60))
	//UdnLog(udn_schema, "\n\n--- Short Template ---: %v  Input:\n%v\n\n", SnippetData(args, 60), input)

	// If arg_1 is present, use this as the input instead of input
	actual_input := input
	if len(args) >= 2 {
		actual_input = args[1]
	}

	// If this is an array, convert it to a string, so it is a concatenated string, and then can be properly turned into a map.
	if actual_input != nil {
		if strings.HasPrefix(fmt.Sprintf("%T", actual_input), "[]") {
			UdnLog(udn_schema, "Short Template: Converting from array to string: %s\n", SnippetData(actual_input, 60))
			actual_input = GetResult(actual_input, type_string)
		} else {
			UdnLog(udn_schema, "Short Template: Input is not an array: %s\n", SnippetData(actual_input, 60))
			//UdnLog(udn_schema, "String Template: Input is not an array: %s\n", actual_input)
		}
	} else {
		UdnLog(udn_schema, "Short Template: Input is nil\n")
	}

	template_str := GetResult(args[0], type_string).(string)

	UdnLog(udn_schema, "Short Template From Value: Template String: %s Template Input: %v\n\n", SnippetData(actual_input, 60), SnippetData(template_str, 60))

	// Use the actual_input, which may be input or arg_1
	input_template_map := GetResult(actual_input, type_map).(map[string]interface{})

	for key, value := range input_template_map {
		fmt.Printf("Key: %v   Value: %v\n", key, value)
		key_replace := fmt.Sprintf("{{{%s}}}", key)
		value_str := GetResult(value, type_string).(string)
		template_str = strings.Replace(template_str, key_replace, value_str, -1)
	}

	result := UdnResult{}
	result.Result = template_str

	UdnLog(udn_schema, "Short Template From Value:  Result:  %v\n\n", template_str)

	return result
}

func UDN_StringTemplateFromValue(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {

	//UdnLog(udn_schema, "\n\nString Template: \n%v\n\n", args)

	// If arg_1 is present, use this as the input instead of input
	actual_input := input
	if len(args) >= 2 {
		actual_input = args[1]
	}

	// If this is an array, convert it to a string, so it is a concatenated string, and then can be properly turned into a map.
	if actual_input != nil {
		if strings.HasPrefix(fmt.Sprintf("%T", actual_input), "[]") {
			UdnLog(udn_schema, "String Template: Converting from array to string: %s\n", SnippetData(actual_input, 60))
			actual_input = GetResult(actual_input, type_string)
		} else {
			UdnLog(udn_schema, "String Template: Input is not an array: %s\n", SnippetData(actual_input, 60))
			//UdnLog(udn_schema, "String Template: Input is not an array: %s\n", actual_input)
		}
	} else {
		UdnLog(udn_schema, "String Template: Input is nil\n")
	}

	template_str := GetResult(args[0], type_string).(string)

	UdnLog(udn_schema, "String Template From Value: Template String: %s Template Input: %v\n\n", SnippetData(actual_input, 60), SnippetData(template_str, 60))

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

func UDN_StringTemplateMultiWrap(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {

	//UdnLog(udn_schema, "\n\nString Template: \n%v\n\n", args)

	wrap_key := GetResult(args[0], type_string).(string)

	// Ensure our arg count is correct
	if len(args) < 2 {
		panic("Wrong number of arguments.  Map Template takes N 2-tuples: set_key, map_data.  The first map_data may be skipped if there is only one set_key, input will be used.")
	} else if len(args) > 3 || len(args) % 2 != 1 {
		panic("Wrong number of arguments.  Map Template takes N 2-tuples: set_key, map_data")
	}

	items := (len(args)-1) / 2

	current_output := ""

	// If arg_1 is present, use this as the input instead of input
	current_input := input
	if len(args) >= 3 {
		current_input = GetResult(args[2], type_map).(map[string]interface{})
	}

	for count := 0 ; count < items ; count++ {
		offset := count * 2

		// Use the input we already had set up before this for loop for the actual_input, initially, every other iteration use our arg map data
		if count > 0 {
			current_input = GetResult(args[offset+2], type_map).(map[string]interface{})

			// Every iteration, after the first, we set the previous current_output to the "value" key, which is the primary content (by convention) in our templates
			current_input.(map[string]interface{})[wrap_key] = current_output
		}

		// Prepare to template
		template_str := GetResult(args[offset+1], type_string).(string)

		UdnLog(udn_schema, "String Template From Value: Template String: %s Template Input: %v\n\n", SnippetData(current_input, 60), SnippetData(template_str, 60))

		// Use the actual_input, which may be input or arg_1
		input_template := NewTextTemplateMap()
		input_template.Map = GetResult(current_input, type_map).(map[string]interface{})

		item_template := template.Must(template.New("text").Parse(template_str))

		item := StringFile{}
		err := item_template.Execute(&item, input_template)
		if err != nil {
			log.Fatal(err)
		}

		// Set the current_output for return, and put it in our udn_data, so we can access it again
		current_output = item.String
	}

	result := UdnResult{}
	result.Result = current_output

	return result
}

func UDN_MapStringFormat(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Map String Format: %v\n", args)

	// Ensure our arg count is correct
	if len(args) < 2 || len(args) % 2 != 0 {
		panic("Wrong number of arguments.  Map Template takes N 2-tuples: set_key, format")
	}

	items := len(args) / 2

	for count := 0 ; count < items ; count++ {
		offset := count * 2

		set_key := GetResult(args[offset+0], type_string).(string)
		format_str := GetResult(args[offset+1], type_string_force).(string)

		UdnLog(udn_schema, "Format: %s  Format String: %s  Input: %v\n", set_key, SnippetData(format_str, 60), SnippetData(input, 60))

		if input != nil {
			input_template := NewTextTemplateMap()
			input_template.Map = input.(map[string]interface{})

			item_template := template.Must(template.New("text").Parse(format_str))

			item := StringFile{}
			err := item_template.Execute(&item, input_template)
			if err != nil {
				log.Fatal(err)
			}

			// Save the templated string to the set_key in our input, so we are modifying our input
			input.(map[string]interface{})[set_key] = item.String

			UdnLog(udn_schema, "Format: %s  Result: %s\n\n", set_key, item.String)
		} else {
			input.(map[string]interface{})[set_key] = format_str

			UdnLog(udn_schema, "Format: %s  Result (No Templating): %s\n\n", set_key, format_str)
		}

	}

	result := UdnResult{}
	result.Result = input

	UdnLog(udn_schema, "Map String Format: Result: %s\n\n", JsonDump(input))

	return result
}

func UDN_MapTemplate(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Map Template: %v\n", args)

	// Ensure our arg count is correct
	if len(args) < 3 || len(args) % 3 != 0 {
		panic("Wrong number of arguments.  Map Template takes N 3-tuples: set_key, text, map")
	}

	items := len(args) / 3

	for count := 0 ; count < items ; count++ {
		offset := count * 3

		set_key := args[offset].(string)
		template_str := GetResult(args[offset+1], type_string).(string)
		template_data := GetResult(args[offset+2], type_map).(map[string]interface{})

		UdnLog(udn_schema, "Map Template: %s Template String: %s Template Data: %v Template Input: %v\n\n", set_key, SnippetData(template_str, 60), SnippetData(template_data, 60), SnippetData(input, 60))

		input_template := NewTextTemplateMap()
		input_template.Map = template_data

		item_template := template.Must(template.New("text").Parse(template_str))

		item := StringFile{}
		err := item_template.Execute(&item, input_template)
		if err != nil {
			log.Fatal(err)
		}

		// Save the templated string to the set_key in our input, so we are modifying our input
		input.(map[string]interface{})[set_key] = item.String
	}

	result := UdnResult{}
	result.Result = input

	return result
}

func UDN_MapUpdate(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	update_map := GetResult(args[0], type_map).(map[string]interface{})

	// Update the input map's fields with the arg0 map
	UdnLog(udn_schema, "Map Update: %s  Over Input: %s\n", SnippetData(update_map, 60), SnippetData(input, 60))

	for k, v := range update_map {
		input.(map[string]interface{})[k] = v
	}

	result := UdnResult{}
	result.Result = input

	fmt.Printf("Map Update: Result: %v", input)

	return result
}

func UDN_HtmlEncode(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "HTML Encode: %v\n", SnippetData(input, 80))

	input_str := GetResult(input, type_string).(string)

	// Replace all the characters with their fixed HTML alternatives
	input_str = strings.Replace(input_str, "<", "&lt;", -1)
	input_str = strings.Replace(input_str, ">", "&gt;", -1)
	input_str = strings.Replace(input_str, "&", "&amp;", -1)

	result := UdnResult{}
	result.Result = input_str

	//UdnLog(udn_schema, "HTML Encode: Result: %v\n", SnippetData(input_str, 80))
	UdnLog(udn_schema, "HTML Encode: Result: %v\n", input_str)

	return result
}

func UDN_StringAppend(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "String Append: %v\n", args)

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

	UdnLog(udn_schema, "String Append: %v  Current: %s  Append (%T): %s\n\n", args, SnippetData(access_str, 60), input, SnippetData(input, 60))

	// Append
	access_str = fmt.Sprintf("%s%s", access_str, GetResult(input, type_string).(string))

	//UdnLog(udn_schema, "String Append: %v  Appended:\n%s\n\n", args, access_str)		//DEBUG

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

func UDN_StringClear(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "String Clear: %v\n", args)

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

func UDN_StringConcat(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "String Concat:\n")

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

func UDN_Input(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	// If we have no arguments, return our input as the result.  This is used for passing our input into a function argument
	if len(args) == 0 {
		result := UdnResult{}
		result.Result = input
		UdnLog(udn_schema, "Input: No args, returning input: %v\n", input)
		return result
	}

	UdnLog(udn_schema, "Input: %v\n", args[0])

	result := UdnResult{}
	result.Result = args[0]

	return result
}

func UDN_InputGet(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	cur_result := input

	UdnLog(udn_schema, "Input Get: %v   Input: %v\n", args, SnippetData(input, 60))

	for _, arg := range args {
		switch arg.(type) {
		case string:
			cur_result = cur_result.(map[string]interface{})[arg.(string)]
		default:
			//TODO(g): Support ints?  Make this a stand alone function, and just call it from the UDN function
			cur_result = nil
		}
	}

	result := UdnResult{}
	result.Result = cur_result

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

func UDN_StoredFunction(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Stored Function: %s\n", SnippetData(args, 80))

	function_name := GetResult(args[0], type_string).(string)

	function_domain_id := udn_data["web_site"].(map[string]interface{})["udn_stored_function_domain_id"]

	sql := fmt.Sprintf("SELECT * FROM udn_stored_function WHERE name = '%s' AND udn_stored_function_domain_id = %d", function_name, function_domain_id)

	function_rows := Query(db, sql)

	// Get all our args, after the first one (which is our function_name)
	udn_data["function_arg"] = GetResult(args[1:], type_map)

	//UdnLog(udn_schema, "Stored Function: Args: %d: %s\n", len(udn_data["function_arg"].(map[string]interface{})), SprintMap(udn_data["function_arg"].(map[string]interface{})))

	// Our result, whether we populate it or not
	result := UdnResult{}

	if len(function_rows) > 0 {
		result.Result = ProcessSchemaUDNSet(db, udn_schema, function_rows[0]["udn_data_json"].(string), udn_data)
	}

	return result
}

func UDN_Execute(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	udn_target := GetResult(args[0], type_string).(string)

	UdnLog(udn_schema, "Execute: UDN String As Target: %s\n", udn_target)

	// Execute the Target against the input
	result := UdnResult{}
	//result.Result = ProcessUDN(db, udn_schema, udn_source, udn_target, udn_data)

	result.Result = ProcessSingleUDNTarget(db, udn_schema, udn_target, input, udn_data)

	return result
}

func UDN_ArrayAppend(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	//UdnLog(udn_schema, "Array Append: %v\n", args)

	// Get whatever we have stored at that location
	array_value_potential := MapGet(args, udn_data)

	// Force it into an array
	array_value := GetResult(array_value_potential, type_array).([]interface{})

	// Append the input into our array
	array_value = AppendArray(array_value, input)

	// Save the result back into udn_data
	MapSet(args, array_value, udn_data)

	// Return the array
	result := UdnResult{}
	result.Result = array_value

	return result
}

func UDN_ArrayDivide(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	divisor, err := strconv.Atoi(args[0].(string))

	// Dont process this, if it isnt valid...  Just pass through
	if err != nil || divisor <= 0 {
		UdnLog(udn_schema, "ERROR: Divisor is invalid: %d\n", divisor)
		result := UdnResult{}
		result.Result = input
		return result
	}

	UdnLog(udn_schema, "Array Divide: %v\n", divisor)

	// Make the new array.  This will be a 2D array, from our 1D input array
	result_array := make([]interface{}, 0)
	current_array := make([]interface{}, 0)


	// Loop until we have taken account of all the elements in the array
	for count, element := range input.([]interface{}) {
		if count % divisor == 0 && count > 0 {
			result_array = AppendArray(result_array, current_array)
			current_array = make([]interface{}, 0)

			UdnLog(udn_schema, "Adding new current array: %d\n", len(result_array))
		}

		current_array = AppendArray(current_array, element)
		UdnLog(udn_schema, "Adding new current array: Element: %d\n", len(current_array))
	}

	if len(current_array) != 0 {
		result_array = AppendArray(result_array, current_array)
	}

	result := UdnResult{}
	result.Result = result_array

	return result
}

func UDN_ArrayMapRemap(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	// Get the remapping information
	arg_0 := args[0]
	remap := GetResult(arg_0, type_map).(map[string]interface{})

	UdnLog(udn_schema, "Array Map Remap: %v\n", remap)

	new_array := make([]interface{}, 0)

	for _, old_map := range input.([]map[string]interface{}) {
		new_map := make(map[string]interface{})

		// Remap all the old map keys to new map keys in the new map
		for new_key, old_key := range remap {
			new_map[new_key] = old_map[old_key.(string)]
		}

		// Add the new map to the new array
		new_array = AppendArray(new_array, new_map)
	}

	result := UdnResult{}
	result.Result = new_array

	return result
}

func UDN_RenderDataWidgetInstance(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	//TODO(g): Take arg3 as optional argument, which is a map of control values.  Allow "dialog=true" to wrap any result in a dialog window.  This will allow non-dialog items to be rendered in a dialog.
	//

	//TODO(g): Make Dialog Form use this and change it to Form.  Then it is ready to be used in a normal page, and I can just wrap it with a Dialog...  Pass in the dialog title and any options (width).
	//

	UdnLog(udn_schema, "Render Data Widget Instance: %v\n", args)

	dom_target_id_str := GetResult(args[0], type_string).(string)
	web_data_widget_instance_id := GetResult(args[1], type_int).(int64)
	widget_instance_update_map := GetResult(args[2], type_map).(map[string]interface{})
	udn_update_map := make(map[string]interface{})
	if len(args) > 3 {
		udn_update_map = GetResult(args[3], type_map).(map[string]interface{})
	}

	// Make this work, we can just fake the data format so it works the same as the page rendering...
	fake_site_page_widget := make(map[string]interface{})
	fake_site_page_widget["name"] = dom_target_id_str
	fake_site_page_widget["web_data_widget_instance_id"] = web_data_widget_instance_id
	fake_site_page_widget["web_widget_instance_output"] = "output." + dom_target_id_str

	// Get the web_data_widget_instance data
	sql := fmt.Sprintf("SELECT * FROM web_data_widget_instance WHERE _id = %d", web_data_widget_instance_id)
	web_data_widget_instance := Query(db, sql)[0]

	// Decode JSON static
	decoded_instance_json := make(map[string]interface{})
	if web_data_widget_instance["static_data_json"] != nil {
		err := json.Unmarshal([]byte(web_data_widget_instance["static_data_json"].(string)), &decoded_instance_json)
		if err != nil {
			log.Panic(err)
		}
	}
	udn_data["data_instance_static"] = decoded_instance_json


	// Get the web_data_widget data
	sql = fmt.Sprintf("SELECT * FROM web_data_widget WHERE _id = %d", web_data_widget_instance["web_data_widget_id"])
	web_data_widget := Query(db, sql)[0]

	// Decode JSON static
	decoded_json := make(map[string]interface{})
	if web_data_widget["static_data_json"] != nil {
		err := json.Unmarshal([]byte(web_data_widget["static_data_json"].(string)), &decoded_json)
		if err != nil {
			log.Panic(err)
		}
	}
	udn_data["data_static"] = decoded_json


	// If we dont have this bucket yet, make it
	if udn_data["widget_instance"] == nil {
		udn_data["widget_instance"] = make(map[string]interface{})
	}

	// Loop over all the keys in the widget_instance_update_map, and update them into the widget_instance
	for key, value := range widget_instance_update_map {
		udn_data["widget_instance"].(map[string]interface{})[key] = value
	}

	// Loop over all the keys in the udn_update_map, and update them directly into the udn_data.  This is for overriding things like "widget_static", which is initialized earlier
	for key, value := range udn_update_map {
		fmt.Printf("Render Data Widget Instance: Update udn_data: %s: %v\n", key, value)
		udn_data[key] = value
	}


	// Render the Widget Instance, from the web_data_widget_instance
	RenderWidgetInstance(db, udn_schema, udn_data, fake_site_page_widget, udn_update_map)

	// Prepare the result from the well-known target output location (dom_target_id_str)
	result := UdnResult{}
	result.Result = udn_data["output"].(map[string]interface{})[dom_target_id_str].(string)

	// Store this result in a well-known location which can be returned as JSON output as well
	api_result := make(map[string]interface{})
	api_result[dom_target_id_str] = result.Result
	udn_data["set_api_result"] = api_result

	return result
}

func UDN_JsonDecode(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "JSON Decode: %v\n", args)

	// Use the argument instead of input, if it exists
	if len(args) != 0 {
		input = args[0]
	}

	//decoded_map := make(map[string]interface{})
	var decoded_interface interface{}

	if input != nil {
		//err := json.Unmarshal([]byte(input.(string)), &decoded_map)
		err := json.Unmarshal([]byte(input.(string)), &decoded_interface)
		if err != nil {
			log.Panic(err)
		}
	}

	result := UdnResult{}
	//result.Result = decoded_map
	result.Result = decoded_interface

	//UdnLog(udn_schema, "JSON Decode: Result: %v\n", decoded_map)
	UdnLog(udn_schema, "JSON Decode: Result: %v\n", decoded_interface)

	return result
}

func UDN_JsonEncode(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "JSON Encode: %v\n", args)

	// Use the argument instead of input, if it exists
	if len(args) != 0 {
		input = args[0]
	}

/*	var buffer bytes.Buffer
	body, _ := json.MarshalIndent(input, "", "  ")
	buffer.Write(body)
*/
	result := UdnResult{}
	result.Result = JsonDump(input)

	UdnLog(udn_schema, "JSON Endcode: Result: %v\n", result.Result)

	return result
}

func UDN_DataGet(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Data Get: %v\n", args)

	collection_name := GetResult(args[0], type_string).(string)
	record_id := GetResult(args[1], type_int).(int64)

	options := make(map[string]interface{})
	if len(args) > 2 {
		options = GetResult(args[2], type_map).(map[string]interface{})
	}

	result_map := DatamanGet(collection_name, int(record_id), options)

	result := UdnResult{}
	result.Result = result_map

	return result
}

func UDN_DataSet(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Data Set: %v\n", args)

	collection_name := GetResult(args[0], type_string).(string)
	record := GetResult(args[1], type_map).(map[string]interface{})

	result_map := DatamanSet(collection_name, record)

	result := UdnResult{}
	result.Result = result_map

	return result
}

func UDN_DataFilter(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Data Filter: %v\n", args)

	collection_name := GetResult(args[0], type_string).(string)
	filter := GetResult(args[1], type_map).(map[string]interface{})

	// Optionally, options
	options := make(map[string]interface{})
	if len(args) >= 3 {
		options = GetResult(args[2], type_map).(map[string]interface{})
	}

	result_list := DatamanFilter(collection_name, filter, options)

	result := UdnResult{}
	result.Result = result_list

	return result
}

func UDN_MapKeyDelete(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Map Key Delete: %v\n", args)

	for _, key := range args {
		delete(input.(map[string]interface{}), key.(string))
	}

	result := UdnResult{}
	result.Result = input

	return result
}

func UDN_MapKeySet(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Map Key Set: %v\n", args)

	// Ensure our arg count is correct
	if len(args) < 2 || len(args) % 2 != 0 {
		panic("Wrong number of arguments.  Map Template takes N 2-tuples: set_key, format")
	}

	items := len(args) / 2

	for count := 0 ; count < items ; count++ {
		offset := count * 2

		set_key := GetResult(args[offset+0], type_string).(string)
		value_str := GetResult(args[offset+1], type_string_force).(string)

		UdnLog(udn_schema, "Map Key Set: %s  Value String: %s  Input: %v\n", set_key, SnippetData(value_str, 60), SnippetData(input, 60))

		input.(map[string]interface{})[set_key] = value_str

	}

	result := UdnResult{}
	result.Result = input

	UdnLog(udn_schema, "Map Key Set: Result: %s\n\n", JsonDump(input))

	return result
}

func UDN_MapCopy(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Map Copy: %v\n", args)

	new_map := make(map[string]interface{})

	for key, value := range input.(map[string]interface{}) {
		new_map[key] = value
	}

	result := UdnResult{}
	result.Result = new_map

	return result
}

func UDN_CompareEqual(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Compare: Equal: %v\n", args)

	arg0 := GetResult(args[0], type_string_force).(string)
	arg1 := GetResult(args[1], type_string_force).(string)

	value := 1
	if arg0 != arg1 {
		value = 0
	}

	fmt.Printf("Compare: Equal: '%s' == '%s' : %d\n", arg0, arg1, value)


	result := UdnResult{}
	result.Result = value

	return result
}

func UDN_CompareNotEqual(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Compare: Equal: %v\n", args)

	arg0 := GetResult(args[0], type_string_force).(string)
	arg1 := GetResult(args[1], type_string_force).(string)

	value := 1
	if arg0 == arg1 {
		value = 0
	}

	fmt.Printf("Compare: Not Equal: '%s' != '%s' : %d\n", arg0, arg1, value)

	result := UdnResult{}
	result.Result = value

	return result
}

func UDN_Test(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Test Function\n")

	result := UdnResult{}
	result.Result = "Testing.  123."

	return result
}

func UDN_TestDifferent(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Different Test Function!!!\n")

	result := UdnResult{}
	result.Result = "Testing.  Differently."

	return result
}

func UDN_Access(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "TBD: UDN Access - navigate through hierarchical data...\n")

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

func UseArgArrayOrFirstArgString(args []interface{}) []interface{} {
	// If we were given a single dotted string, expand it into our arg array
	if len(args) == 1 {
		switch args[0].(type) {
		case string:
			// If this has dots in it, then it can be exploded to become an array of args
			if strings.Contains(args[0].(string), ".") {
				new_args := SimpleDottedStringToArray(args[0].(string))

				return new_args
			}
		}
	}

	return args
}

func GetArgsFromArgsOrStrings(args []interface{}) []interface{} {
	out_args := make([]interface{}, 0)

	for _, arg := range args {
		switch arg.(type) {
		case string:
			// If this has dots in it, then it can be exploded to become an array of args
			if strings.Contains(arg.(string), ".") {
				new_args := SimpleDottedStringToArray(arg.(string))

				for _, new_arg := range new_args {
					out_args = AppendArray(out_args, new_arg)
				}
			} else {
				out_args = AppendArray(out_args, arg)
			}
		default:
			out_args = AppendArray(out_args, arg)
		}
	}

	//fmt.Printf("\n\nGetArgsFromArgsOrStrings: %v   ===>>>  %v\n\n", args, out_args)

	return out_args
}

/*
func IsArray(value interface{}) bool {
	type_str := fmt.Sprintf("%T", value)

	if strings.HasPrefix(type_str, "[]") {
		return true
	} else {
		return false
	}
}

func IsMap(value interface{}) bool {
	type_str := fmt.Sprintf("%T", value)

	if type_str == "map[string]interface {}" {
		return true
	} else {
		return false
	}
}
*/

func GetChildResult(parent interface{}, child interface{}) DynamicResult {
	type_str := fmt.Sprintf("%T", parent)
	//fmt.Printf("\n\nGetChildResult: %s: %s: %v\n\n", type_str, child, SnippetData(parent, 300))

	result := DynamicResult{}

	if strings.HasPrefix(type_str, "[]") {
		// Array access
		parent_array := parent.([]interface{})

		index := GetResult(child, type_int).(int64)

		result.Result = parent_array[index]
		result.Type = type_array

		return result

	} else {
		child_str := GetResult(child, type_string).(string)

		// Map access
		parent_map := parent.(map[string]interface{})

		result.Result = parent_map[child_str]
		result.Type = type_map

		return result
	}
}

func _MapGet(args []interface{}, udn_data map[string]interface{}) interface{} {
	// This is what we will use to Set the data into the last map[string]
	last_argument := GetResult(args[len(args)-1], type_string).(string)

	// Start at the top of udn_data, and work down
	var cur_udn_data interface{}
	cur_udn_data = udn_data

	// Go to the last element, so that we can set it with the last arg
	for count := 0; count < len(args) - 1; count++ {
		arg := GetResult(args[count], type_string).(string)

		if count != 0 {
			//fmt.Printf("Get: Cur UDN Data: Before change: %s: %v\n\n", arg, JsonDump(cur_udn_data))
		}

		child_result := GetChildResult(cur_udn_data, arg)

		if child_result.Result != nil {
			if child_result.Type == type_array {
				cur_udn_data = child_result.Result
			} else {
				cur_udn_data = child_result.Result
			}
		} else {
			// Make a new map, simulating something being here.  __set will create this, so this make its bi-directinally the same...
			cur_udn_data = make(map[string]interface{})
		}
	}

	//fmt.Printf("Get: Last Arg data: %s: %s\n\n", last_argument, SnippetData(cur_udn_data, 800))

	// Our result will be a list, of the result of each of our iterations, with a UdnResult per element, so that we can Transform data, as a pipeline
	final_result := GetChildResult(cur_udn_data, last_argument)

	return final_result.Result
}

func SetChildResult(parent interface{}, child interface{}, value interface{}) {
	type_str := fmt.Sprintf("%T", parent)
	//fmt.Printf("\n\nSetChildResult: %s: %v: %v\n\n", type_str, child, SnippetData(parent, 300))

	if strings.HasPrefix(type_str, "[]") {
		// Array access
		parent_array := parent.([]interface{})

		index := GetResult(child, type_int).(int64)

		parent_array[index] = value
	} else {
		child_str := GetResult(child, type_string).(string)

		// Map access
		parent_map := parent.(map[string]interface{})

		// Set the value
		parent_map[child_str] = value
	}
}

func _MapSet(args []interface{}, input interface{}, udn_data map[string]interface{}) {

	// This is what we will use to Set the data into the last map[string]
	last_argument := GetResult(args[len(args)-1], type_string).(string)

	// Start at the top of udn_data, and work down
	var cur_udn_data interface{}
	cur_udn_data = udn_data

	// Go to the last element, so that we can set it with the last arg
	for count := 0; count < len(args) - 1; count++ {
		child_result := GetChildResult(cur_udn_data, args[count])

		// If we dont have this key, create a map[string]interface{} to allow it to be created easily
		if child_result.Result == nil {
			new_map := make(map[string]interface{})
			SetChildResult(cur_udn_data, args[count], new_map)
			child_result = GetChildResult(cur_udn_data, args[count])
		}

		// Go down the depth of maps
		if child_result.Type == type_array {
			cur_udn_data = child_result.Result
		} else {
			cur_udn_data = child_result.Result
		}
	}

	// Set the last element
	SetChildResult(cur_udn_data, last_argument, input)
}


/*
func _MapGet(args []interface{}, udn_data map[string]interface{}) interface{} {
	// This is what we will use to Set the data into the last map[string]
	last_argument := GetResult(args[len(args)-1], type_string).(string)

	// Start at the top of udn_data, and work down
	cur_udn_data := udn_data

	// Go to the last element, so that we can set it with the last arg
	for count := 0; count < len(args) - 1; count++ {
		arg := GetResult(args[count], type_string).(string)

		if count != 0 {
			//fmt.Printf("Get: Cur UDN Data: Before change: %s: %v\n\n", arg, JsonDump(cur_udn_data))
		}

		// Go down the depth of maps
		//TODO(g): If this is an integer, it might be a list/array, but lets assume nothing but map[string] for now...
		if cur_udn_data[arg] != nil {
			cur_udn_data = cur_udn_data[arg].(map[string]interface{})
		} else {
			// Make a new map, simulating something being here.  __set will create this, so this make its bi-directinally the same...
			cur_udn_data = make(map[string]interface{})
		}
	}

	//fmt.Printf("Get: Last Arg data: %s: %s\n\n", last_argument, SnippetData(cur_udn_data, 800))

	// Our result will be a list, of the result of each of our iterations, with a UdnResult per element, so that we can Transform data, as a pipeline
	return cur_udn_data[last_argument]
}
*/

func MapGet(args []interface{}, udn_data map[string]interface{}) interface{} {
	// If we were given a single dotted string, expand it into our arg array
	args = UseArgArrayOrFirstArgString(args)

	// Only try with our first argument converted initially (legacy behavior)
	result := _MapGet(args, udn_data)

	if result == nil {
		// Try converting all our arguments if we couldnt get it before.  It might be dotted.
		args = GetArgsFromArgsOrStrings(args)

		result = _MapGet(args, udn_data)
	}

	return result
}

func MapSet(args []interface{}, input interface{}, udn_data map[string]interface{}) interface{} {
	// Determine what our args should be, based on whether the data is available for getting already, allow explicit to override depth-search
	first_args := UseArgArrayOrFirstArgString(args)
	result := _MapGet(first_args, udn_data)
	if result == nil {
		// If we didnt find this value in it's explicit (dotted) string location, then expand the dots
		args = GetArgsFromArgsOrStrings(args)
	} else {
		args = first_args
	}

	_MapSet(args, input, udn_data)

	// Input is a pass-through
	return input
}

func UDN_GetFirst(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Get First: %v\n", SnippetData(args, 300))

	result := UdnResult{}

	// Process each of our args, until one of them isnt nil
	for _, arg := range args {
		type_str := fmt.Sprintf("%T", arg)

		if strings.HasPrefix(type_str, "[]") {
			for _, item := range arg.([]interface{}) {
				arg_str := GetResult(item, type_string).(string)
				arg_array := make([]interface{}, 0)
				arg_array = AppendArray(arg_array, arg_str)

				result.Result = MapGet(arg_array, udn_data)

				// If this wasnt nil, quit
				if result.Result != nil {
					UdnLog(udn_schema, "Get First: %v   Found: %v   Value: %v\n", SnippetData(args, 300), arg_str, result.Result)
					break
				}
			}
		} else {
			arg_str := GetResult(arg, type_string).(string)
			arg_array := make([]interface{}, 0)
			arg_array = AppendArray(arg_array, arg_str)

			result.Result = MapGet(arg_array, udn_data)

			// If this wasnt nil, quit
			if result.Result != nil {
				UdnLog(udn_schema, "Get First: %v   Found: %v\n", SnippetData(args, 300), arg_str)
			}
		}

		// Always stop if we have a result here
		if result.Result != nil {
			break
		}
	}


	//UdnLog(udn_schema, "Get: %v   Result: %v\n", SnippetData(args, 80), SnippetData(result.Result, 80))
	UdnLog(udn_schema, "Get First: %v   Result: %v\n", SnippetData(args, 300), result.Result)

	return result
}

func UDN_Get(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Get: %v\n", SnippetData(args, 80))

	result := UdnResult{}
	result.Result = MapGet(args, udn_data)

	//UdnLog(udn_schema, "Get: %v   Result: %v\n", SnippetData(args, 80), SnippetData(result.Result, 80))
	UdnLog(udn_schema, "Get: %v   Result: %v\n", SnippetData(args, 80), result.Result)

	return result
}

func UDN_Set(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Set: %v   Input: %s\n", SnippetData(args, 80), SnippetData(input, 40))

	result := UdnResult{}
	result.Result = MapSet(args, input, udn_data)

	UdnLog(udn_schema, "Set: %v  Result: %s\n\n", SnippetData(args, 80), SnippetData(result.Result, 80))

	return result
}

// This returns a string with the temp prefix to be unique.  Initially just pre-pending "temp"
func UDN_GetTempAccessor(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Get Temp Accessor: %v\n", SnippetData(args, 80))

	initial_accessor := GetResult(args[0], type_string).(string)

	// Start at the top of udn_data, and work down
	//TODO(g): Ensure temp works with concurrency, we would use the concurrency block's ID to ensure uniqueness
	temp_accessor := "test." + initial_accessor

	result := UdnResult{}
	result.Result = temp_accessor

	return result
}

func UDN_GetTemp(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Get: %v\n", SnippetData(args, 80))

	// This is what we will use to Set the data into the last map[string]
	//last_argument := args.Back().Value.(string)
	//last_argument := args.Back().Value.(*UdnResult).Result.(string)
	last_argument := GetResult(args[len(args)-1], type_string).(string)

	// Start at the top of udn_data, and work down
	//TODO(g): Ensure temp works with concurrency, we would use the concurrency block's ID to ensure uniqueness
	cur_udn_data := udn_data["temp"].(map[string]interface{})

	// Go to the last element, so that we can set it with the last arg
	for count := 0; count < len(args) - 1; count++ {
		arg := GetResult(args[count], type_string).(string)

		//UdnLog(udn_schema, "Get: Cur UDN Data: Before change: %s: %v\n\n", arg, SnippetData(cur_udn_data, 300))

		// Go down the depth of maps
		//TODO(g): If this is an integer, it might be a list/array, but lets assume nothing but map[string] for now...
		cur_udn_data = cur_udn_data[arg].(map[string]interface{})
	}

	//UdnLog(udn_schema, "Get: Last Arg data: %s: %s\n\n", last_argument, SnippetData(cur_udn_data, 800))

	// Our result will be a list, of the result of each of our iterations, with a UdnResult per element, so that we can Transform data, as a pipeline
	result := UdnResult{}
	result.Result = cur_udn_data[last_argument]

	//UdnLog(udn_schema, "Get: %v   Result: %v\n", SnippetData(args, 80), SnippetData(result.Result, 80))

	return result
}

func PrettyPrint(data interface{}) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		panic(err)
	}

	return string(b)
}

func UDN_SetTemp(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Set: %v   Input: %s\n", SnippetData(args, 80), SnippetData(input, 40))

	// This is what we will use to Set the data into the last map[string]
	last_argument := GetResult(args[len(args)-1], type_string).(string)

	// Start at the top of udn_data, and work down
	//TODO(g): Ensure temp works with concurrency, we would use the concurrency block's ID to ensure uniqueness
	cur_udn_data := udn_data["temp"].(map[string]interface{})

	// Go to the last element, so that we can set it with the last arg
	for count := 0; count < len(args) - 1; count++ {
		arg := GetResult(args[count], type_string).(string)

		// If we dont have this key, create a map[string]interface{} to allow it to be created easily
		if _, ok := cur_udn_data[arg]; !ok {
			cur_udn_data[arg] = make(map[string]interface{})
		}

		// Go down the depth of maps
		//TODO(g): If this is an integer, it might be a list/array, but lets assume nothing but map[string] for now...
		cur_udn_data = cur_udn_data[arg].(map[string]interface{})
	}

	// Set the last element
	cur_udn_data[last_argument] = input

	//UdnLog(udn_schema, "Set: %s  To: %s\nResult:\n%s\n\n", last_argument, SnippetData(input, 40), PrettyPrint(udn_data))
	//UDN_Get(db, udn_schema, udn_start, args, input, udn_data)	//TODO:REMOVE:DEBUG: Checking it out using the same udn_data, for sure, because we havent left this function....

	// Input is a pass-through
	result := UdnResult{}
	result.Result = input

	return result
}

func UDN_Iterate(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	// Will loop over all UdnParts until it finds __end_iterate.  It expects input to hold a list.List, which use to iterate and execute the UdnPart blocks
	// It will set a variable that will be accessable by the "__get.current.ARG0"
	// Will return a list.List of each of the loops, which allows for filtering the iteration

	// This is our final input list, as an array, it always works and gets input to pass into the first function
	input_array := GetResult(input, type_array).([]interface{})


	UdnLog(udn_schema, "Iterate: [%s]  Input: %v\n\n", udn_start.Id, input_array)

	// Our result will be a list, of the result of each of our iterations, with a UdnResult per element, so that we can Transform data, as a pipeline
	result := UdnResult{}
	result_list := make([]interface{}, 0)

	// If we have something to iterate over
	if len(input_array) > 0 {
		// Loop over the items in the input
		for _, item := range input_array {
			UdnLog(udn_schema, "\n====== Iterate Loop Start: [%s]  Input: %v\n\n", udn_start.Id, SnippetData(item, 300))

			// Get the input
			current_input := item

			// Variables for looping over functions (flow control)
			udn_current := udn_start

			// Loop over the UdnParts, executing them against the input, allowing it to transform each time
			for udn_current != nil && udn_current.Id != udn_start.BlockEnd.Id && udn_current.NextUdnPart != nil {
				udn_current = udn_current.NextUdnPart

				//UdnLog(udn_schema, "  Walking ITERATE block [%s]: Current: %s   Current Input: %v\n", udn_start.Id, udn_current.Value, SnippetData(current_input, 60))

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
			UdnLog(udn_schema, "\n====== Iterate Finished: [%s]  NextUdnPart: %v\n\n", udn_start.Id, result.NextUdnPart)
		} else if result.NextUdnPart.NextUdnPart != nil {
			UdnLog(udn_schema, "\n====== Iterate Finished: [%s]  NextUdnPart: %v\n\n", udn_start.Id, result.NextUdnPart)
		} else {
			UdnLog(udn_schema, "\n====== Iterate Finished: [%s]  NextUdnPart: End of UDN Parts\n\n", udn_start.Id)
		}
	} else {
		// Else, there is nothing to iterate over, but we still need to get our NextUdnPart to skip iterate's execution block
		udn_current := udn_start

		// Loop over the UdnParts, executing them against the input, allowing it to transform each time
		for udn_current != nil && udn_current.Id != udn_start.BlockEnd.Id && udn_current.NextUdnPart != nil {
			udn_current = udn_current.NextUdnPart
			result.NextUdnPart = udn_current
		}
	}


	// Store the result list
	result.Result = result_list

	// Return the
	return result
}

func UDN_IfCondition(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	arg_0 := args[0]

	UdnLog(udn_schema, "If Condition: %s\n", arg_0)

	// If this is true, all other blocks (else-if, else) will be skipped.  It doesnt matter which block this is, an IF/ELSE-IF/ELSE chain only executes 1 block
	executed_a_block := false
	// Track when we leave the "then" (first) block
	outside_of_then_block := false
	// Used to control when we skip a block
	skip_this_block := false

	// Evaluate whether we will execute the IF-THEN (first) block.  (We dont use a THEN, but thats the saying)
	execute_then_block := true
	if arg_0 == "0" || arg_0 == nil || arg_0 == 0 || arg_0 == false || arg_0 == "" {
		execute_then_block = false

		UdnLog(udn_schema, "If Condition: Not Executing THEN: %s\n", arg_0)
	} else {
		// We will execute the "then" block, so we mark this now, so we skip any ELSE-IF/ELSE blocks
		// Execute A Block, means we should execute at least one
		executed_a_block = true

		UdnLog(udn_schema, "If Condition: Executing THEN: %s\n", arg_0)
	}

	// Variables for looping over functions (flow control)
	udn_current := udn_start

	current_input := input

	// Check the first argument, to see if we should execute the IF-THEN statements, if it is false, we will look for ELSE-IF or ELSE if no ELSE-IF blocks are true.

	// Keep track of any embedded IF statements, as we will need to process or not process them, depending on whether we are currently embedded in other IFs
	embedded_if_count := 0

	//TODO(g): Walk our NextUdnPart until we find our __end_if, then stop, so we can skip everything for now, initial flow control
	for udn_current != nil && (embedded_if_count == 0 && udn_current.Value != "__end_if") && udn_current.NextUdnPart != nil {
		udn_current = udn_current.NextUdnPart

		UdnLog(udn_schema, "Walking IF block: Current: %s   Current Input: %v\n", udn_current.Value, current_input)

		// If we are not executing the THEN block, and we encounter an __if statement, keep track of depth
		if execute_then_block == false && outside_of_then_block == false && udn_current.Value == "__if" {
			embedded_if_count++
		} else if embedded_if_count > 0 {
			// Skip everything until our embedded if is done
			if udn_current.Value == "__end_if" {
				embedded_if_count--
			}
		} else if udn_current.Value == "__else" || udn_current.Value == "__else_if" {
			outside_of_then_block = true
			// Reset this every time we get a new control block start (__else/__else_if), because we havent tested it to be skipped yet
			skip_this_block = false

			if executed_a_block {
				// If we have already executed a block before, then it's time to skip the remaining blocks/parts
				UdnLog(udn_schema, "Found non-main-if block, skipping: %s\n", udn_current.Value)
				break
			} else {
				// Else, we havent executed a block, so we need to determine if we should start executing.  This is only variable for "__else_if", "else" will always execute if we get here
				if udn_current.Value == "__else_if" {
					udn_current_arg_0 := udn_current.Children.Front().Value.(*UdnPart)
					// If we dont have a "true" value, then skip this next block
					if udn_current_arg_0.Value == "0" {
						skip_this_block = true
					} else {
						UdnLog(udn_schema, "Executing Else-If Block: %s\n", udn_current_arg_0.Value)
						// Mark block execution, so we wont do any more
						executed_a_block = true
					}
				} else {
					// This is an "__else", and we made it here, so we are executing the else.  Leaving this here to demonstrate that
					UdnLog(udn_schema, "Executing Else Block\n")
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

					// If we were told what our NextUdnPart is, jump ahead
					if current_result.NextUdnPart != nil {
						UdnLog(udn_schema, "If: Flow Control: JUMPING to NextUdnPart: %s [%s]\n", current_result.NextUdnPart.Value, current_result.NextUdnPart.Id)
						udn_current = current_result.NextUdnPart
					}
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

func UDN_ElseCondition(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Else Condition\n")

	result := UdnResult{}
	result.Result = input

	return result
}

func UDN_ElseIfCondition(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Else If Condition\n")

	result := UdnResult{}
	result.Result = input

	return result
}

func UDN_Not(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, args []interface{}, input interface{}, udn_data map[string]interface{}) UdnResult {
	UdnLog(udn_schema, "Not: %v\n", SnippetData(input, 60))

	value := "0"
	if input != nil && input != "0" {
		value = "1"
	}

	result := UdnResult{}
	result.Result = value

	return result
}

// Parse a UDN string and return a hierarchy under UdnPart
func ParseUdnString(db *sql.DB, udn_schema map[string]interface{}, udn_value_source string) *UdnPart {

	// First Stage
	next_split := _SplitQuotes(db, udn_schema, udn_value_source)

	//UdnLog(udn_schema, "\nSplit: Quotes: AFTER: %v\n\n", next_split)

	next_split = _SplitCompoundStatements(db, udn_schema, next_split)

	//UdnLog(udn_schema, "\nSplit: Compound: AFTER: %v\n\n", next_split)

	next_split = _SplitStatementLists(db, udn_schema, next_split)

	//UdnLog(udn_schema, "\nSplit: List: AFTER: %v\n\n", next_split)

	// Forth Stage
	next_split = _SplitStatementMaps(db, udn_schema, next_split)

	//UdnLog(udn_schema, "\nSplit: Compound: Map: %v\n\n", next_split)

	// Fifth Stage
	next_split = _SplitStatementMapKeyValues(db, udn_schema, next_split)

	//UdnLog(udn_schema, "\nSplit: Compound: Map Key Values: %v\n\n", next_split)

	// Put it into a structure now -- UdnPart
	//
	udn_start := CreateUdnPartsFromSplit_Initial(db, udn_schema, next_split)

	//output := DescribeUdnPart(&udn_start)
	//UdnLog(udn_schema, "\n===== 0 - Description of UDN Part:\n\n%s\n===== 0 - END\n", output)

	// Put it into a structure now -- UdnPart
	//
	FinalParseProcessUdnParts(db, udn_schema, &udn_start)

	output := DescribeUdnPart(&udn_start)
	UdnLog(udn_schema, "\n===== 1 - Description of UDN Part:\n\n%s\n===== 1 - END\n", output)

	return &udn_start
}

// Take the partially created UdnParts, and finalize the parsing, now that it has a hierarchical structure.  Recusive function
func FinalParseProcessUdnParts(db *sql.DB, udn_schema map[string]interface{}, part *UdnPart) {

	//UdnLog(udn_schema, "\n** Final Parse **:  Type: %d   Value: %s   Children: %d  Next: %v\n", part.PartType, part.Value, part.Children.Len(), part.NextUdnPart)

	// If this is a map component, make a new Children list with our Map Keys
	if part.PartType == part_map {
		new_children := list.New()

		//fmt.Printf("\n\nMap Part:\n%s\n\n", DescribeUdnPart(part))

		next_child_is_value := false
		next_child_is_assignment := false

		for child := part.Children.Front(); child != nil; child = child.Next() {
			cur_child := *child.Value.(*UdnPart)

			// If this child isn't the value of the last Map Key, then we are expecting a new Map Key, possibly a value
			if next_child_is_assignment == true {
				// We found the assignment child, so the next child is the value
				next_child_is_assignment = false
				next_child_is_value = true
			} else if next_child_is_value == false {
				map_key_split := strings.Split(cur_child.Value, "=")

				// Create the map key part
				map_key_part := NewUdnPart()
				map_key_part.Value = map_key_split[0]
				map_key_part.PartType = part_map_key
				map_key_part.Depth = part.Depth + 1
				map_key_part.ParentUdnPart = part

				// Add to the new Children
				new_children.PushBack(&map_key_part)

				if len(map_key_split) == 1 {
					// We only had the key, so the next child is the assignment
					next_child_is_assignment = true
				} else if map_key_split[1] == "" {
					// We split on the =, but the next section is empty, so the value is in the next child
					next_child_is_value = true
				} else {
					// Else, we make a new UdnPart from the second half of this split, and add it as a child to a new Map Key.  The key and the value were in a single string...
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
		//UdnLog(udn_schema, "  Compound type!\n\n")
	}

	// If this is a function, remove any children that are for other functions (once other functions start)
	if part.PartType == part_function {
		if part.ParentUdnPart != nil && part.ParentUdnPart.PartType == part_compound {
			// This is a function inside a compound, so dont do what we normally do, as we are already OK!
			//UdnLog(udn_schema, "\nSkipping: Parent is compound: %s\n\n", part.Value)
		} else {
			// Else, this is not a Compound function (Function Argument)
			if part.ParentUdnPart != nil {
				//UdnLog(udn_schema, "\nMap Function: %s  Parent:  %s (%d)\n\n", part.Value, part.ParentUdnPart.Value, part.ParentUdnPart.PartType)
			} else {
				//UdnLog(udn_schema, "\nMap Function: %s  Parent:  NONE\n\n", part.Value)
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

					//UdnLog(udn_schema, "Adding to new_function_list: %s\n", new_udn_function.Value)

				} else if child.Value.(*UdnPart).PartType == part_compound {
					//SKIP: If this is a compount function, we dont need to do anything...
					//UdnLog(udn_schema, "-=-=-= Found Compound!\n -=-=-=-\n")
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

					//UdnLog(udn_schema, "  Adding new function Argument/Child: %s\n", new_udn.Value)
				}
			}

			// Remove these children from the current part.Children
			for child := remove_children.Front(); child != nil; child = child.Next() {

				//UdnLog(udn_schema, "Removing: %v\n", child.Value.(*list.Element).Value)

				_ = part.Children.Remove(child.Value.(*list.Element))
				//removed := part.Children.Remove(child.Value.(*list.Element))
				//UdnLog(udn_schema, "  Removed: %v\n", removed)
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
				//UdnLog(udn_schema, "Moving forward: %s   Next: %v\n", last_udn_part.Value, last_udn_part.NextUdnPart)
			}

			//UdnLog(udn_schema, "Elements in new_function_list: %d\n", new_function_list.Len())

			// Add all the functions to the NextUdnPart, starting from last_udn_part
			for new_function := new_function_list.Front(); new_function != nil; new_function = new_function.Next() {
				// Get the UdnPart for the next function
				add_udn_function := *new_function.Value.(*UdnPart)

				// Set at the next item, and connect parrent
				last_udn_part.NextUdnPart = &add_udn_function
				add_udn_function.ParentUdnPart = last_udn_part

				//UdnLog(udn_schema, "Added NextUdnFunction: %s\n", add_udn_function.Value)

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
	//UdnLog(udn_schema, "UdnPart: Add Function: Parent: %s   Function: %s\n", udn_parent.Value, value)

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
		//UdnLog(udn_schema, "  Starting function: %v\n", start_function)

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
	//UdnLog(udn_schema, "UdnPart: Add Child: Parent: %s   Child: %s (%d)\n", udn_parent.Value, value, part_type)

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

	UdnLog(udn_schema, "Create UDN Parts: Initial: %v\n\n", source_array)

	// Traverse into the data, and start storing everything
	for _, cur_item := range source_array {
		//UdnLog(udn_schema, "  Create UDN Parts: UDN Current: %-20s    Cur Item: %v\n", udn_current.Value, cur_item)

		// If we are in a string, and we are not about to end it, keep appending to the previous element
		if is_open_quote && cur_item != "'" {
			udn_current.Value += cur_item
		} else {
			// We are not in a currently open string, or are about to end it, so do normal processing

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
					//UdnLog(udn_schema, "Create UDN: Function Start: %s\n", cur_item)
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
					udn_current = udn_current.AddChild(part_string, "")
					//UdnLog(udn_schema, "Create UDN: Starting Quoted String\n")
				} else if is_open_quote {
					is_open_quote = false

					// Add single quotes using the HTML Double Quote mechanism, so we can still have single quotes
					udn_current.Value = strings.Replace(udn_current.Value, "&QUOTE;", "'", -1)

					// Reset to before we were a in string
					udn_current = udn_current.ParentUdnPart
					//UdnLog(udn_schema, "Create UDN: Closing Quoted String\n")
				}
			} else if cur_item == "(" {
				//UdnLog(udn_schema, "Create UDN: Starting Compound\n")

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
				//UdnLog(udn_schema, "Create UDN: Closing Compound\n")

				// Walk backwards until we are done
				done := false
				for done == false {
					if udn_current.ParentUdnPart == nil {
						// If we have no more parents, we are done because there is nothing left to come back from
						//TODO(g): This could be invalid grammar, need to test for that (extra closing sigils)
						done = true
						//UdnLog(udn_schema, "COMPOUND: No more parents, finished\n")
					} else if udn_current.PartType == part_compound {
						// Else, if we are already currently on the map, just move off once
						udn_current = udn_current.ParentUdnPart

						done = true
						//UdnLog(udn_schema, "COMPOUND: Moved out of the Compound\n")
					} else {
						//UdnLog(udn_schema, "COMPOUND: Updating UdnPart to part: %v --> %v\n", udn_current, *udn_current.ParentUdnPart)
						udn_current = udn_current.ParentUdnPart
						//UdnLog(udn_schema, "  Walking Up the Compound:  Depth: %d\n", udn_current.Depth)
					}

				}
			} else if cur_item == "[" {
				// Make this list current, so we continue to add into it, until it closes
				udn_current = udn_current.AddChild(part_list, cur_item)

			} else if cur_item == "]" {
				//UdnLog(udn_schema, "Create UDN: Closing List\n")

				// Walk backwards until we are done
				done := false
				for done == false {
					if udn_current.ParentUdnPart == nil {
						// If we have no more parents, we are done because there is nothing left to come back from
						//TODO(g): This could be invalid grammar, need to test for that (extra closing sigils)
						done = true
						//UdnLog(udn_schema, "LIST: No more parents, finished\n")
					} else if udn_current.PartType == part_list {
						// Else, if we are already currently on the map, just move off once
						udn_current = udn_current.ParentUdnPart

						done = true
						//UdnLog(udn_schema, "LIST: Moved out of the List\n")
					} else {
						//UdnLog(udn_schema, "LIST: Updating UdnPart to part: %v --> %v\n", udn_current, *udn_current.ParentUdnPart)
						udn_current = udn_current.ParentUdnPart
						//UdnLog(udn_schema, "  Walking Up the List:  Depth: %d\n", udn_current.Depth)
					}

				}
			} else if cur_item == "{" {
				// Make this list current, so we continue to add into it, until it closes
				udn_current = udn_current.AddChild(part_map, cur_item)

			} else if cur_item == "}" {
				//UdnLog(udn_schema, "Create UDN: Closing Map\n")

				// Walk backwards until we are done
				done := false
				for done == false {
					if udn_current.ParentUdnPart == nil {
						// If we have no more parents, we are done because there is nothing left to come back from
						//TODO(g): This could be invalid grammar, need to test for that (extra closing sigils)
						done = true
						UdnLog(udn_schema, "MAP: No more parents, finished\n")
					} else if udn_current.PartType == part_map {
						// Else, if we are already currently on the map, just move off once
						udn_current = udn_current.ParentUdnPart

						done = true
						//UdnLog(udn_schema, "MAP: Moved out of the Map\n")
					} else {
						//UdnLog(udn_schema, "MAP: Updating UdnPart to part: %v --> %v\n", udn_current, *udn_current.ParentUdnPart)
						udn_current = udn_current.ParentUdnPart
						//UdnLog(udn_schema, "  Walking Up the Map:  Depth: %d\n", udn_current.Depth)
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

	}

	//UdnLog(udn_schema, "Finished Create UDN Parts: Initial\n\n")

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

	//UdnLog(udn_schema, "Split: %s  Sep: %s  Result: %s\n", value, separator, final_array)

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
	//UdnLog(udn_schema, "\nSplit: Quotes: %v\n\n", udn_value)

	split_result := _SplitStringAndKeepSeparator(udn_value, "'")

	return split_result
}

// SECOND STAGE: Recursive function, tracked by depth int.  Inserts sequentially into next_processing_udn_list (list[string]), each of the compound nested items, starting with the inner-most first, and then working out, so that all compound statements can be sequentially processed, with the inner-most getting processed before their immediate next-outer layer, which is the proper order
func _SplitCompoundStatements(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//UdnLog(udn_schema, "\nSplit: Compound: %v\n\n", source_array)

	// Split Open Compound
	split_result := _SplitStringArray(source_array, "(")

	// Split Close Compound
	split_result = _SplitStringArray(split_result, ")")

	return split_result
}

// THIRD STAGE: Linear function, iterating over the THIRD STAGE's list[string], list values are collected as argument variables for their respective UDN processing sections
func _SplitStatementLists(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//UdnLog(udn_schema, "\nSplit: Lists: %v\n\n", source_array)

	// Split Open Compound
	split_result := _SplitStringArray(source_array, "[")

	// Split Close Compound
	split_result = _SplitStringArray(split_result, "]")

	return split_result
}

// FOURTH STAGE: Linear function, iterating over the SECOND STAGE's list[string], map values are collected as argument variables for their respective UDN processing sections
func _SplitStatementMaps(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//UdnLog(udn_schema, "\nSplit: Maps: %v\n\n", source_array)

	// Split Open Compound
	split_result := _SplitStringArray(source_array, "{")

	// Split Close Compound
	split_result = _SplitStringArray(split_result, "}")

	return split_result
}

// FIFTH STAGE: Linear function, iterating over the THIRD STAGE's list[string], list values are collected as argument variables for their respective UDN processing sections
func _SplitStatementMapKeyValues(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//UdnLog(udn_schema, "\nSplit: Map Key Values: %v\n\n", source_array)

	return source_array
}

// SIXTH STAGE: Linear function, iterating over the FIRST STAGE's list[string] sequence of compound-nested-items.  This populates a new list[string] which now includes the split items at each compound-layer, which means we have a full set of UDN statements that will be processed at the end of this function
func _SplitStatementItems(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//UdnLog(udn_schema, "\nSplit: Items: %v\n\n", source_array)

	// Split Open Compound
	split_result := _SplitStringArray(source_array, ".")

	return split_result
}

// SEVENTH STAGE: Linear function, iterating over the THIRD STAGE's list[string], list values are collected as argument variables for their respective UDN processing sections
func _DepthTagList(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
	//UdnLog(udn_schema, "\nSplit: Lists: %v\n\n", source_array)

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
