# UDN Functions

## Data Access


### __get ::: Get Global Data

Go: UDN_Get

Input: Ignored

Args:

  - string :: If quoted, this can contain dots, of each arg will become part of a "dotted string" to access the global data
  - string (optional, variadic) :: Any number of args can be provided, all strings

Output: list of maps :: []map[string]interface{}

Example:

```
__input.Testing123.__set.temp.testing.__get.temp.testing
```

Result:

```
Testing123
```

Alternate Example, single dotted string uses the same Global Data:

```
__input.Testing123.__set.temp.testing.__get.'temp.testing'
```

Side Effect: None

### __set ::: Set Global Data

Go: UDN_Get

Input: Ignored

Args:

  - string :: If quoted, this can contain dots, of each arg will become part of a "dotted string" to access the global data
  - string (optional, variadic) :: Any number of args can be provided, all strings
  - Any :: The final data can be any value, and is set into the location

Output: list of maps :: []map[string]interface{}

Example:

```
__input.Testing123.__set.temp.testing.__get.temp.testing
```

Result:

```
Testing123
```


Alternate Example, single dotted string uses the same Global Data:

```
__input.Testing123.__set.'temp.testing'.__get.temp.testing
```


Side Effect: None

## Database

### __query ::: Stored SQL Querying

Go: UDN_QueryById

Input: Ignored

Args:

  - int :: datasource_query.id record primary key
  - map (optional) :: data arguments for the query, are short templated into the stored SQL

Output: list of maps :: []map[string]interface{}

Example:

```
__query.25
```

Side Effect: None

## Debugging

### ____debug_output

Go: UDN_QueryById

Input: Any

Args: None

Output: Pass Through Input

Example:

```
__debug_output
```

Side Effect: Prints input to the debug log

## Conditions and Looping

### __if :: Conditional If

Go: UDN_IfCondition

Input: Any

Args: None

Output: Last Output Function Result

Example:

```
__if.1.__debug_output.__end_if
```

Related Functions: __else_if

End Block: __end_if

Side Effect: Loops over all functions in the block (between __if and matching __end_if)

### __else_if :: Conditional Else, If

Go: UDN_ElseCondition

Input: Any

Args: None

Output: Last Output Function Result

Example:

```
__if.0.__debug_output.__else_if.__debug_output.__end_if
```

End Block: __end_if

Side Effect: Loops over all functions in the block (between __else_if and matching __end_if or next __else_if)

### __end_if :: End If/ElseIf Block

Go: nil

Input: Any

Args: None

Output: Last Output Function Result

Example:

```
__if.1.__debug_output.__end_if
```

Side Effect: None

### __not :: Not - Reverses boolean test (1, "1", true)

Go: UDN_Not

Input: Boolean value: true, 1, "1", false, 0, "0"

Args:
    - Boolean, String, Integer: true, false, "1", "0", 1, 0

Output: Boolean: 1, 0

Example:

```
__if.(__not.0).__debug_output.__end_if
```

Side Effect: None

### __not_nil :: Not Nil - Returns "1" (true) if not nil

Go: UDN_NotNil

Input: nil or Not

Args: None

Output: Boolean: 1, 0

Example:

```
__if.(__not.0).__debug_output.__end_if
```

Side Effect: None

### __iterate :: Iterate

Go: UDN_Iterate

Input: Any

Args: None

Output: First Element of Array

Example:

```
__iterate.__debug_output.__end_iterate
```

End Block: __end_iterate

Side Effect: Loops over all functions in the block (between __iterate and matching __end_iterate)


### __end_iterate :: End Iterate

Go: nil

Input: Any

Args: None

Output: Array of All iterate block runs

Example:

```
__input.[1,2,3].__iterate.__debug_output.__end_iterate
```

Returns:

```
[1,2,3]
```

End Block: __end_iterate

Side Effect: None



		"__get":          UDN_Get,
		"__set":          UDN_Set,
		"__get_first":          UDN_GetFirst,		// Takes N strings, which are dotted for udn_data accessing.  The first value that isnt nil is returned.  nil is returned if they all are
		"__get_temp":          UDN_GetTemp,			// Function stack based temp storage
		"__set_temp":          UDN_SetTemp,			// Function stack based temp storage
		"__temp_label":          UDN_GetTempAccessor,		// This takes a string as an arg, like "info", then returns "(__get.'__function_stack.-1.uuid').info".  Later we will make temp data concurrency safe, so when you need accessors as a string, to a temp (like __string_clear), use this
		"__test_return":           UDN_TestReturn, // Return some data as a result
		"__test":           UDN_Test,
		"__test_different": UDN_TestDifferent,
		"__widget": UDN_Widget,
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
