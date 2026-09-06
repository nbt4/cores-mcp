package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	endpoint := flag.String("endpoint", "http://127.0.0.1:8090/mcp", "MCP endpoint")
	token := flag.String("token", "", "optional bearer token")
	flag.Parse()
	transport := &mcp.StreamableClientTransport{Endpoint: *endpoint}
	if *token != "" {
		transport.HTTPClient = &http.Client{Transport: bearerTransport{token: *token}}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "cores-mcp-smoke", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		fatal("connect", err)
	}
	defer session.Close()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		fatal("list tools", err)
	}
	failures := 0
	for _, tool := range listed.Tools {
		if strings.HasSuffix(tool.Name, ".prepare_create") || tool.Annotations != nil && !tool.Annotations.ReadOnlyHint {
			fmt.Printf("SKIP %s (requires interactive cores:write consent)\n", tool.Name)
			continue
		}
		result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool.Name, Arguments: smokeArguments(tool.Name)})
		if callErr != nil || result.IsError {
			failures++
			fmt.Printf("FAIL %s: %v %s\n", tool.Name, callErr, contentText(result.Content))
			continue
		}
		fmt.Printf("OK   %s\n", tool.Name)
	}
	prompts, err := session.ListPrompts(context.Background(), nil)
	if err != nil {
		fatal("list prompts", err)
	}
	for _, prompt := range prompts.Prompts {
		_, promptErr := session.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: prompt.Name, Arguments: map[string]string{
			"product": "Schelle", "category": "Akkuschrauber", "options": "Einhell, Makita, Hilti", "area": "suiteweit",
		}})
		if promptErr != nil {
			failures++
			fmt.Printf("FAIL prompt/%s: %v\n", prompt.Name, promptErr)
			continue
		}
		fmt.Printf("OK   prompt/%s\n", prompt.Name)
	}
	resources, err := session.ListResources(context.Background(), nil)
	if err != nil {
		fatal("list resources", err)
	}
	fmt.Printf("%d tools, %d prompts, %d resources, %d failures\n", len(listed.Tools), len(prompts.Prompts), len(resources.Resources), failures)
	if failures > 0 {
		os.Exit(1)
	}
}

func contentText(contents []mcp.Content) string {
	var values []string
	for _, content := range contents {
		if text, ok := content.(*mcp.TextContent); ok {
			values = append(values, text.Text)
		}
	}
	return strings.Join(values, " | ")
}

func smokeArguments(name string) map[string]any {
	switch {
	case name == "cores.data.dictionary" || name == "cores.data.quality" || name == "cores.operations.overview" || name == "cores.services.health" || name == "knowledge.documents.list":
		return map[string]any{}
	case strings.Contains(name, ".get"):
		return map[string]any{"id": "1"}
	case name == "inventory.coverage.check" || name == "inventory.alternatives.compare" || name == "inventory.procurement.recommendations":
		return map[string]any{"query": "Schelle", "limit": 5}
	case name == "knowledge.documents.search":
		return map[string]any{"query": "Cores", "limit": 5}
	case strings.Contains(name, ".upcoming") || strings.Contains(name, ".requirements") || strings.Contains(name, ".due") || strings.Contains(name, ".recent") || strings.Contains(name, ".expected"):
		return map[string]any{"from": "2026-01-01", "to": "2027-12-31", "limit": 5}
	case strings.Contains(name, ".summary"):
		if name == "planner.workload.summary" || name == "warehouse.utilization.summary" {
			return map[string]any{"limit": 5}
		}
		return map[string]any{"from": "2026-01-01", "to": "2027-12-31"}
	default:
		return map[string]any{"query": "", "limit": 5}
	}
}

type bearerTransport struct{ token string }

func (c bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request.Header.Set("Authorization", "Bearer "+c.token)
	return http.DefaultTransport.RoundTrip(request)
}

func fatal(step string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", step, err)
	os.Exit(1)
}
