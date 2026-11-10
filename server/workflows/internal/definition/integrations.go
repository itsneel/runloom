package definition

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/example/runloom/workflows/internal/model"
)

type IntegrationParameter struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}
type IntegrationSpec struct {
	ID            string                 `json:"id"`
	Provider      string                 `json:"provider"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Parameters    []IntegrationParameter `json:"parameters"`
	Documentation string                 `json:"documentation"`
	Mutates       bool                   `json:"mutates"`
}
type integrationRoute struct {
	IntegrationSpec
	method, endpoint string
	path             []string
	query            [][2]string
	body             any
	headers          map[string]string
}

// The catalog is code-owned and versioned with the worker. Callers cannot
// supply an endpoint, header, arbitrary operation or credential value.
func integrationRoutes() []integrationRoute {
	text := func(name, label string) IntegrationParameter {
		return IntegrationParameter{name, label, "string", true}
	}
	cursor := IntegrationParameter{"cursor", "Next-page token (optional)", "string", false}
	return []integrationRoute{
		{IntegrationSpec: IntegrationSpec{"sheets-read", "Google Sheets", "Read cells", "Read one bounded range; spreadsheet access token required.", []IntegrationParameter{text("spreadsheet", "Spreadsheet ID"), text("range", "A1 range")}, "https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/get", false}, method: "GET", endpoint: "https://sheets.googleapis.com/v4/spreadsheets/{spreadsheet}/values/{range}", path: []string{"spreadsheet", "range"}},
		{IntegrationSpec: IntegrationSpec{"sheets-append", "Google Sheets", "Append rows", "Appends RAW values without interpreting formulas. One attempt.", []IntegrationParameter{text("spreadsheet", "Spreadsheet ID"), text("range", "A1 range"), {"values", "Rows (JSON array of arrays)", "array", true}}, "https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/append", true}, method: "POST", endpoint: "https://sheets.googleapis.com/v4/spreadsheets/{spreadsheet}/values/{range}:append?valueInputOption=RAW&insertDataOption=INSERT_ROWS", path: []string{"spreadsheet", "range"}, body: map[string]any{"values": "${parameters.values}", "majorDimension": "ROWS"}},
		{IntegrationSpec: IntegrationSpec{"drive-list", "Google Drive", "List files", "One page of at most 100 file metadata records; does not download contents.", []IntegrationParameter{{"query", "Drive search query (optional)", "string", false}, cursor}, "https://developers.google.com/workspace/drive/api/reference/rest/v3/files/list", false}, method: "GET", endpoint: "https://www.googleapis.com/drive/v3/files?pageSize=100&fields=nextPageToken,files(id,name,mimeType)", query: [][2]string{{"query", "q"}, {"cursor", "pageToken"}}},
		{IntegrationSpec: IntegrationSpec{"gmail-list", "Gmail", "List messages", "One page of at most 100 message IDs. Requires a Gmail access token.", []IntegrationParameter{{"query", "Gmail search query (optional)", "string", false}, cursor}, "https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages/list", false}, method: "GET", endpoint: "https://gmail.googleapis.com/gmail/v1/users/me/messages?maxResults=100", query: [][2]string{{"query", "q"}, {"cursor", "pageToken"}}},
		{IntegrationSpec: IntegrationSpec{"calendar-list", "Google Calendar", "List events", "One page of at most 100 events.", []IntegrationParameter{text("calendar", "Calendar ID (primary for your calendar)"), cursor}, "https://developers.google.com/workspace/calendar/api/v3/reference/events/list", false}, method: "GET", endpoint: "https://www.googleapis.com/calendar/v3/calendars/{calendar}/events?maxResults=100", path: []string{"calendar"}, query: [][2]string{{"cursor", "pageToken"}}},
		{IntegrationSpec: IntegrationSpec{"slack-send", "Slack", "Send message", "Requires a bot token with chat:write and access to the channel.", []IntegrationParameter{text("channel", "Channel ID"), text("text", "Message")}, "https://docs.slack.dev/reference/methods/chat.postMessage/", true}, method: "POST", endpoint: "https://slack.com/api/chat.postMessage", body: map[string]any{"channel": "${parameters.channel}", "text": "${parameters.text}", "unfurl_links": false, "unfurl_media": false}},
		{IntegrationSpec: IntegrationSpec{"teams-send", "Microsoft Teams", "Send channel message", "Requires a delegated Graph token with channel message permission; ordinary app-only sending is not supported.", []IntegrationParameter{text("team", "Microsoft team ID"), text("channel", "Channel ID"), text("text", "Message")}, "https://learn.microsoft.com/en-us/graph/api/channel-post-messages?view=graph-rest-1.0", true}, method: "POST", endpoint: "https://graph.microsoft.com/v1.0/teams/{team}/channels/{channel}/messages", path: []string{"team", "channel"}, body: map[string]any{"body": map[string]any{"contentType": "text", "content": "${parameters.text}"}}},
		{IntegrationSpec: IntegrationSpec{"outlook-send", "Microsoft Outlook", "Send email", "Sends from the delegated signed-in user's mailbox. Requires Mail.Send; acceptance is not proof of delivery.", []IntegrationParameter{text("to", "Recipient email"), text("subject", "Subject"), text("text", "Message")}, "https://learn.microsoft.com/en-us/graph/api/user-sendmail?view=graph-rest-1.0", true}, method: "POST", endpoint: "https://graph.microsoft.com/v1.0/me/sendMail", body: map[string]any{"message": map[string]any{"subject": "${parameters.subject}", "body": map[string]any{"contentType": "Text", "content": "${parameters.text}"}, "toRecipients": []any{map[string]any{"emailAddress": map[string]any{"address": "${parameters.to}"}}}}, "saveToSentItems": true}},
		{IntegrationSpec: IntegrationSpec{"notion-search", "Notion", "Search pages", "Search shared content by title, returning one page. Requires a Notion integration token.", []IntegrationParameter{{"query", "Title search (optional)", "string", false}, cursor}, "https://developers.notion.com/reference/post-search", false}, method: "POST", endpoint: "https://api.notion.com/v1/search", headers: map[string]string{"Notion-Version": "2026-03-11"}, body: map[string]any{"page_size": 100}},
		{IntegrationSpec: IntegrationSpec{"hubspot-contacts", "HubSpot", "List contacts", "One page of at most 100 contacts. Requires a token with contact read access.", []IntegrationParameter{cursor}, "https://developers.hubspot.com/docs/api-reference/legacy/crm/objects/contacts/guide", false}, method: "GET", endpoint: "https://api.hubapi.com/crm/v3/objects/contacts?limit=100", query: [][2]string{{"cursor", "after"}}},
	}
}

func IntegrationCatalog() []IntegrationSpec {
	out := []IntegrationSpec{}
	for _, route := range integrationRoutes() {
		out = append(out, route.IntegrationSpec)
	}
	return out
}

func ValidateIntegrationResponse(operation string, output map[string]any) error {
	if operation == "slack-send" {
		body, _ := output["body"].(map[string]any)
		if body["ok"] != true {
			return fmt.Errorf("Slack rejected the message; verify token permissions and channel access")
		}
	}
	return nil
}
func integrationRouteFor(id string) (integrationRoute, error) {
	for _, route := range integrationRoutes() {
		if route.ID == id {
			return route, nil
		}
	}
	return integrationRoute{}, fmt.Errorf("unsupported integration operation")
}
func ValidateIntegration(in *model.Integration, resolved bool) error {
	if in == nil || !idPattern.MatchString(in.Secret) {
		return fmt.Errorf("integration requires a named team secret, not a token value")
	}
	route, err := integrationRouteFor(in.Operation)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, p := range route.Parameters {
		allowed[p.Name] = true
		value, present := in.Parameters[p.Name]
		if !present {
			if p.Required {
				return fmt.Errorf("integration parameter %s is required", p.Name)
			}
			continue
		}
		if text, ok := value.(string); ok {
			if !resolved && referencePattern.MatchString(text) {
				continue
			}
			if p.Type != "string" || len(text) > 16000 || strings.ContainsRune(text, 0) || (p.Required && strings.TrimSpace(text) == "") {
				return fmt.Errorf("invalid integration parameter %s", p.Name)
			}
		} else if p.Type == "array" {
			rows, ok := value.([]any)
			if !ok || len(rows) > 1000 {
				return fmt.Errorf("%s must be an array of at most 1000 rows", p.Name)
			}
			for _, raw := range rows {
				row, ok := raw.([]any)
				if !ok || len(row) > 1000 {
					return fmt.Errorf("sheet rows must be arrays of at most 1000 cells")
				}
				for _, cell := range row {
					switch cell.(type) {
					case nil, string, bool, float64, int, int64:
					default:
						return fmt.Errorf("sheet cells must be scalar JSON values")
					}
				}
			}
		} else {
			return fmt.Errorf("integration parameter %s must be text", p.Name)
		}
	}
	for name := range in.Parameters {
		if !allowed[name] {
			return fmt.Errorf("unsupported integration parameter %s", name)
		}
	}
	_, err = DataResultBound(in.Parameters)
	return err
}

func BuildIntegration(in model.Integration) (model.HTTPRequest, error) {
	if err := ValidateIntegration(&in, true); err != nil {
		return model.HTTPRequest{}, err
	}
	route, _ := integrationRouteFor(in.Operation)
	endpoint := route.endpoint
	for _, name := range route.path {
		value := in.Parameters[name].(string)
		if value == "." || value == ".." {
			return model.HTTPRequest{}, fmt.Errorf("invalid resource identifier")
		}
		endpoint = strings.ReplaceAll(endpoint, "{"+name+"}", url.PathEscape(value))
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return model.HTTPRequest{}, err
	}
	query := parsed.Query()
	for _, pair := range route.query {
		if value, ok := in.Parameters[pair[0]].(string); ok && value != "" {
			query.Set(pair[1], value)
		}
	}
	parsed.RawQuery = query.Encode()
	body, err := Resolve(route.body, map[string]any{"parameters": in.Parameters})
	if err != nil {
		return model.HTTPRequest{}, err
	}
	if in.Operation == "notion-search" {
		fields := body.(map[string]any)
		if value, ok := in.Parameters["query"].(string); ok && value != "" {
			fields["query"] = value
		}
		if value, ok := in.Parameters["cursor"].(string); ok && value != "" {
			fields["start_cursor"] = value
		}
	}
	h := model.HTTPRequest{URL: parsed.String(), Method: route.method, Headers: route.headers, Body: body, SecretHeaders: map[string]model.SecretHeader{"Authorization": {Secret: in.Secret, Prefix: "Bearer "}}}
	return h, ValidateHTTP(&h)
}
