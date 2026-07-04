package action

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseActions(t *testing.T) {
	actions, err := Parse("[]")
	require.Nil(t, err)
	require.Empty(t, actions)

	// Basic test
	actions, err = Parse("action=http, label=Open door, url=https://door.lan/open; view, Show portal, https://door.lan")
	require.Nil(t, err)
	require.Equal(t, 2, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, "Open door", actions[0].Label)
	require.Equal(t, "https://door.lan/open", actions[0].URL)
	require.Equal(t, "view", actions[1].Action)
	require.Equal(t, "Show portal", actions[1].Label)
	require.Equal(t, "https://door.lan", actions[1].URL)

	// JSON
	actions, err = Parse(`[{"action":"http","label":"Open door","url":"https://door.lan/open"}, {"action":"view","label":"Show portal","url":"https://door.lan"}]`)
	require.Nil(t, err)
	require.Equal(t, 2, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, "Open door", actions[0].Label)
	require.Equal(t, "https://door.lan/open", actions[0].URL)
	require.Equal(t, "view", actions[1].Action)
	require.Equal(t, "Show portal", actions[1].Label)
	require.Equal(t, "https://door.lan", actions[1].URL)

	// Other params
	actions, err = Parse("action=http, label=Open door, url=https://door.lan/open, body=this is a body, method=PUT")
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, "Open door", actions[0].Label)
	require.Equal(t, "https://door.lan/open", actions[0].URL)
	require.Equal(t, "PUT", actions[0].Method)
	require.Equal(t, "this is a body", actions[0].Body)

	// Extras with underscores
	actions, err = Parse("action=broadcast, label=Do a thing, extras.command=some command, extras.some_param=a parameter")
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "broadcast", actions[0].Action)
	require.Equal(t, "Do a thing", actions[0].Label)
	require.Equal(t, 2, len(actions[0].Extras))
	require.Equal(t, "some command", actions[0].Extras["command"])
	require.Equal(t, "a parameter", actions[0].Extras["some_param"])

	// Broadcast action with intent
	actions, err = Parse("action=broadcast, label=Do a thing, intent=io.heckel.ntfy.TEST_INTENT")
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "broadcast", actions[0].Action)
	require.Equal(t, "Do a thing", actions[0].Label)
	require.Equal(t, "io.heckel.ntfy.TEST_INTENT", actions[0].Intent)

	// Headers with dashes
	actions, err = Parse("action=http, label=Send request, url=http://example.com, method=GET, headers.Content-Type=application/json, headers.Authorization=Basic sdasffsf")
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, "Send request", actions[0].Label)
	require.Equal(t, 2, len(actions[0].Headers))
	require.Equal(t, "application/json", actions[0].Headers["Content-Type"])
	require.Equal(t, "Basic sdasffsf", actions[0].Headers["Authorization"])

	// Quotes
	actions, err = Parse(`action=http, "Look ma, \"quotes\"; and semicolons", url=http://example.com`)
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, `Look ma, "quotes"; and semicolons`, actions[0].Label)
	require.Equal(t, `http://example.com`, actions[0].URL)

	// Single quotes
	actions, err = Parse(`action=http, '"quotes" and \'single quotes\'', url=http://example.com`)
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, `"quotes" and 'single quotes'`, actions[0].Label)
	require.Equal(t, `http://example.com`, actions[0].URL)

	// Single quotes (JSON)
	actions, err = Parse(`action=http, Post it, url=http://example.com, body='{"temperature": 65}'`)
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, "Post it", actions[0].Label)
	require.Equal(t, `http://example.com`, actions[0].URL)
	require.Equal(t, `{"temperature": 65}`, actions[0].Body)

	// Out of order
	actions, err = Parse(`label="Out of order!" , action="http", url=http://example.com`)
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, `Out of order!`, actions[0].Label)
	require.Equal(t, `http://example.com`, actions[0].URL)

	// Spaces
	actions, err = Parse(`action = http, label = 'this is a label', url = "http://google.com"`)
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, `this is a label`, actions[0].Label)
	require.Equal(t, `http://google.com`, actions[0].URL)

	// Non-ASCII
	actions, err = Parse(`action = http, 'Кохайтеся а не воюйте, 💙🫤', url = "http://google.com"`)
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, `Кохайтеся а не воюйте, 💙🫤`, actions[0].Label)
	require.Equal(t, `http://google.com`, actions[0].URL)

	// Multiple actions, awkward spacing
	actions, err = Parse(`http , 'Make love, not war 💙🫤' , https://ntfy.sh ; view, " yo ", https://x.org, clear=true`)
	require.Nil(t, err)
	require.Equal(t, 2, len(actions))
	require.Equal(t, "http", actions[0].Action)
	require.Equal(t, `Make love, not war 💙🫤`, actions[0].Label)
	require.Equal(t, `https://ntfy.sh`, actions[0].URL)
	require.Equal(t, false, actions[0].Clear)
	require.Equal(t, "view", actions[1].Action)
	require.Equal(t, " yo ", actions[1].Label)
	require.Equal(t, `https://x.org`, actions[1].URL)
	require.Equal(t, true, actions[1].Clear)

	// Copy action (simple format)
	actions, err = Parse("copy, Copy code, 1234")
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "copy", actions[0].Action)
	require.Equal(t, "Copy code", actions[0].Label)
	require.Equal(t, "1234", actions[0].Value)

	// Copy action (JSON)
	actions, err = Parse(`[{"action":"copy","label":"Copy OTP","value":"567890"}]`)
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "copy", actions[0].Action)
	require.Equal(t, "Copy OTP", actions[0].Label)
	require.Equal(t, "567890", actions[0].Value)

	// Copy action with clear
	actions, err = Parse("copy, Copy code, 1234, clear=true")
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "copy", actions[0].Action)
	require.Equal(t, "Copy code", actions[0].Label)
	require.Equal(t, "1234", actions[0].Value)
	require.Equal(t, true, actions[0].Clear)

	// Copy action with explicit value key
	actions, err = Parse("action=copy, label=Copy token, clear=true, value=abc-123-def")
	require.Nil(t, err)
	require.Equal(t, 1, len(actions))
	require.Equal(t, "copy", actions[0].Action)
	require.Equal(t, "Copy token", actions[0].Label)
	require.Equal(t, "abc-123-def", actions[0].Value)
	require.True(t, actions[0].Clear)

	// Copy action without value (error)
	_, err = Parse("copy, Copy code")
	require.EqualError(t, err, "parameter 'value' is required for action 'copy'")

	// Invalid syntax
	_, err = Parse(`label="Out of order!" x, action="http", url=http://example.com`)
	require.EqualError(t, err, "unexpected character 'x' at position 22")

	_, err = Parse(`label="", action="http", url=http://example.com`)
	require.EqualError(t, err, "parameter 'label' is required")

	_, err = Parse(`label=, action="http", url=http://example.com`)
	require.EqualError(t, err, "parameter 'label' is required")

	_, err = Parse(`label="xx", action="http", url=http://example.com, what is this anyway`)
	require.EqualError(t, err, "term 'what is this anyway' unknown")

	_, err = Parse(`fdsfdsf`)
	require.EqualError(t, err, "parameter 'action' cannot be 'fdsfdsf', valid values are 'view', 'broadcast', 'http' and 'copy'")

	_, err = Parse(`aaa=a, "bbb, 'ccc, ddd, eee "`)
	require.EqualError(t, err, "key 'aaa' unknown")

	_, err = Parse(`action=http, label="omg the end quote is missing`)
	require.EqualError(t, err, "unexpected end of input, quote started at position 20")

	_, err = Parse(`;;;;`)
	require.EqualError(t, err, "only 3 actions allowed")

	_, err = Parse(`,,,,,,;;`)
	require.EqualError(t, err, "term '' unknown")

	_, err = Parse(`''";,;"`)
	require.EqualError(t, err, "unexpected character '\"' at position 2")

	_, err = Parse(`action=http, label=a label, body=somebody`)
	require.EqualError(t, err, "parameter 'url' is required for action 'http'")

	_, err = Parse(`action=http, label=a label, url=http://ntfy.sh, method=HEAD, body=somebody`)
	require.EqualError(t, err, "parameter 'body' cannot be set if method is HEAD")

	_, err = Parse(`[ invalid json ]`)
	require.EqualError(t, err, "JSON error: invalid character 'i' looking for beginning of value")

	_, err = Parse(`[ { "some": "object" } ]`)
	require.EqualError(t, err, "parameter 'action' cannot be '', valid values are 'view', 'broadcast', 'http' and 'copy'")

	_, err = Parse("\x00\x01\xFFx\xFE")
	require.EqualError(t, err, "invalid utf-8 string")

	_, err = Parse(`http, label, http://x.org, clear=x`)
	require.EqualError(t, err, "parameter 'clear' cannot be 'x', only boolean values are allowed (true/yes/1/false/no/0)")

}
