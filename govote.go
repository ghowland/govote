package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/jcasts/gosrv"
	_ "github.com/mattn/go-sqlite3"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
    "github.com/junhsieh/goexamples/fieldbinding/fieldbinding"
	//"github.com/jacksontj/goUDN"
	//"container/list"
	//  "net/url"
	//  "io"
	//  "bytes"
	//  "path"
)


type ApiRequest struct {
	// User information
	UserId int
	UserName string
	UserAuthToken string
	
	// Ingress API information
	IngressHostname string
	IngressProtocol string
	IngressUri string
	IngressArgs string
	IngressHeaderMap map[string] string
	IngressCookieMap map[string] string
	IngressDataMap map[string] interface{}
	
	// Database information -- For security reasons, dont put DBs in this map that this user doesnt have access to
	DatabaseList map[int64] *sql.DB
}

type StringFile struct {
	String string
}

func (s *StringFile) Write(ingress []byte) (count int, err error) {
	s.String += string(ingress)

	return len(ingress), nil
}

func NewTextTemplateMap() *TextTemplateMap {
	return &TextTemplateMap {
		Map: make(map[string]interface{}),
	}
}

func NewTextTemplateMapItem() TextTemplateMap {
	return TextTemplateMap {
		Map: make(map[string]interface{}),
	}
}

type TextTemplateMap struct {
	Map map[string]interface{}
}


type Forum struct {
	Id              int
	Name            string
	URL             string
	CountViews      int
	CountThreads    int
	CountComments   int
	Body            string
	CommentDataJson string
}


type CommentSet struct {
	comments []Comment
}

type Comment struct {
	Posted        int64
	Edited        int64
	AuthorId      int64
	Body          string
	CountUpvote   int
	CountDownvote int
}

func main() {
	s, err := gosrv.NewFromFlag()
	if err != nil {
		panic(err)
	}

	customThing, err := s.Config.String("customThing")
	if err != nil {
		fmt.Println("Custom thing is " + customThing + " for environment " + s.Config.Env)
	}

	s.HandleFunc("/", handler)

	err = s.ListenAndServe()
	if err != nil {
		panic(err)
	}
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

	relative_path := "./web" + uri

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


func Extend(slice []Comment, element Comment) []Comment {
	n := len(slice)
	if n == cap(slice) {
		// Slice is full; must grow.
		// We double its size and add 1, so if the size is zero we still grow.
		newSlice := make([]Comment, len(slice), 2*len(slice)+1)
		copy(newSlice, slice)
		slice = newSlice
	}
	slice = slice[0 : n+1]
	slice[n] = element
	return slice
}

// Append appends the items to the slice.
// First version: just loop calling Extend.
func Append(slice []Comment, items ...Comment) []Comment {
	for _, item := range items {
		slice = Extend(slice, item)
	}
	return slice
}

func dynamicPage(uri string, w http.ResponseWriter, r *http.Request) {

	// DB
	db, err := sql.Open("sqlite3", "./vvvoting.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// DB Web
	db_web, err := sql.Open("sqlite3", "/Users/ghowland/projects/slowcust/database/govote.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db_web.Close()

	web_site_id := 1
	//web_site_domain_id := 1
	
	//TODO(g): Get the web_site_domain from host header
	
	// Get the path to match from the DB
	sql := fmt.Sprintf("SELECT * FROM web_site WHERE id = %d", web_site_id)
	web_site_result := Query(db_web, sql)

	fmt.Printf("\n\nGetting Web Site Page from URI: %s\n\n", uri)

	
	// Get the path to match from the DB
	sql = fmt.Sprintf("SELECT * FROM web_site_page WHERE web_site_id = %d AND name = '%s'", web_site_id, SanitizeSQL(uri))
	web_site_page_result := Query(db_web, sql)

	// If we found a matching page
	if uri == "/api/save" {
		dynamicPage_API_Save(db_web, db, uri, w, r)
	} else if (len(web_site_page_result) > 0) {
		dynamePage_RenderWidgets(db_web, db, web_site_result[0], web_site_page_result[0], uri, w, r)
	} else {
		dynamicPage_404(uri, w, r)
	}
}

func MapListToDict(map_array []TextTemplateMap, key string) *TextTemplateMap {
	// Build a map of all our web site page widgets, so we can 
	output_map := NewTextTemplateMap()
	
	for _, map_item := range map_array {
		output_map.Map[map_item.Map[key].(string)] = map_item;
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
		if (site_page_widget.Map["id"] == web_site_page.Map["base_page_web_site_page_widget_id"]) {
			continue
		}
		
		// Get the base widget
		sql = fmt.Sprintf("SELECT * FROM web_widget WHERE id = %d", site_page_widget.Map["web_widget_id"])
		page_widgets := Query(db_web, sql)
		page_widget := page_widgets[0]
		
		fmt.Printf("Page Widget: %s: %s\n" , site_page_widget.Map["name"], page_widget.Map["name"])
		
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
					if (strings.HasPrefix(widget_value.(string), "__")) {
						result := ProcessDataUDN(db_web, db, *web_site_page_widget_map, web_site_page, *page_map, page_widget, *widget_map, widget_value.(string))
						
						widget_map.Map[widget_key] = result
					} else {
						widget_map.Map[widget_key] = fmt.Sprintf("%v", widget_value)
					}
				default:
					widget_map.Map[widget_key] = fmt.Sprintf("%v", widget_value)
			}
			
		}
		
		fmt.Printf("Title: %s\n", widget_map.Map["title"])
		
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
	
	if (parts[0] == "__auth") {
		if (parts[1] == "user_name") {
			result = "Geoff Howland"
		} else if (parts[1] == "user_label") {
			result = "Voter Prime"
		} else if (parts[1] == "user_image") {
			result = "/img/geoff_.jpg"
		} else {
			result = "Unknown"
		}
	} else if (parts[0] == "__widget") {
		
		//TODO(g): This doesnt exist yet, because it hasnt been created yet.  Instead I want to pull it from the UNPROCESSED TEMPLATE, because we are going to process it here!
		result = page_map.Map[parts[1]]
		
		fmt.Printf("Widget Initial Result: %v", result)
		
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
		
		fmt.Printf("\n_____\nPage Map Values: %v\n\nWidget Map Values: %v\n\nWeb Site Page: %v\n\nSite Page Widgets: %v\n\nWidget JSON Data: %v\n\n_____\n", page_map.Map, widget_map.Map, web_site_page.Map, web_site_page_widget_map.Map, site_page_widget_json_data.Map)
		
		fmt.Printf("\nSite Page Widget Specific: %v\n\n", page_widget.Map)
		
		// If we have a data source specified as a suffixed handler to this widget
		//TODO(g): Generalize this search for all UDN components, for now its OK that its just __widget, as thats where I need it first
//		if (len(parts) > 2 && strings.HasPrefix(parts[1], "__")) {
		if (len(parts) > 2) {
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
		
		
	} else if (parts[0] == "__query") {
		sql := fmt.Sprintf("SELECT * FROM web_site_query WHERE id = %s", parts[1])

		fmt.Printf("\n_____\nQuery Handler: %s\n\n_____\n", sql)

		rows := Query(db_web, sql)
		
		//fmt.Printf("\n_____\nQuery: %v\n\nQuery Result Values: %v\n_____\n", sql, rows)
		
		// Perform the query we just fetched, from the correct DB (not db_web)
		//TODO(g): This should actually be able to talk to multiple databases, as specified by the web_site_datasource_id
		rows = Query(db, rows[0].Map["sql"].(string))
		
		fmt.Printf("\n_____\nQuery: %v\n\nWidget Map Values: %v\n_____\n", rows[0].Map["sql"], rows)
		
		//TODO(g): The calling function should call this:  	defer rows.Close()   How do we enforce this?  I assume failure to do this will cause leaks and maybe other problems.
		result = rows
		
	} else if (parts[0] == "__row") {
		//result = fmt.Sprintf("ROW: Not found: %s  --> %v", udn, parts)
		result = widget_map.Map[parts[1]]
		fmt.Printf("__row: %s: %v", parts[1], result)
	} else if (parts[0] == "__site_map") {
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
		
	} else if (parts[0] == "__widget_id") {
		fmt.Printf("Generating query: %s\n", parts[1])
		sql := fmt.Sprintf("SELECT * FROM web_widget WHERE id = %s", parts[1])
		fmt.Printf("Generated query: %s\n", sql)
		
		rows := Query(db_web, sql)
		
		fmt.Printf("Generated query next: 2\n", sql)
		result = ReadPathData(rows[0].Map["path"].(string))
		fmt.Printf("Generated query next: 3\n", sql)

	} else if (parts[0] == "__field") {
		field_name := parts[1]
		
		new_udn := strings.Join(parts[2:], ".")
		
		rows := ProcessDataUDN(db_web, db, web_site_page_widget_map, web_site_page, page_map, page_widget, widget_map, new_udn).([]TextTemplateMap)
		
		fmt.Printf("Field Processor: %s: %v\n", field_name, rows)
		
		result = rows[0].Map[field_name]
		
	} else if (parts[0] == "__json_rows") {
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
		
		
	} else if (parts[0] == "__save") {
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
	
	} else if (parts[0] == "__delete_confirm") {
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
		
	} else if (parts[0] == "__save_append_json_row") {
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
		
	} else if (parts[0] == "__save_update_json_row") {
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
		
		
	} else if (parts[0] == "__save_delete_json_row") {
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
		
	} else if (strings.HasPrefix(parts[0], "__")) {
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
	
	return outArr;
}


func SanitizeSQL(text string) string {
	text = strings.Replace(text, "'", "''", -1)
	
	return text
}


func dynamicPage_SubmitComment(uri string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/json")

	thread_id, err := strconv.ParseInt(r.FormValue("thread_id"), 10, 64)
	if err != nil {
		thread_id = -1
	}
	body := r.FormValue("body")

	log.Println("Submit Comment:", thread_id, body)

	// DB
	db, err := sql.Open("sqlite3", "./vvvoting.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	forum := Forum{}

	// Query
	rows, err := db.Query(fmt.Sprintf("select id, name, url, body, comment_data_json from forum_thread WHERE id = %d", thread_id))
	if err != nil {
		log.Fatal(err)
	}

	rows.Next()

	err = rows.Scan(&forum.Id, &forum.Name, &forum.URL, &forum.Body, &forum.CommentDataJson)
	if err != nil {
		log.Fatal(err)
	}

	rows.Close()

	log.Println("Forum thread:", forum.Name, forum.Id)

	dec := json.NewDecoder(strings.NewReader(forum.CommentDataJson))

	var comment_set CommentSet

	err = dec.Decode(&comment_set.comments)
	if err == io.EOF {
	} else if err != nil {
		log.Fatal(err)
	} else {
		log.Printf("%d: %s", comment_set.comments[0].AuthorId, comment_set.comments[0].Body)
	}

	var comment Comment
	comment.AuthorId = 1
	comment.Body = body

	comment_set.comments = Append(comment_set.comments, comment)

	log.Println(comment)
	log.Println(comment_set)

	comments_json, err := json.Marshal(comment_set.comments)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Comment Json:", string(comments_json))

	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}

	// Save this comment
	data_json := SanitizeSQL(string(comments_json))
	sql := fmt.Sprintf("UPDATE forum_thread SET comment_data_json = '%s' WHERE id = %d", data_json, thread_id)
	log.Println("SQL: ", sql)
	_, err = tx.Exec(sql)
	if err != nil {
		log.Fatal(err)
	}
	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	log.Print("Committed...")

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}

	output_json := "{\"__reload_page\": true}"
	
	log.Println(output_json)
	
	// Write out the final page
	w.Write([]byte(output_json))
}


func dynamicPage_SubmitThread(uri string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/json")

	forum_id, err := strconv.ParseInt(r.FormValue("forum_id"), 10, 64)
	if err != nil {
		forum_id = -1
	}
	body := r.FormValue("body")
	url := r.FormValue("url")
	subject := r.FormValue("subject")

	log.Println("Submit Thread:", forum_id, subject, url, body)

	// DB
	db, err := sql.Open("sqlite3", "./vvvoting.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Do it
	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}

	// Save this comment
	sql := fmt.Sprintf("INSERT INTO forum_thread (forum_id, user_poster, posted, updated, name, url, body, comment_data_json, vote_data_json) VALUES (%d, %d, DATETIME('now'), DATETIME('now'), '%s', '%s', '%s', '', '')", forum_id, 1, SanitizeSQL(subject), SanitizeSQL(url), SanitizeSQL(body))
	log.Println("SQL: ", sql)
	_, err = tx.Exec(sql)
	if err != nil {
		log.Fatal(err)
	}
	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	log.Print("Committed...")


	output_json := "{\"__reload_page\": true}"
	
	log.Println(output_json)
	
	// Write out the final page
	w.Write([]byte(output_json))
}


func dynamicPage_SubmitForum(uri string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/json")

	name := r.FormValue("name")
	is_public := r.FormValue("is_public")
	body := r.FormValue("body")

	log.Println("Submit Forum:", name, is_public, body)

	// DB
	db, err := sql.Open("sqlite3", "./vvvoting.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Do it
	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}

	
	
	// Save this comment
	sql := fmt.Sprintf("INSERT INTO forum (name, user_owner, is_private, info) VALUES ('%s', %d, %d, '%s')", SanitizeSQL(name), 1, 0, SanitizeSQL(body))
	log.Println("SQL: ", sql)
	_, err = tx.Exec(sql)
	if err != nil {
		log.Fatal(err)
	}
	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	log.Print("Committed...")


	output_json := "{\"__reload_page\": true}"
	
	log.Println(output_json)
	
	// Write out the final page
	w.Write([]byte(output_json))
}


func dynamicPage_404(uri string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	base_html, err := ioutil.ReadFile("web/404.html")
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
	bundle_map := map[string] TextTemplateMap{}

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
			if (strings.HasPrefix(key, "__meta.")) {
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
			if (strings.HasPrefix(key, "__meta.")) {
				parts := strings.Split(key, ".")
				meta_key := parts[1]
				meta_command := parts[2]
				
				if meta_command == "__increment" {
					field = meta_key
					value = value		// Set to itself, positive numberline
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
			last_row := json_map_list.([]interface{})[len(json_map_list.([]interface{}))-1].(map[string] interface{})
			
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
			item := row.(map[string] interface{})
			
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
			item := row.(map[string] interface{})
			
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

