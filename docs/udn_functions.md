# UDN Functions

## Data Access


### __get ::: Get Global Data

**Go:** UDN_Get

**Input:** Ignored

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

Go: UDN_Set

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


### __get_first ::: Get first non-nil Global Data

Takes an array of N strings, which are dotted for udn_data accessing.  The first value that isnt nil is returned.  nil is returned if they all are.

Go: UDN_Get

Input: Ignored

Args:

  - string :: Dotted string ('location.goes.here')
  - string (optional, variadic) :: Any number of args can be provided, same as the first argument

Output: Any

Example:

```
__input.'Hello World'.__set.special.field.__get_first.'not.a.real.place'.'special.field'
```

Result:

```
Hello World
```

Side Effect: None

### __get_temp ::: Get Temporary Data

Just like __get, except uses a portino of the Global Data space behind a UUID for this ProcessSchemaUDNSet() or __function call.  It allows names to be re-used, which they cannot be in the normal Global Data space, as it is global.

Go: UDN_GetTemp

Input: Ignored

Args:

  - string :: If quoted, this can contain dots, of each arg will become part of a "dotted string" to access the global data
  - string (optional, variadic) :: Any number of args can be provided, all strings

Output: Any

Example:

```
__input.Testing123.__set_temp.temp.testing.__get_temp.temp.testing
```

Result:

```
Testing123
```


Alternate Example, single dotted string uses the same Global Data:

```
__input.Testing123.__set_temp.'temp.testing'.__get_temp.temp.testing
```


Side Effect: None


### __set_temp ::: Set Global Data

Just like __set, except uses a portino of the Global Data space behind a UUID for this ProcessSchemaUDNSet() or __function call.  It allows names to be re-used, which they cannot be in the normal Global Data space, as it is global.

Go: UDN_SetTemp

Input: Ignored

Args:

  - string :: If quoted, this can contain dots, of each arg will become part of a "dotted string" to access the global data
  - string (optional, variadic) :: Any number of args can be provided, all strings
  - Any :: The final data can be any value, and is set into the location

Output: list of maps :: []map[string]interface{}

Example:

```
__input.Testing123.__set_temp.testing.__get_temp.testing
```

Result:

```
Testing123
```


Alternate Example, single dotted string uses the same Global Data:

```
__input.Testing123.__set_'temp.testing'.__get_temp.testing
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


### __compare_equal :: Conditon to Check for Equality

Go: UDN_CompareEqual

Input: Ignored

Args:
    - Any :: Converted to a string for comparison
    - Any :: Converted to a string for comparison

Output: Boolean: 1, 0

Example:

```
__if.(__compare_equal.Tom.Jerry).__input.1.__else.__input.0.__end_if
```

Returns:

```
0
```

Related Functions: __compare_not_equal

Side Effect: None

### __compare_not_equal :: Conditon to Check for Non-Equality

Go: UDN_CompareNotEqual

Input: Ignored

Args:
    - Any :: Converted to a string for comparison
    - Any :: Converted to a string for comparison

Output: Boolean: 1, 0

Example:

```
__if.(__compare_not_equal.Tom.Jerry).__input.1.__else.__input.0.__end_if
```

Returns:

```
1
```

Related Functions: __compare_equal

Side Effect: None

## Text

### __template :: String Template From Value

Go: UDN_StringTemplateFromValue

Input: Map :: map[string]interface{}

Args:
    - string :: Text to be templated, using Go's text/template function
    - Map (optional) :: Overrides the Input map value, if present

Output: string

Example:

```
__input.{name="Bob"}.__template.'Name: {index .Map "name"}'
```

Returns:

```
"Name: Bob"
```

Related Functions: __template_wrap, __template_short, __format, __template_map

Side Effect: None


### __template_wrap :: String Template From Value

Takes N-2 tuple args, after 0th arg, which is the wrap_key, (also supports a single arg templating, like __template, but not the main purpose).  For each N-Tuple, the new map data gets "value" set by the previous output of the last template, creating a rolling "wrap" function.

NOTE(g): I dont know how this function is used at this point.  It was useful, but I dont see an example to explain it.  It's extremely overloaded, but powerful.

Go: UDN_StringTemplateMultiWrap

Input: Map :: map[string]interface{}

Args:
    - string :: Text to be templated, using Go's text/template function
    - Map (optional) :: Overrides the Input map value, if present

Output: string

Example:

```
__input.{name=Bob,job=Programmer}.__template_wrap.'Name: {index .Map "name"}'.{name=Bob}.'Job: {index .Map "job"}'.{job=Programmer}
```

Returns:

```
"Name: Bob"
```

Related Functions: __template, __template_short, __format, __template_map

Side Effect: None

### __template_map :: String Template From Value

Like format, for templating.  Takes 3*N args: (key,text,map), any number of times.  Performs template and assigns key into the input map

Go: UDN_MapTemplate

Input: Ignored

Args:
    - String :: Set key.  This is where we will set the value once templated.
    - String :: Template text.  This is the text to be templated.
    - Map :: This is the data to be templated into the 2nd arg.

Output: Passed Through Input

Example:

```
__template_map.'location.saved'.'Name: {index .Map "name"}'.{name=Bob}.__get.location.saved
```

Returns:

```
"Name: Bob"
```

Related Functions: __template_wrap, __template_short, __format, __template

Side Effect: None


### __format :: Format Strings from Map

Updates a map with keys and string formats.  Uses the map to format the strings.  Takes N args, doing each arg in sequence, for order control

Go: UDN_MapStringFormat

Input: Map :: map[string]interface

Args:
    - String :: Set key.  This is where we will set the value once templated.
    - Map :: This is the data to be templated into the 2nd arg.
    - String (optional, variadic) :: Indefinite pairs of String/Map args
    - Map (optional, variadic) :: Indefinite pairs of String/Map args

Output: Passed Through Input

Example:

```
__input.{name=Bob,job=Programmer}.__format.'location.saved.name'.'Name: {index .Map "name"}'.'location.saved.job'.'Job: {index .Map "job"}.__get.location.saved.name'
```

Returns:

```
"Name: Bob"
```

Related Functions: __template_wrap, __template_short, __format, __template

Side Effect: None


### __template_short :: String Template From Value

Like __template, but uses {{{name}} instead of {index .Map "name"}

Go: UDN_StringTemplateFromValueShort

Input: Map :: map[string]interface

Args:
    - String :: Set key.  This is where we will set the value once templated.
    - Map (optional) :: This overrides the Input, if present

Output: String

Example:

```
__input.{name=Bob,job=Programmer}.__template_short.'Name: {{{name}}}'
```

Returns:

```
"Name: Bob"
```

Related Functions: __template_wrap, __template_short, __format, __template

Side Effect: None


### __string_append :: String Append

Appends to an existing string, or creates a string if nil (not present in Global Data).  Args work like __get

Go: UDN_StringAppend

Input: String

Args:
  - string :: If quoted, this can contain dots, of each arg will become part of a "dotted string" to access the global data
  - string (optional, variadic) :: Any number of args can be provided, all strings

Output: String

Example:

```
__input.'The Quick '.__set.temp.test.__input.'Brown Fox'.__string_append.temp.test.__get.temp.test
```

Returns:

```
"The Quick Brown Fox"
```

Related Functions: __string_clear, __concat

Side Effect: None


### __string_clear:: String Clear

This is only needed when re-using a Global Data label, you can start appending to an non-existent location and it will start it with an empty string.

Go: UDN_StringClear

Input: String

Args:
  - string :: If quoted, this can contain dots, of each arg will become part of a "dotted string" to access the global data
  - string (optional, variadic) :: Any number of args can be provided, all strings

Output: String

Example:

```
__string_clear.temp.test
```

Related Functions: __string_append

Side Effect: None


### __concat :: String Concatenate

TODO(g): Not Yet Implemented

Go: UDN_StringConcat

Input: String

Args:
  - string :: If quoted, this can contain dots, of each arg will become part of a "dotted string" to access the global data
  - string (optional, variadic) :: Any number of args can be provided, all strings

Output: String

Example:

```
```

Returns:

```
```

Related Functions: __string_clear, __string_append

Side Effect: None


### __json_decode :: JSON Decode

Decodes a string to Go data: map[string]interface is assumed if using Global Data

Go: UDN_JsonDecode

Input: String

Args: None

Output: Map :: map[string]interface

Example:

```
__input.'{"a": 1}'.__json_decode
```

Returns:

```
{a: 1}
```

Related Functions: __json_encode

Side Effect: None


### __json_encode :: JSON Encode

Encodes Go data into a JSON string

Go: UDN_JsonDecode

Input: Any

Args: None

Output: String

Example:

```
__input.{a=1}.__json_encode
```

Returns:

```
{"a": "1"}
```

Related Functions: __json_decode

Side Effect: None





		"__html_encode": UDN_HtmlEncode,		// Encode HTML symbols so they are not taken as literal HTML

		"__input": UDN_Input,			//TODO(g): This takes any input as the first arg, and then passes it along, so we can type in new input to go down the pipeline...
		"__input_get": UDN_InputGet,			// Gets information from the input, accessing it like __get


		"__widget": UDN_Widget,

		"__render_data": UDN_RenderDataWidgetInstance,			// Renders a Data Widget Instance:  arg0 = web_data_widget_instance.id, arg1 = widget_instance map update


		"__function": UDN_StoredFunction,			//TODO(g): This uses the udn_stored_function.name as the first argument, and then uses the current input to pass to the function, returning the final result of the function.		Uses the web_site.udn_stored_function_domain_id to determine the stored function
		"__execute": UDN_Execute,			//TODO(g): Executes ("eval") a UDN string, assumed to be a "Set" type (Target), will use __input as the Source, and the passed in string as the Target UDN



		"__array_append": UDN_ArrayAppend,			// Appends the input into the specified target location (args)

		"__array_divide": UDN_ArrayDivide,			//TODO(g): Breaks an array up into a set of arrays, based on a divisor.  Ex: divide=4, a 14 item array will be 4 arrays, of 4/4/4/2 items each.
		"__array_map_remap": UDN_ArrayMapRemap,			//TODO(g): Takes an array of maps, and makes a new array of maps, based on the arg[0] (map) mapping (key_new=key_old)


		"__map_key_delete": UDN_MapKeyDelete,			// Each argument is a key to remove
		"__map_key_set": UDN_MapKeySet,			// Sets N keys, like __format, but with no formatting
		"__map_copy": UDN_MapCopy,			// Make a copy of the current map, in a new map
		"__map_update": UDN_MapUpdate,			// Input map has fields updated with arg0 map

		"__data_get": UDN_DataGet,					// Dataman Get
		"__data_set": UDN_DataSet,					// Dataman Set
		"__data_filter": UDN_DataFilter,			// Dataman Filter

		"__ddd_render": UDN_DddRender,			// DDD Render.current: the JSON Dialog Form data for this DDD position.  Uses __ddd_get to get the data, and ___ddd_move to change position.

		"__login": UDN_Login,				// Login through LDAP


# Remove these functions?

		"__temp_label":          UDN_GetTempAccessor,		// This takes a string as an arg, like "info", then returns "(__get.'__function_stack.-1.uuid').info".  Later we will make temp data concurrency safe, so when you need accessors as a string, to a temp (like __string_clear), use this
        "__test_return":           UDN_TestReturn, // Return some data as a result
		"__test":           UDN_Test,
		"__test_different": UDN_TestDifferent,

