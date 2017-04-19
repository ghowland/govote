package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/jcasts/gosrv"
	//_ "github.com/mattn/go-sqlite3"
	_ "github.com/lib/pq"
	//"io"
	"github.com/junhsieh/goexamples/fieldbinding/fieldbinding"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
	"container/list"
	//"github.com/jacksontj/goUDN"
	//"container/list"
	//  "net/url"
	//  "io"
	//  "bytes"
	//  "path"
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
	part_unknown = iota
	part_function = iota
	part_item = iota
	part_string = iota
	part_compound = iota
	part_list = iota
	part_map = iota
	part_map_key = iota
)

func NewUdnPart() UdnPart {
	return UdnPart{
		Children: list.New(),
	}
}

type UdnPart struct {
	Depth int
	PartType int

	Value string

	// List of UdnPart structs, list is easier to use dynamically
	Children *list.List

	// Puts the data here after it's been evaluated
	ValueFinal interface{}
	ValueFinalType int

	// Allows casting the type, not sure about this, but seems useful to cast ints from strings for indexing.  We'll see
	CastValue string

	ParentUdnPart *UdnPart
	NextUdnPart *UdnPart
}

type UdnResult struct {
	// This is the result
	Result interface{}

	// This is the next UdnPart to process.  If nil, the executor will just continue from current UdnPart.NextUdnPart
	NextUdnPart *UdnPart

	// Error messages, we will stop processing if not nil
	Error string
}

type UdnFunc func(db *sql.DB, udn_start *UdnPart, arguments list.List, input UdnResult) UdnResult

var UdnFunctions = map[string]UdnFunc {
	"__query": UDN_QueryById,
	"__debug_output": UDN_DebugOutput,
	"__test": UDN_Test,
	"__if": UDN_IfCondition,
	"__else": UDN_ElseCondition,
	"__else_if": UDN_ElseIfCondition,
	"__end_if": nil,
	"__end_else": nil,
	"__end_else_if": nil,
}

func DescribeUdnPart(part *UdnPart) string {

	depth_margin := strings.Repeat("  ", part.Depth)

	output := fmt.Sprintf("%sType: %d\n", depth_margin, part.PartType)
	output += fmt.Sprintf("%sValue: '%s'\n", depth_margin, part.Value)
	//output += fmt.Sprintf("%sDepth: %d\n", depth_margin, part.Depth)

	if part.Children.Len() > 0 {
		output += fmt.Sprintf("%sChildren: %d\n", depth_margin, part.Children.Len())
		for child := part.Children.Front(); child != nil; child = child.Next() {
			output += DescribeUdnPart(child.Value.(*UdnPart))
		}
	}

	if part.NextUdnPart != nil {
		output += fmt.Sprintf("%sNext Command:\n", depth_margin)
		output += DescribeUdnPart(part.NextUdnPart)
	}

	output += fmt.Sprintf("\n")

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

func main() {
	TestUdn()

	s, err := gosrv.NewFromFlag()
	if err != nil {
		panic(err)
	}

	s.HandleFunc("/", handler)

	/*
	err = s.ListenAndServe()
	if err != nil {
		panic(err)
	}
	*/
}


func TestUdn() {
	// DB Web
	db_web, err := sql.Open("postgres", "user=postgres dbname=opsdb password='password' host=localhost sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db_web.Close()

	// Test the UDN Processor
	udn_schema := PrepareSchemaUDN(db_web)
	fmt.Printf("\n\nUDN Schema: %v\n\n", udn_schema)


	//udn_source := "__something.[1,2,3].'else.here'.(__more.arg1.arg2.arg3).goes.(here.and).here.{a=5,b=22,k='bob',z=(a.b.c.[a,b,c])}.__if.condition.__output.something.__else.__output.different.__end_else.__end_if"
	//udn_target := "__iterate_list.map.string.__set.user_info.{id=(__data.current.id), name=(__data.current.name)}.__output.(__data.current).__end_iterate"

	udn_source := "__if.1.__query.1.__else.__test.__end_else.__end_if"
	udn_target := "__debug_output"

	//udn_dest := "__iterate.map.string.__dosomething.{arg1=(__data.current.field1), arg2=(__data.current.field2)}"

	udn_data := make(map[string]TextTemplateMap)

	ProcessUDN(db_web, udn_schema, udn_source, udn_target, udn_data)

	fmt.Printf("UDN Result: %v\n\n", udn_data)
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
	//web_site_domain_id := 1



	//// Test the UDN Processor
	//udn_schema := PrepareSchemaUDN(db_web)
	//fmt.Printf("\n\nUDN Schema: %v\n\n", udn_schema)
	//
	//udn_value := "__something.else"
	//
	//udn_data := make(map[string]TextTemplateMap)
	//
	//udn_result := ProcessUDN(db_web, udn_schema, udn_value, "", udn_data)
	//
	//fmt.Printf("UDN Result: %v\n\n", udn_result)



	//TODO(g): Get the web_site_domain from host header

	// Get the path to match from the DB
	sql := fmt.Sprintf("SELECT * FROM web_site WHERE id = %d", web_site_id)
	web_site_result := Query(db_web, sql)

	fmt.Printf("\n\nGetting Web Site Page from URI: %s\n\n", uri)

	// Get the path to match from the DB
	sql = fmt.Sprintf("SELECT * FROM web_site_page WHERE web_site_id = %d AND name = '%s'", web_site_id, SanitizeSQL(uri))
	fmt.Printf("\n\nQuery: %s\n\n", sql)
	web_site_page_result := Query(db_web, sql)
	fmt.Printf("\n\nWeb Page Results: %v\n\n", web_site_page_result)

	// If we found a matching page
	if uri == "/api/save" {
		dynamicPage_API_Save(db_web, db, uri, w, r)
	} else if len(web_site_page_result) > 0 {
		fmt.Printf("\n\nFound Dynamic Page: %v\n\n", web_site_page_result[0])
		dynamePage_RenderWidgets(db_web, db, web_site_result[0], web_site_page_result[0], uri, w, r)
	} else {
		fmt.Printf("\n\nPage not found: 404: %v\n\n", web_site_page_result)

		dynamicPage_404(uri, w, r)
	}
}

func MapListToDict(map_array []TextTemplateMap, key string) *TextTemplateMap {
	// Build a map of all our web site page widgets, so we can
	output_map := NewTextTemplateMap()

	for _, map_item := range map_array {
		output_map.Map[map_item.Map[key].(string)] = map_item
	}

	return output_map
}

func dynamePage_RenderWidgets(db_web *sql.DB, db *sql.DB, web_site TextTemplateMap, web_site_page TextTemplateMap, uri string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	sql := fmt.Sprintf("SELECT * FROM web_site_page_widget WHERE web_site_page_id = %d ORDER BY priority ASC", web_site_page.Map["id"])
	web_site_page_widgets := Query(db_web, sql)

	// Get the base web site widget
	sql = fmt.Sprintf("SELECT * FROM web_site_page_widget WHERE id = %d", web_site_page.Map["base_page_web_site_page_widget_id"])
	base_page_widgets := Query(db_web, sql)

	// If we couldnt find the page, quit
	if len(base_page_widgets) < 1 {
		dynamicPage_404(uri, w, r)
		return
	}

	base_page_widget := base_page_widgets[0]

	// Get the base widget
	sql = fmt.Sprintf("SELECT * FROM web_widget WHERE id = %d", base_page_widget.Map["web_widget_id"])
	base_widgets := Query(db_web, sql)

	base_page_html, err := ioutil.ReadFile(base_widgets[0].Map["path"].(string))
	if err != nil {
		log.Panic(err)
	}

	// Build a map of all our web site page widgets, so we can
	web_site_page_widget_map := MapListToDict(web_site_page_widgets, "name")

	// Put all our widgets into this map
	page_map := NewTextTemplateMap()

	// Loop over the page widgets, and template them
	for _, site_page_widget := range web_site_page_widgets {
		// Skip it if this is the base page, because we
		if site_page_widget.Map["id"] == web_site_page.Map["base_page_web_site_page_widget_id"] {
			continue
		}

		// Get the base widget
		sql = fmt.Sprintf("SELECT * FROM web_widget WHERE id = %d", site_page_widget.Map["web_widget_id"])
		page_widgets := Query(db_web, sql)
		page_widget := page_widgets[0]

		fmt.Printf("Page Widget: %s: %s\n", site_page_widget.Map["name"], page_widget.Map["name"])

		//TODO(g): We need to use the page_map data here too, because we need to template in the sub-widgets.  Think about this after testing it as-is...
		widget_map := NewTextTemplateMap()

		err = json.Unmarshal([]byte(site_page_widget.Map["data_json"].(string)), &widget_map.Map)
		if err != nil {
			log.Panic(err)
		}
		fmt.Println(widget_map.Map)

		for widget_key, widget_value := range widget_map.Map {
			fmt.Printf("\n\nWidget Key: %s:  Value: %v\n\n", widget_key, widget_value)

			switch widget_value.(type) {
			case string:
				// If this is one of our UDN control keys
				if strings.HasPrefix(widget_value.(string), "__") {
					result := ProcessDataUDN(db_web, db, *web_site_page_widget_map, web_site_page, *page_map, page_widget, *widget_map, widget_value.(string))

					widget_map.Map[widget_key] = result
				} else {
					widget_map.Map[widget_key] = fmt.Sprintf("%v", widget_value)
				}
			default:
				widget_map.Map[widget_key] = fmt.Sprintf("%v", widget_value)
			}

		}

		//fmt.Printf("Title: %s\n", widget_map.Map["title"])

		item_html, err := ioutil.ReadFile(page_widget.Map["path"].(string))
		if err != nil {
			log.Panic(err)
		}

		item_template := template.Must(template.New("text").Parse(string(item_html)))

		item := StringFile{}
		err = item_template.Execute(&item, widget_map)
		if err != nil {
			log.Fatal(err)
		}

		// Append to our total forum_list_string
		key := site_page_widget.Map["name"]

		page_map.Map[key.(string)] = item.String
	}

	// Get base page widget items.  These were also processed above, as the base_page_widget was included with the page...
	base_page_widget_map := NewTextTemplateMap()
	err = json.Unmarshal([]byte(base_page_widget.Map["data_json"].(string)), &base_page_widget_map.Map)
	if err != nil {
		log.Panic(err)
	}

	// Add base_page_widget entries to page_map, if they dont already exist
	for key, value := range base_page_widget_map.Map {
		if _, ok := page_map.Map[key]; ok {
			// Pass, already has this value
		} else {
			// Set the value, static text
			page_map.Map[key] = value
		}
	}

	fmt.Println("Rendering base page")

	// Put them into the base page
	base_page_template := template.Must(template.New("text").Parse(string(base_page_html)))

	// Write the base page
	base_page := StringFile{}
	err = base_page_template.Execute(&base_page, page_map)
	if err != nil {
		log.Fatal(err)
	}

	// Write out the final page
	w.Write([]byte(base_page.String))

}

func ProcessDataUDN(db_web *sql.DB, db *sql.DB, web_site_page_widget_map TextTemplateMap, web_site_page TextTemplateMap, page_map TextTemplateMap, page_widget TextTemplateMap, widget_map TextTemplateMap, udn string) interface{} {

	fmt.Printf("\nUDN: %s\n", udn)

	parts := strings.Split(udn, ".")

	var result interface{}

	if parts[0] == "__auth" {
		if parts[1] == "user_name" {
			result = "Geoff Howland"
		} else if parts[1] == "user_label" {
			result = "Voter Prime"
		} else if parts[1] == "user_image" {
			result = "/img/geoff_.jpg"
		} else {
			result = "Unknown"
		}
	} else if parts[0] == "__widget" {

		//TODO(g): This doesnt exist yet, because it hasnt been created yet.  Instead I want to pull it from the UNPROCESSED TEMPLATE, because we are going to process it here!
		result = page_map.Map[parts[1]]

		//fmt.Printf("Widget Initial Result: %v\n\n", result)

		// Get the data from the web page DB, for this widget.  Need to pull out json_data next.
		site_page_widget_data := web_site_page_widget_map.Map[parts[1]].(TextTemplateMap)

		// Get the web_widget from the web DB
		sql := fmt.Sprintf("SELECT * FROM web_widget WHERE id = %d", site_page_widget_data.Map["web_widget_id"])
		web_widget_array := Query(db_web, sql)
		web_widget_data := web_widget_array[0]

		// Store the JSON data in here
		site_page_widget_json_data := NewTextTemplateMap()

		// Decode the JSON data for the widget data
		err := json.Unmarshal([]byte(site_page_widget_data.Map["data_json"].(string)), &site_page_widget_json_data.Map)
		if err != nil {
			log.Panic(err)
		}

		//fmt.Printf("\n\n_____Widget Start_____\nPage Map Values: %v\n\nWidget Map Values: %v\n\nWeb Site Page: %v\n\nSite Page Widgets: %v\n\nWidget JSON Data: %v\n\nSite Page Widget Specific: %v\n\n_____Widget End_____\n\n", page_map.Map, widget_map.Map, web_site_page.Map, web_site_page_widget_map.Map, site_page_widget_json_data.Map, page_widget.Map)

		// If we have a data source specified as a suffixed handler to this widget
		//TODO(g): Generalize this search for all UDN components, for now its OK that its just __widget, as thats where I need it first
		//if (len(parts) > 2 && strings.HasPrefix(parts[1], "__")) {
		if len(parts) > 2 {
			//widget_data := web_site_page.Map[parts[1]]

			// The template come from this:
			widget_template_text := ReadPathData(web_widget_data.Map["path"].(string))

			// Use the above result as the template to loop over
			widget_template := template.Must(template.New("text").Parse(widget_template_text))

			// We will loop over the query results, and populate them with widget_template
			rolling_result := ""

			new_udn := strings.Join(parts[2:], ".")

			fmt.Printf("\n_____\nGetting Widget Data UDN: %s\n\n_____\n", new_udn)

			fmt.Printf("\n\nAbout to process data, widget_map: %v\n\n", widget_map.Map)

			// Process the query, which will return a list of data elements -- We use the widget_data, which was specified in parts[1]
			data_result := ProcessDataUDN(db_web, db, web_site_page_widget_map, web_site_page, page_map, page_widget, widget_map, new_udn).([]TextTemplateMap)
			for _, element := range data_result {
				fmt.Printf("Data Result Element: %v", element)

				// Were going to run the data against what we just run against the widget_template data we are referencing
				widget_map.Map["__row"] = element

				// Get the widget data we were referencing

				// Get the template data that the above widget data will template into

				// Loop over the keys going into this, and proces them as UDN against the __row, so that they give the right data

				// Template the data from all the processed keys

				// Append to the total output

				// Return result

				widget_template_map := NewTextTemplateMap()

				for item_key, item_data := range site_page_widget_json_data.Map {

					// Using the UDN Query above, process the new item_data UDN, to get the widget_template_map key
					widget_template_map.Map[item_key] = ProcessDataUDN(db_web, db, web_site_page_widget_map, web_site_page, page_map, page_widget, element, item_data.(string))

				}

				fmt.Printf("\nWidget Template Text: %s: %s\n\n", udn, widget_template_text)

				fmt.Printf("\n\nWidget Template Map AFTER: %v\n\n", widget_template_map)

				// Perform a new template from our widget_template above, using the element from the data_result
				list_item := StringFile{}
				err := widget_template.Execute(&list_item, widget_template_map)
				if err != nil {
					log.Fatal(err)
				}

				fmt.Printf("Widget Rolling Result: %v\n", list_item.String)

				// Update rolling result, we add in a widget template for each row
				rolling_result += list_item.String
			}

			// The real final result is "result" is the culmination of the rolling_result
			result = rolling_result
		}

	} else if parts[0] == "__query" {
		sql := fmt.Sprintf("SELECT * FROM datasource_query WHERE id = %s", parts[1])

		fmt.Printf("\n_____\nQuery Handler: %s\n\n_____\n", sql)

		rows := Query(db_web, sql)

		//fmt.Printf("\n_____\nQuery: %v\n\nQuery Result Values: %v\n_____\n", sql, rows)

		// Perform the query we just fetched, from the correct DB (not db_web)
		//TODO(g): This should actually be able to talk to multiple databases, as specified by the web_site_datasource_id
		rows = Query(db, rows[0].Map["sql"].(string))

		fmt.Printf("\n_____\nQuery: %v\n\nWidget Map Values: %v\n_____\n", rows[0].Map["sql"], rows)

		//TODO(g): The calling function should call this:  	defer rows.Close()   How do we enforce this?  I assume failure to do this will cause leaks and maybe other problems.
		result = rows

	} else if parts[0] == "__row" {
		//result = fmt.Sprintf("ROW: Not found: %s  --> %v", udn, parts)
		result = widget_map.Map[parts[1]]
		fmt.Printf("__row: %s: %v\n", parts[1], result)
		//fmt.Printf("  Widget Map: %v\n", widget_map)

	} else if parts[0] == "__site_map" {
		new_udn := strings.Join(parts[1:], ".")

		site_map_item := ProcessDataUDN(db_web, db, web_site_page_widget_map, web_site_page, page_map, page_widget, widget_map, new_udn)

		fmt.Printf("Site Map: 1\n")

		// Use the above result as the template to loop over
		site_map_item_template := template.Must(template.New("text").Parse(site_map_item.(string)))

		fmt.Printf("Site Map: 2\n")

		//TODO(g): Get map dynamically
		sql := fmt.Sprintf("SELECT wsmi.*, wsp.name AS url FROM web_site_map_item AS wsmi JOIN web_site_page wsp ON wsp.id = wsmi.web_site_page_id WHERE web_site_map_id = %d", 1)

		rows := Query(db_web, sql)

		rolling_result := ""

		fmt.Printf("Site Map: 3\n")

		for _, row := range rows {
			fmt.Printf("Formating site map item from: %v", row)

			list_item := StringFile{}
			err := site_map_item_template.Execute(&list_item, row)
			if err != nil {
				log.Fatal(err)
			}

			rolling_result += list_item.String
		}

		result = rolling_result

	} else if parts[0] == "__widget_id" {
		fmt.Printf("Generating query: %s\n", parts[1])
		sql := fmt.Sprintf("SELECT * FROM web_widget WHERE id = %s", parts[1])
		fmt.Printf("Generated query: %s\n", sql)

		rows := Query(db_web, sql)

		fmt.Printf("Generated query next: 2\n", sql)
		result = ReadPathData(rows[0].Map["path"].(string))
		fmt.Printf("Generated query next: 3\n", sql)

	} else if parts[0] == "__field" {
		field_name := parts[1]

		new_udn := strings.Join(parts[2:], ".")

		rows := ProcessDataUDN(db_web, db, web_site_page_widget_map, web_site_page, page_map, page_widget, widget_map, new_udn).([]TextTemplateMap)

		fmt.Printf("Field Processor: %s: %v\n", field_name, rows)

		result = rows[0].Map[field_name]

	} else if parts[0] == "__json_rows" {
		new_udn := strings.Join(parts[1:], ".")

		json_data := ProcessDataUDN(db_web, db, web_site_page_widget_map, web_site_page, page_map, page_widget, widget_map, new_udn)

		fmt.Printf("Result of JSON Rows sub-query: %v\n", json_data)

		var json_interface interface{}

		// Decode the JSON data for the widget data
		err := json.Unmarshal([]byte(json_data.(string)), &json_interface)
		if err != nil {
			log.Panic(err)
		}

		row_count := 0

		switch v := json_interface.(type) {
		case []interface{}:
			for _, x := range v {
				//fmt.Printf("Row: %v", x.(map[string]interface{})["email"])
				fmt.Printf("Row: %v\n", x)
				row_count++
			}
		default:
			fmt.Println("No type found")
		}

		// Make enough rows to collect all our JSON rows
		rows := make([]TextTemplateMap, row_count)

		row_count = 0

		switch v := json_interface.(type) {
		case []interface{}:
			for _, x := range v {
				//fmt.Printf("Row: %v", x.(map[string]interface{})["email"])
				fmt.Printf("Row: %v\n", x)

				rows[row_count] = NewTextTemplateMapItem()

				for key, value := range x.(map[string]interface{}) {
					rows[row_count].Map[key] = value
				}

				// If we dont have an "id" key, create one and make it the row number
				if _, ok := rows[row_count].Map["id"]; ok {
					// Pass, already has this value
				} else {
					// Set the value, static text
					rows[row_count].Map["id"] = row_count
				}

				row_count++
			}
		default:
			fmt.Println("No type found")
		}

		// Store the JSON data in here
		//json_data_map := NewTextTemplateMap()

		result = rows

	} else if parts[0] == "__save" {
		db_id, err := strconv.ParseInt(parts[1], 10, 64)
		if err == nil {
			table_name := parts[2]

			row_id, err := strconv.ParseInt(parts[3], 10, 64)
			if err == nil {

				fmt.Printf("Save: %v: %v: %v: %v\n\n", db_id, table_name, row_id, widget_map)

				sql := GenerateSaveSql(db_id, table_name, row_id, widget_map)

				selected_db := GetSelectedDb(db_web, db, db_id)

				//// Assume we are using the non-web DB
				//selected_db := db
				//
				//if db_id == 1 {
				//	selected_db = db_web
				//} else if db_id == 2 {
				//	selected_db = db
				//}

				// Make the query
				_ = Query(selected_db, sql)

			} else {
				fmt.Printf("ERROR:  Not an int: '%v'", parts[3])
			}
		} else {
			fmt.Printf("ERROR:  Not an int: '%v'", parts[1])
		}

	} else if parts[0] == "__delete_confirm" {
		db_id, err := strconv.ParseInt(parts[1], 10, 64)
		if err == nil {
			table_name := parts[2]

			row_id, err := strconv.ParseInt(parts[3], 10, 64)
			if err == nil {

				fmt.Printf("Save: Delete Confirm: %v: %v: %v: %v\n\n", db_id, table_name, row_id, widget_map)

				//TODO(g): Confirm the user has access to delete this record.

				sql := fmt.Sprintf("DELETE FROM %s WHERE id = %d", table_name, row_id)

				selected_db := GetSelectedDb(db_web, db, db_id)

				//// Assume we are using the non-web DB
				//selected_db := db
				//
				//if db_id == 1 {
				//	selected_db = db_web
				//} else if db_id == 2 {
				//	selected_db = db
				//}

				// Make the query
				_ = Query(selected_db, sql)
			}
		}

	} else if parts[0] == "__save_append_json_row" {
		db_id, err := strconv.ParseInt(parts[1], 10, 64)
		if err == nil {
			table_name := parts[2]

			row_id, err := strconv.ParseInt(parts[3], 10, 64)
			if err == nil {
				field_name := parts[4]

				fmt.Printf("Save: Append JSON Row: %v: %v: %v: %v: %v\n\n", db_id, table_name, row_id, field_name, widget_map)

				selected_db := GetSelectedDb(db_web, db, db_id)

				SaveSqlAppendJsonRow(selected_db, db_id, table_name, row_id, field_name, widget_map)
			}
		}

	} else if parts[0] == "__save_update_json_row" {
		db_id, err := strconv.ParseInt(parts[1], 10, 64)
		if err == nil {
			table_name := parts[2]

			row_id, err := strconv.ParseInt(parts[3], 10, 64)
			if err == nil {
				field_name := parts[4]

				json_row_id, err := strconv.ParseInt(parts[5], 10, 64)
				if err == nil {

					fmt.Printf("Save: Append JSON Row: %v: %v: %v: %v: %v: %v\n\n", db_id, table_name, row_id, field_name, json_row_id, widget_map)

					selected_db := GetSelectedDb(db_web, db, db_id)
					SaveSqlUpdateJsonRow(selected_db, db_id, table_name, row_id, field_name, json_row_id, widget_map)
				}
			}
		}

	} else if parts[0] == "__save_delete_json_row" {
		db_id, err := strconv.ParseInt(parts[1], 10, 64)
		if err == nil {
			table_name := parts[2]

			row_id, err := strconv.ParseInt(parts[3], 10, 64)
			if err == nil {
				field_name := parts[4]

				json_row_id, err := strconv.ParseInt(parts[5], 10, 64)
				if err == nil {

					fmt.Printf("Save: Delete JSON Row: %v: %v: %v: %v: %v\n\n", db_id, table_name, row_id, field_name, json_row_id)

					selected_db := GetSelectedDb(db_web, db, db_id)
					SaveSqlDeleteJsonRow(selected_db, db_id, table_name, row_id, field_name, json_row_id)
				}
			}
		}

	} else if strings.HasPrefix(parts[0], "__") {
		result = fmt.Sprintf("Unknown widget: %s == %v", udn, parts)
	} else {
		//result = fmt.Sprintf("Value: %s", udn)
		result = udn
	}

	return result
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

func Query(db *sql.DB, sql string) []TextTemplateMap {
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

func dynamicPage_API_Save(db_web *sql.DB, db *sql.DB, uri string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("\nCouldnt read the HTTP body: %s\n\n", err)
		w.Write([]byte(fmt.Sprintf("{\"error\": \"Could not read HTTP body: %v\"}\n\n", err)))
		return
	}

	fmt.Printf("\nHTTP Body:\n%v\n\n", string(body))

	var http_body_json_data interface{}

	// Decode the row JSON
	err = json.Unmarshal(body, &http_body_json_data)
	if err != nil {
		fmt.Printf("\nCant parse JSON data: %s\n\n", err)
		w.Write([]byte(fmt.Sprintf("{\"error\": \"Could not read Parse JSON data: %v\"}\n\n", err)))
		return
	}

	fmt.Printf("\n\nAPI: Save: JSON Payload: %v\n\n", http_body_json_data)

	// Cast this into something easier to iterate over
	json_data := http_body_json_data.(map[string]interface{})

	// Get all the Save related keys from the API request data
	udn_map := NewTextTemplateMap()

	for key, value := range json_data {
		if strings.HasPrefix(key, "__") {
			udn_map.Map[key] = value
		}
	}

	fmt.Printf("UDN Map: %v\n\n", udn_map.Map)

	//// This is where we will store all the control commands we get from the JSON http body data.  Which widgets we will generate, their ID names, any redirection or whatever.  Control commands here...
	//meta_bundle_map := map[string] TextTemplateMap{}

	// Bundle up the keys into atomic values (maps, etc)
	bundle_map := map[string]TextTemplateMap{}

	// These are commands we need to process
	command_map := NewTextTemplateMapItem()

	// Our results...
	result_map := NewTextTemplateMapItem()

	for key, value := range udn_map.Map {
		// Split it
		parts := strings.Split(key, ".")

		// Determine it's UDN type, and parse it into pieces
		// NOT CORRECT -->>  NOTE --- All of them start like this::   __command.db_id.table_name.row_id.   As long as that is it, then they can be bundled.  No, some have different lengths...

		bundle_key := ""
		field_key := ""

		//__save, __delete_confirm
		if strings.HasPrefix(key, "__save.") || strings.HasPrefix(key, "__delete_confirm.") {
			bundle_key = fmt.Sprintf("%s.%s.%s.%s", parts[0], parts[1], parts[2], parts[3])

			field_key = strings.Join(parts[4:], ".")
		} else if strings.HasPrefix(key, "__save_append_json_row.") {
			bundle_key = fmt.Sprintf("%s.%s.%s.%s.%s", parts[0], parts[1], parts[2], parts[3], parts[4])

			field_key = strings.Join(parts[5:], ".")
		} else if strings.HasPrefix(key, "__save_update_json_row.") || strings.HasPrefix(key, "__save_delete_json_row.") {
			bundle_key = fmt.Sprintf("%s.%s.%s.%s.%s.%s", parts[0], parts[1], parts[2], parts[3], parts[4], parts[5])

			field_key = strings.Join(parts[6:], ".")
		} else if strings.HasPrefix(key, "__render_widget.") || strings.HasPrefix(key, "__redirect_url.") {
			// Save the command, to process it after all our data saving is done
			command_map.Map[key] = value
		} else {
			fmt.Printf("\n\nERROR: Unknown bundle key type: %s\n\n", key)
		}

		// If this bundle key doesnt already exist, create the new map
		if _, ok := bundle_map[bundle_key]; !ok {
			bundle_map[bundle_key] = NewTextTemplateMapItem()
		}

		// Set the remaining data into this map
		bundle_map[bundle_key].Map[field_key] = value
	}

	fmt.Printf("\n\nCreated Bundle Map: %v\n\n", bundle_map)

	// Sort the bundles keys based on any dependencies
	//TODO(g): Not doing this yet, but at some point we will want some new data to refer to other new data, and will need to de-reference the negative (-1) new ID key, and use it in one of the other rows we want to save

	// Process each of the bundle items, saving all the data
	for key, value := range bundle_map {
		fmt.Printf("Processing Bundle: %s = %v\n\n", key, value)

		//TODO(g): Do all the regular table updates before the JSON row updates, as an order of precedence.  Separate for loops.  Can do this later...  Just doing the loop twice and testing for different keys both times.

		_ = ProcessDataUDN(db_web, db, value, value, value, value, value, key)
	}

	// Process each of the bundle items, saving all the data
	for key, value := range command_map.Map {
		fmt.Printf("Processing Command: %s = %v\n\n", key, value)

		data_map := NewTextTemplateMapItem()

		//NOTE(g):
		// Writing data back, because we will be regenerating some of the page elements here...
		// Reirection and other commands are also added here, each time, potentially.  There should be __meta bundle!

		_ = ProcessDataUDN(db_web, db, data_map, data_map, data_map, data_map, data_map, key)
	}

	// Marshal the JSON results
	json_result, err := json.Marshal(result_map.Map)
	if err != nil {
		log.Panic(err)
	}

	// Write the JSON results
	w.Write([]byte(json_result))
}

func GenerateSaveSql(db_id int64, table_name string, row_id int64, data_map TextTemplateMap) string {
	sql := ""

	if row_id >= 0 {
		base_sql := "UPDATE %s SET %s WHERE id = %d"

		set_string := ""

		for key, value := range data_map.Map {
			assignment := ""

			// If this is not a normal key, that we just set the value, there is a meta-command being given
			if strings.HasPrefix(key, "__meta.") {
				parts := strings.Split(key, ".")
				meta_key := parts[1]
				meta_command := parts[2]

				if meta_command == "__increment" {
					assignment = fmt.Sprintf("`%s` = `%s` + %v", meta_key, meta_key, value)
				} else {
					//ERROR or something...  Log...
					fmt.Printf("ERROR: Unknown meta command: %s", meta_command)
				}

			} else {
				// Else, this is just a normal data key.  Not a __meta key
				assignment = fmt.Sprintf("`%s` = '%s'", key, value)
			}

			//TODO(g): How do we handle failures without exceptions raising up the stack?  Its very inconvenient, need to decide on a pattern...
			if len(assignment) != 0 {
				if len(set_string) != 0 {
					set_string = fmt.Sprintf("%s, %s", set_string, assignment)
				} else {
					set_string = assignment
				}
			}
		}

		sql = fmt.Sprintf(base_sql, table_name, set_string, row_id)

	} else {
		base_sql := "INSERT INTO %s (%s) VALUES (%s)"

		field_string := ""
		value_string := ""

		for key, value := range data_map.Map {
			field := ""

			// If this is not a normal key, that we just set the value, there is a meta-command being given
			if strings.HasPrefix(key, "__meta.") {
				parts := strings.Split(key, ".")
				meta_key := parts[1]
				meta_command := parts[2]

				if meta_command == "__increment" {
					field = meta_key
					value = value // Set to itself, positive numberline
				} else {
					//ERROR or something...  Log...
					fmt.Printf("ERROR: Unknown meta command: %s", meta_command)
				}

			} else if key == "id" {
				// Dont put the ID field in, so we skip this one, explicitly setting it empty so it's obvious.  The row should auto-increment
				field = ""
			} else {
				// Else, this is just a normal data key.  Not a __meta key
				field = key
			}

			//TODO(g): How do we handle failures without exceptions raising up the stack?  Its very inconvenient, need to decide on a pattern...
			if len(field) != 0 {
				if len(field_string) != 0 {
					field_string = fmt.Sprintf("%s, %s", field_string, field)
					value_string = fmt.Sprintf("%s, '%v'", value_string, value)
				} else {
					field_string = field
					value_string = fmt.Sprintf("'%v'", value)
				}
			}
		}

		sql = fmt.Sprintf(base_sql, table_name, field_string, value_string)

	}

	fmt.Printf("\nGenerateSaveSql: %s\n\n", sql)

	return sql
}

func SaveSqlAppendJsonRow(db *sql.DB, db_id int64, table_name string, row_id int64, field_name string, data_map TextTemplateMap) bool {
	success := false

	lock_udn := fmt.Sprintf("save_lock.%d.%s.%d", db_id, table_name, row_id)

	// Lock this data, so we are the only one making this change.  This is required because this cannot be fully atomic.
	Lock(lock_udn)
	defer Unlock(lock_udn)

	sql := fmt.Sprintf("SELECT %s FROM %s WHERE id = %d", field_name, table_name, row_id)

	rows := Query(db, sql)

	if len(rows) == 1 {
		row := rows[0]

		var json_map_list interface{}

		// Decode the row JSON
		err := json.Unmarshal([]byte(row.Map[field_name].(string)), &json_map_list)
		if err != nil {
			log.Panic(err)
		}

		//// Add in our own keys -- This is for update...
		//for key, value := range data_map.Map {
		//	json_row.Map[key] = value
		//}

		// Determine the json_row_id of our new appended row, so it can be updated and referenced
		json_row_id := float64(len(json_map_list.([]interface{})) + 1)

		// Try to get the last record, if it has a "id" field, increment that one
		if len(json_map_list.([]interface{})) >= 1 {
			last_row := json_map_list.([]interface{})[len(json_map_list.([]interface{}))-1].(map[string]interface{})

			// If the last row has an "id" field, then increment it and use that as our new json_row_id
			if _, ok := last_row["id"]; ok {
				json_row_id = last_row["id"].(float64) + 1.0
			}
		}

		// Add the row ID
		data_map.Map["id"] = json_row_id

		// Append our data_map to the JSON list of maps
		json_map_list_after := append(json_map_list.([]interface{}), data_map.Map)

		// Serialize as JSON string
		json_updated, err := json.Marshal(json_map_list_after)
		if err != nil {
			log.Panic(err)
		}

		// Update the field with this data
		sql = fmt.Sprintf("UPDATE %s SET %s = '%s' WHERE id = %d", table_name, field_name, json_updated, row_id)

		fmt.Printf("Update JSON Row SQL: %s: %s\n\n", lock_udn, sql)

		Query(db, sql)

	} else {
		fmt.Printf("ERROR: Couldnt find the row to append too: %s\n\n", lock_udn)
	}

	return success
}

func SaveSqlUpdateJsonRow(db *sql.DB, db_id int64, table_name string, row_id int64, field_name string, json_row_id int64, data_map TextTemplateMap) bool {
	success := false

	lock_udn := fmt.Sprintf("save_lock.%d.%s.%d", db_id, table_name, row_id)

	// Lock this data, so we are the only one making this change.  This is required because this cannot be fully atomic.
	Lock(lock_udn)
	defer Unlock(lock_udn)

	sql := fmt.Sprintf("SELECT %s FROM %s WHERE id = %d", field_name, table_name, row_id)

	rows := Query(db, sql)

	if len(rows) == 1 {
		row := rows[0]

		var json_map_list interface{}

		// Decode the row JSON
		err := json.Unmarshal([]byte(row.Map[field_name].(string)), &json_map_list)
		if err != nil {
			log.Panic(err)
		}

		// Loop over the json_map_list, and find the record we specified (id=row_id)
		found_record := false
		for _, row := range json_map_list.([]interface{}) {
			item := row.(map[string]interface{})

			if _, ok := item["id"]; ok {

				fmt.Printf("Comparing: %s == %s\n", int64(item["id"].(float64)), json_row_id)

				if int64(item["id"].(float64)) == json_row_id {
					// Update the JSON map from our data_map values (except "id" field, which shouldnt ever be touched)
					for key, value := range data_map.Map {
						if key != "id" {
							item[key] = value
							fmt.Printf("Updating JSON key: %s: %s: %s: %s :%s", lock_udn, field_name, json_row_id, key, value)
						}
					}

					found_record = true

					// We did what we were looping for
					break
				}
			}
		}

		if found_record {
			// Serialize as JSON string again
			json_updated, err := json.Marshal(json_map_list)
			if err != nil {
				log.Panic(err)
			}

			// Update the field with this data
			sql = fmt.Sprintf("UPDATE %s SET %s = '%s' WHERE id = %d", table_name, field_name, json_updated, row_id)

			fmt.Printf("Update JSON Row SQL: %s: %s\n\n", lock_udn, sql)

			Query(db, sql)

		} else {
			fmt.Printf("ERROR: Couldnt find specified JSON row: %s: %s: %s\n\n", lock_udn, field_name, json_row_id)
		}

	} else {
		fmt.Printf("ERROR: Couldnt find the row to append too: %s\n\n", lock_udn)
	}

	return success
}

func SaveSqlDeleteJsonRow(db *sql.DB, db_id int64, table_name string, row_id int64, field_name string, json_row_id int64) bool {
	success := false

	lock_udn := fmt.Sprintf("save_lock.%d.%s.%d", db_id, table_name, row_id)

	// Lock this data, so we are the only one making this change.  This is required because this cannot be fully atomic.
	Lock(lock_udn)
	defer Unlock(lock_udn)

	sql := fmt.Sprintf("SELECT %s FROM %s WHERE id = %d", field_name, table_name, row_id)

	rows := Query(db, sql)

	if len(rows) == 1 {
		row := rows[0]

		var json_map_list interface{}

		// Decode the row JSON
		err := json.Unmarshal([]byte(row.Map[field_name].(string)), &json_map_list)
		if err != nil {
			log.Panic(err)
		}

		// Loop over the json_map_list, and find the record we specified (id=row_id)
		found_record := false
		row_count := 0
		for _, row := range json_map_list.([]interface{}) {
			item := row.(map[string]interface{})

			if _, ok := item["id"]; ok {

				fmt.Printf("Comparing: %s == %s\n", int64(item["id"].(float64)), json_row_id)

				if int64(item["id"].(float64)) == json_row_id {
					found_record = true
					break
				}
			}

			row_count++
		}

		if found_record {
			// Remove this record from the array
			json_map_list = append(json_map_list.([]interface{})[:row_count], json_map_list.([]interface{})[row_count+1:]...)

			// Serialize as JSON string again
			json_updated, err := json.Marshal(json_map_list)
			if err != nil {
				log.Panic(err)
			}

			// Update the field with this data
			sql = fmt.Sprintf("UPDATE %s SET %s = '%s' WHERE id = %d", table_name, field_name, json_updated, row_id)

			fmt.Printf("Update JSON Row SQL: %s: %s\n\n", lock_udn, sql)

			Query(db, sql)

		} else {
			fmt.Printf("ERROR: Couldnt find specified JSON row: %s: %s: %s\n\n", lock_udn, field_name, json_row_id)
		}

	} else {
		fmt.Printf("ERROR: Couldnt find the row to append too: %s\n\n", lock_udn)
	}

	return success
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


// Prepare UDN processing from schema specification -- Returns all the data structures we need to parse UDN properly
func PrepareSchemaUDN(db *sql.DB) map[string]interface{} {
	// Config
	sql := "SELECT * FROM udn_config ORDER BY name"
	
	result := Query(db, sql)
	
	//udn_config_map := make(map[string]string)
	//udn_config_map := make(map[string]TextTemplateMap)
	udn_config_map := NewTextTemplateMap()
	//udn_map := NewTextTemplateMap()

	
	// Add base_page_widget entries to page_map, if they dont already exist
	for _, value := range result {
		fmt.Printf("UDN Config: %s = \"%s\"\n", value.Map["name"], value.Map["sigil"])
		
		// Save the config value and sigil
		//udn_config_map[string(value.Map["name"].(string))] = string(value.Map["sigil"].(string))
		
		// Create the TextTemplateMap
		udn_config_map.Map[string(value.Map["name"].(string))] = string(value.Map["sigil"].(string))
	}

	fmt.Printf("udn_config_map: %v\n", udn_config_map)

	
	// Function
	sql = "SELECT * FROM udn_function ORDER BY name"
	
	result = Query(db, sql)
	
	udn_function_map := make(map[string]string)
	udn_function_id_alias_map := make(map[int64]string)
	udn_function_id_function_map := make(map[int64]string)
	
	// Add base_page_widget entries to page_map, if they dont already exist
	for _, value := range result {
		fmt.Printf("UDN Function: %s = \"%s\"\n", value.Map["alias"], value.Map["function"])
		
		// Save the config value and sigil
		udn_function_map[string(value.Map["alias"].(string))] = string(value.Map["function"].(string))
		udn_function_id_alias_map[value.Map["id"].(int64)] = string(value.Map["alias"].(string))
		udn_function_id_function_map[value.Map["id"].(int64)] = string(value.Map["function"].(string))
	}

	fmt.Printf("udn_function_map: %v\n", udn_function_map)
	fmt.Printf("udn_function_id_alias_map: %v\n", udn_function_id_alias_map)
	fmt.Printf("udn_function_id_function_map: %v\n", udn_function_id_function_map)


	// Group
	sql = "SELECT * FROM udn_group ORDER BY name"
	
	result = Query(db, sql)
	
	udn_group_map := make(map[string]*TextTemplateMap)
	
	// Add base_page_widget entries to page_map, if they dont already exist
	for _, value := range result {
		fmt.Printf("UDN Group: %s = Start: \"%s\"  End: \"%s\"  Is Key Value: %v\n", value.Map["name"], value.Map["sigil"])
		
		
		udn_group_map[string(value.Map["name"].(string))] = NewTextTemplateMap()
		
		// Save the config value and sigil
		for map_key, map_value := range value.Map {
			fmt.Printf("Map Key: %v  Map Value: %v\n", map_key, map_value)
			
			//udn_group_map[string(value.Map["name"].(string))].Map[map_key] = string(map_value.(string))
		}
	}

	fmt.Printf("udn_group_map: %v\n", udn_group_map)
	
	
	// Pack a result map for return
	result_map := make(map[string]interface{})
	
	result_map["function_map"] = udn_function_map
	result_map["function_id_alias_map"] = udn_function_id_alias_map
	result_map["function_id_function_map"] = udn_function_id_function_map
	result_map["group_map"] = udn_group_map
	result_map["config_map"] = udn_config_map
	
	return result_map
}


// Pass in a UDN string to be processed - Takes function map, and UDN schema data and other things as input, as it works stand-alone from the application it supports
func ProcessUDN(db *sql.DB, udn_schema map[string]interface{}, udn_value_source string, udn_value_target string, udn_data map[string]TextTemplateMap) {
	fmt.Printf("\n\nProcess UDN: %s: %v\n\n", udn_value_source, udn_data)

	udn_source := ParseUdnString(db, udn_schema, udn_value_source)
	udn_target := ParseUdnString(db, udn_schema, udn_value_target)

	output_source := DescribeUdnPart(udn_source)
	output_target := DescribeUdnPart(udn_target)

	fmt.Printf("\nDescription of UDN Source: %s\n\n%s\n", udn_value_source, output_source)
	fmt.Printf("\nDescription of UDN Target: %s\n\n%s\n", udn_value_target, output_target)

	source_input := UdnResult{}

	// Execute the Source UDN
	source_result := ExecuteUdn(db, udn_schema, udn_source, source_input, udn_data)

	target_input := UdnResult{}
	target_input.Result = source_result

	fmt.Printf("UDN Source result: %v\n", source_result)

	// Execute the Target UDN
	ExecuteUdn(db, udn_schema, udn_target, target_input, udn_data)
}


// Execute a single UDN (Soure or Target) and return the result
func ExecuteUdn(db *sql.DB, udn_schema map[string]interface{}, udn_start *UdnPart, input UdnResult, udn_data map[string]TextTemplateMap) UdnResult {
	fmt.Printf("Executing: %s\n\n", udn_start.Value)

	args := list.List{} // make this work, get from our children, execute any children as functions

	// Process all our arguments, Executing any functions, at all depths.  Furthest depth first, to meet dependencies

	// In case the function is nil, just pass through the input as the result.  Setting it here because we need this declared in function-scope
	udn_result := UdnResult{}

	// If this is a real function (not an end-block nil function)
	if UdnFunctions[udn_start.Value] != nil {
		udn_result = UdnFunctions[udn_start.Value](db, udn_start, args, input)

		// If we have more to process, do it
		if udn_result.NextUdnPart != nil {
			// Our result gave us a NextUdnPart, so we can assume they performed some execution flow control themeselves, we will continue where they told us to
			udn_result = ExecuteUdn(db, udn_schema, udn_result.NextUdnPart, udn_result, udn_data)
		} else if udn_start.NextUdnPart != nil {
			// We have a NextUdnPart and we didnt recieve a different NextUdnPart from our udn_result, so execute sequentially
			udn_result = ExecuteUdn(db, udn_schema, udn_start.NextUdnPart, udn_result, udn_data)
		}
	} else {
		// Set the result to our input, because we got a nil-function, which doesnt change the result
		udn_result = input
	}

	return udn_result
}



func UDN_QueryById(db *sql.DB, udn_start *UdnPart, arguments list.List, input UdnResult) UdnResult {
	result := UdnResult{}

	result.Result = Query(db, "SELECT * FROM datasource_query")

	return result
}

func UDN_DebugOutput(db *sql.DB, udn_start *UdnPart, arguments list.List, input UdnResult) UdnResult {
	result := UdnResult{}

	fmt.Printf("Debug Output: %v\n", input.Result)

	return result
}

func UDN_Test(db *sql.DB, udn_start *UdnPart, arguments list.List, input UdnResult) UdnResult {
	fmt.Printf("Test Function!!!\n")

	return input
}

func UDN_IfCondition(db *sql.DB, udn_start *UdnPart, arguments list.List, input UdnResult) UdnResult {
	fmt.Printf("If Condition\n")

	return input
}

func UDN_ElseCondition(db *sql.DB, udn_start *UdnPart, arguments list.List, input UdnResult) UdnResult {
	fmt.Printf("Else Condition\n")

	return input
}

func UDN_ElseIfCondition(db *sql.DB, udn_start *UdnPart, arguments list.List, input UdnResult) UdnResult {
	fmt.Printf("Else If Condition\n")

	return input
}



// Parse a UDN string and return a hierarchy under UdnPart
func ParseUdnString(db *sql.DB, udn_schema map[string]interface{}, udn_value_source string) *UdnPart {

	// First Stage
	next_split := _SplitQuotes(db, udn_schema, udn_value_source)

	fmt.Printf("\nSplit: Quotes: AFTER: %v\n\n", next_split)

	next_split = _SplitCompountStatements(db, udn_schema, next_split)

	fmt.Printf("\nSplit: Compound: AFTER: %v\n\n", next_split)

	next_split = _SplitStatementLists(db, udn_schema, next_split)

	fmt.Printf("\nSplit: List: AFTER: %v\n\n", next_split)

	// Forth Stage
	next_split = _SplitStatementMaps(db, udn_schema, next_split)

	fmt.Printf("\nSplit: Compound: Map: %v\n\n", next_split)

	// Fifth Stage
	next_split = _SplitStatementMapKeyValues(db, udn_schema, next_split)

	fmt.Printf("\nSplit: Compound: Map Key Values: %v\n\n", next_split)

	// Put it into a structure now -- UdnPart
	//
	udn_start := CreateUdnPartsFromSplit_Initial(db, udn_schema, next_split)

	//output := DescribeUdnPart(udn_start)
	//
	//fmt.Printf("\nDescription of UDN Part:\n\n%s\n", output)

	// Put it into a structure now -- UdnPart
	//
	FinalParseProcessUdnParts(db, udn_schema, &udn_start)

	CreateCodeBlocksFromUdnParts(db, udn_schema, &udn_start)

	//output := DescribeUdnPart(udn_start)
	//
	//fmt.Printf("\nDescription of UDN Part:\n\n%s\n", output)

	// Load it into a UdnPart, as we go.  This will auto-depth tag and stuff, as we walk.  Above this, it's safe to do.
	//

	// All the above sections, and the below sections can be done in a generalized way.
	// 		Just loop over the DB and split in order, one of them can happen after the UdnPart loading happens.
	//

	// How do the lists and maps and such work in the middle of a UDN thing?
	//

	// Need to look into this.  As a selection it can make sense as options, so filtering, basically.
	//		As a set target, maybe it doesn't make sense.  How to put them into all of them, same filter system?  Unlikely.  Should be uniform.
	//		They make the most sense as pulling different data together and setting it into a target.
	//		List and dicts inside of other things is kinda weird, but does make sense as selection/filtering.
	//
	// 		Also options/arguments to functions.  This makes the most sense.  Passing in lists and such into functions makes sense.
	//		This could also be used for Dest?  Not sure, unless its going to return into a target.  If Dest doesn't turn into a target (no data), its a failure to validate the Dest.
	//

	//type UdnPart struct {
	//	Depth int
	//	PartType int
	//	Value string
	//	Children []UdnPart
	//	// Allows casting the type, not sure about this, but seems useful to cast ints from strings for indexing.  We'll see
	//	CastValue string
	//}


	// Split commas, if it isnt a quote, and it is in a dict or list
	//

	// Split equals, if it isnt a quote, and it is in a dict
	//

	// Sixth Stage
	//next_split := _SplitStatementItems(db, udn_schema, first_stage_udn_list, next_split)

	// Seventh Stage
	//_DepthTagList(db, udn_schema, next_split)

	return &udn_start
}



// Find any code block functions, and embedded them, so we can handle their custom execution control (if/iterate/switch/etc)
func CreateCodeBlocksFromUdnParts(db *sql.DB, udn_schema map[string]interface{}, part *UdnPart) {

}


// Take the partially created UdnParts, and finalize the parsing, now that it has a hierarchical structure.  Recusive function
func FinalParseProcessUdnParts(db *sql.DB, udn_schema map[string]interface{}, part *UdnPart) {

	fmt.Printf("Type: %d   Value: %s   Children: %d\n", part.PartType, part.Value, part.Children.Len())


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
	if part.PartType == part_function {
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

				new_function_list.PushBack(&new_udn_function)
				remove_children.PushBack(child)

				cur_udn_function = new_udn_function

				fmt.Printf("Adding to new_function_list: %s\n", new_udn_function.Value)

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

				fmt.Printf("  Adding new function Argument/Child: %s\n", new_udn.Value)
			}
		}


		// Remove these children from the current part.Children
		for child := remove_children.Front(); child != nil; child = child.Next() {

			fmt.Printf("Removing: %v\n", child.Value.(*list.Element).Value)

			removed := part.Children.Remove(child.Value.(*list.Element))
			fmt.Printf("  Removed: %v\n", removed)
		}



		// Find the last UdnPart, that doesnt have a NextUdnPart, so we can add all the functions onto this
		last_udn_part := part
		for last_udn_part.NextUdnPart != nil {
			last_udn_part = last_udn_part.NextUdnPart
			fmt.Printf("Moving forward: %s   Next: %v\n", last_udn_part.Value, last_udn_part.NextUdnPart)
		}

		fmt.Printf("Elements in new_function_list: %d\n", new_function_list.Len())


		// Add all the functions to the NextUdnPart, starting from last_udn_part
		for new_function := new_function_list.Front(); new_function != nil; new_function = new_function.Next() {
			// Get the UdnPart for the next function
			add_udn_function := *new_function.Value.(*UdnPart)

			// Set at the next item, and connect parrent
			last_udn_part.NextUdnPart = &add_udn_function
			add_udn_function.ParentUdnPart = last_udn_part

			fmt.Printf("Added NextUdnFunction: %s\n", add_udn_function.Value)

			// Update our new last UdnPart, which continues the Next trail
			last_udn_part = &add_udn_function
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

// Take partially split text, and start putting it into the structure we need
func CreateUdnPartsFromSplit_Initial(db *sql.DB, udn_schema map[string]interface{}, source_array []string) UdnPart {
	udn_start := NewUdnPart()
	udn_current := &udn_start

	// We start at depth zero, and descend with sub-statements, lists, maps, etc
	udn_current.Depth = 0

	is_open_quote := false

	// Traverse into the data, and start storing everything
	for _, cur_item := range source_array {
		// If this is a Underscore, make a new piece, unless this is the first one
		if strings.HasPrefix(cur_item, "__") {

			// Split any dots that may be connected to this still (we dont split on them before this), so we do it here and the part_item test, to complete that
			dot_split_array := strings.Split(cur_item, ".")

			if udn_current.PartType == part_unknown {
				fmt.Printf("Create UDN: Function Start: %s\n", cur_item)
				// If this is the first function, tag the part type
				udn_current.PartType = part_function

				udn_current.Value = dot_split_array[0]
			} else {
				fmt.Printf("Create UDN: Additional Function Start: %s   Parent: %s\n", cur_item, udn_current.Value)
				// Else, this is not the first function, so create a new function at this label/depth, and add it in, setting it as the current, so we chain them
				new_udn := NewUdnPart()
				new_udn.Value = dot_split_array[0]
				new_udn.Depth = udn_current.Depth + 1
				new_udn.PartType = part_function

				// Set up parent child relationship
				udn_current.NextUdnPart = &new_udn
				new_udn.ParentUdnPart = udn_current
				//fmt.Printf("Setting New UDN Parent: %v   Parent: %v\n", new_udn, udn_current)


				// Go to the next UDN, at this level.  Should the depth change?
				udn_current = &new_udn
			}

			// Add any of the remaining dot_split_array as children
			for dot_count, doc_split_child := range dot_split_array {
				// Skip the 1st element, which is the function name we stored above
				if dot_count >= 1 {
					if doc_split_child != "" {
						// Sub-statement.  UDN inside UDN, process these first, by depth, but initially parse them into place
						new_udn := NewUdnPart()
						new_udn.ParentUdnPart = udn_current
						//fmt.Printf("Setting New UDN Parent: %v   Parent: %v\n", new_udn, udn_current)

						new_udn.Depth = udn_current.Depth + 1

						new_udn.PartType = part_item
						new_udn.Value = doc_split_child

						// Add to current chilidren
						udn_current.Children.PushBack(&new_udn)

						fmt.Printf("Create UDN: Add Child Element: %s    Adding to: %s\n", doc_split_child, udn_current.Value)
					}
				}
			}

		} else if cur_item == "'" {
			// Enable and disable our quoting, it is simple enough we can just start/stop it.  Lists, maps, and subs cant be done this way.
			if !is_open_quote {
				is_open_quote = true
				fmt.Printf("Create UDN: Starting Quoted String\n")
			} else if is_open_quote {
				is_open_quote = false
				fmt.Printf("Create UDN: Closing Quoted String\n")
			}
		} else if is_open_quote {
			// Add this quoted string into the children position, with a new UdnPart
			new_udn := NewUdnPart()

			new_udn.Depth = udn_current.Depth + 1
			new_udn.PartType = part_string
			new_udn.Value = cur_item
			new_udn.ValueFinal = cur_item

			udn_current.Children.PushBack(&new_udn)

			fmt.Printf("Create UDN: Added Quoted String: %s\n", cur_item)

		} else if cur_item == "(" {
			fmt.Printf("Create UDN: Starting Compound\n")
			// Sub-statement.  UDN inside UDN, process these first, by depth, but initially parse them into place
			new_udn := NewUdnPart()
			new_udn.Value = cur_item
			new_udn.PartType = part_compound
			new_udn.ParentUdnPart = udn_current
			//fmt.Printf("Setting New UDN Parent: %v   Parent: %v\n", new_udn, udn_current)

			new_udn.Depth = udn_current.Depth + 1

			// Add to current chilidren
			udn_current.Children.PushBack(&new_udn)

			// Make this current, so we add into it
			udn_current = &new_udn

		} else if cur_item == ")" {
			fmt.Printf("Create UDN: Closing Compound\n")

			// Walk backwards until we are done
			done := false
			for done == false {
				if udn_current.ParentUdnPart == nil {
					// If we have no more parents, we are done because there is nothing left to come back from
					//TODO(g): This could be invalid grammar, need to test for that (extra closing sigils)
					done = true
					fmt.Printf("COMPOUND: No more parents, finished\n")
				} else {
					fmt.Printf("COMPOUND: Updating UdnPart to part: %v --> %v\n", udn_current, *udn_current.ParentUdnPart)
					udn_current = udn_current.ParentUdnPart
					if udn_current.PartType == part_compound {
						// One more parent, as this is the top level of the Compound, which we are closing now
						udn_current = udn_current.ParentUdnPart

						done = true
						fmt.Printf("COMPOUND: Moved out of the Compound\n")
					} else {
						fmt.Printf("  Walking Up the Compound:  Depth: %d\n", udn_current.Depth)
					}
				}

			}
		} else if cur_item == "[" {
			fmt.Printf("Create UDN: Starting List\n")
			// Sub-statement.  UDN inside UDN, process these first, by depth, but initially parse them into place
			new_udn := NewUdnPart()
			new_udn.Value = cur_item
			new_udn.PartType = part_list
			new_udn.ParentUdnPart = udn_current
			//fmt.Printf("Setting New UDN Parent: %v   Parent: %v\n", new_udn, udn_current)

			new_udn.Depth = udn_current.Depth + 1

			// Add to current chilidren
			udn_current.Children.PushBack(&new_udn)

			// Make this current, so we add into it
			udn_current = &new_udn

		} else if cur_item == "]" {
			fmt.Printf("Create UDN: Closing List\n")

			// Walk backwards until we are done
			done := false
			for done == false {
				if udn_current.ParentUdnPart == nil {
					// If we have no more parents, we are done because there is nothing left to come back from
					//TODO(g): This could be invalid grammar, need to test for that (extra closing sigils)
					done = true
					fmt.Printf("LIST: No more parents, finished\n")
				} else {
					fmt.Printf("LIST: Updating UdnPart to part: %v --> %v\n", udn_current, *udn_current.ParentUdnPart)
					udn_current = udn_current.ParentUdnPart
					if udn_current.PartType == part_list {
						// One more parent, as this is the top level of the Compound, which we are closing now
						udn_current = udn_current.ParentUdnPart

						done = true
						fmt.Printf("LIST: Moved out of the List\n")
					} else {
						fmt.Printf("  Walking Up the List:  Depth: %d\n", udn_current.Depth)
					}
				}

			}
		} else if cur_item == "{" {
			fmt.Printf("Create UDN: Starting Map\n")
			// Sub-statement.  UDN inside UDN, process these first, by depth, but initially parse them into place
			new_udn := NewUdnPart()
			new_udn.Value = cur_item
			new_udn.PartType = part_map
			new_udn.ParentUdnPart = udn_current
			//fmt.Printf("Setting New UDN Parent: %v   Parent: %v\n", new_udn, udn_current)

			new_udn.Depth = udn_current.Depth + 1

			// Add to current chilidren
			udn_current.Children.PushBack(&new_udn)

			// Make this current, so we add into it
			udn_current = &new_udn

		} else if cur_item == "}" {
			fmt.Printf("Create UDN: Closing Map\n")

			// Walk backwards until we are done
			done := false
			for done == false {
				if udn_current.ParentUdnPart == nil {
					// If we have no more parents, we are done because there is nothing left to come back from
					//TODO(g): This could be invalid grammar, need to test for that (extra closing sigils)
					done = true
					fmt.Printf("MAP: No more parents, finished\n")
				} else {
					fmt.Printf("MAP: Updating UdnPart to part: %v --> %v\n", udn_current, *udn_current.ParentUdnPart)
					udn_current = udn_current.ParentUdnPart
					if udn_current.PartType == part_map {
						// One more parent, as this is the top level of the Compound, which we are closing now
						udn_current = udn_current.ParentUdnPart

						done = true
						fmt.Printf("MAP: Moved out of the Map\n")
					} else {
						fmt.Printf("  Walking Up the Map:  Depth: %d\n", udn_current.Depth)
					}
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
						if new_child_item != "" {
							// Sub-statement.  UDN inside UDN, process these first, by depth, but initially parse them into place
							new_udn := NewUdnPart()
							new_udn.ParentUdnPart = udn_current
							//fmt.Printf("Setting New UDN Parent: %v   Parent: %v\n", new_udn, udn_current)

							new_udn.Depth = udn_current.Depth + 1

							new_udn.PartType = part_item
							new_udn.Value = new_child_item

							// Add to current chilidren
							udn_current.Children.PushBack(&new_udn)

							fmt.Printf("Create UDN: Add Child Element: %s    Adding to: %s\n", new_child_item, udn_current.Value)
						}
					}
				}
			}
		}
	}



	return udn_start
}



func _SplitStringAndKeepSeparator(value string, separator string) []string {
	split_array := strings.Split(value, separator)

	final_array := make([]string, (len(split_array) * 2) - 1)


	for pos, item := range split_array {
		cur_pos := pos * 2

		final_array[cur_pos] = item

		if cur_pos + 1 < len(final_array) {
			final_array[cur_pos + 1] = separator
		}
	}

	// Fix the stupid trailing empty item, if it exists.  Will just increase with splits.
	if final_array[len(final_array)-1] == "" {
		final_array = final_array[0:len(final_array)-1]
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
func _SplitCompountStatements(db *sql.DB, udn_schema map[string]interface{}, source_array []string) []string {
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


__query.1.__query.4


__if.condition.__fucn.args.__else.__functionaoeuoeu.__end_else.__endif

__iterate.map.string.__func.stuff.__end_iterate.__output.user


Non-Concurrency:


[
	[Source, Dest]
	[Source, Dest]
]


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
