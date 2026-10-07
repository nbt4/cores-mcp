package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	coresauth "github.com/nbt4/cores-mcp/internal/authn"
	"github.com/nbt4/cores-mcp/internal/config"
)

// Position tools need a scope-specific linking signal: discovery alone does not
// tell a client which additional grant to request for a particular invocation.
func positionOAuthScopes(name string) []string {
	const prefix = "rental.job_positions."
	if !strings.HasPrefix(name, prefix) {
		return nil
	}
	operation := strings.TrimPrefix(name, prefix)
	switch operation {
	case "get", "search":
		return []string{coresauth.ReadScope(), "cores:rental:financial"}
	case "audit_history":
		return []string{coresauth.ReadScope()}
	case "prepare_create", "create", "prepare_update", "update", "prepare_archive", "archive":
		action := strings.TrimPrefix(operation, "prepare_")
		return []string{coresauth.ReadScope(), "cores:rental:financial", coresauth.ServiceWriteScope("rental", action)}
	}
	return nil
}

type oauthSecurityScheme struct {
	Type   string   `json:"type"`
	Scopes []string `json:"scopes"`
}

type oauthToolDescriptor struct {
	*mcp.Tool
	SecuritySchemes []oauthSecurityScheme `json:"securitySchemes,omitempty"`
}

// The SDK retains standard MCP tool fields; this envelope adds the documented
// ChatGPT extension and its _meta mirror without changing the registered tool.
type oauthToolsResult struct{ *mcp.ListToolsResult }

func (r *oauthToolsResult) MarshalJSON() ([]byte, error) {
	tools := make([]oauthToolDescriptor, 0, len(r.Tools))
	for _, tool := range r.Tools {
		descriptor := oauthToolDescriptor{Tool: tool}
		if scopes := positionOAuthScopes(tool.Name); len(scopes) > 0 {
			copy := *tool
			copy.Meta = make(mcp.Meta, len(tool.Meta)+1)
			for key, value := range tool.Meta {
				copy.Meta[key] = value
			}
			schemes := []oauthSecurityScheme{{Type: "oauth2", Scopes: scopes}}
			copy.Meta["securitySchemes"] = schemes
			descriptor.Tool = &copy
			descriptor.SecuritySchemes = schemes
		}
		tools = append(tools, descriptor)
	}
	return json.Marshal(struct {
		*mcp.ListToolsResult
		Tools []oauthToolDescriptor `json:"tools"`
	}{r.ListToolsResult, tools})
}

func toolOAuthMiddleware(cfg config.Config) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method == "tools/call" {
				if params, ok := request.GetParams().(*mcp.CallToolParamsRaw); ok {
					scopes := positionOAuthScopes(params.Name)
					execution := executionToolForPreparation(params.Name)
					// Unregistered writes on a read-only server remain unknown tools, rather
					// than advertising an authorization path which cannot be used there.
					if len(scopes) > 0 && (!isMutationTool(execution) || cfg.EnableWrites) {
						info := auth.TokenInfoFromContext(ctx)
						missing := []string{}
						for _, scope := range scopes {
							if info != nil && (containsString(info.Scopes, scope) || scope == requiredMutationScope(execution) && containsString(info.Scopes, coresauth.WriteScope())) {
								continue
							}
							missing = append(missing, scope)
						}
						if len(missing) > 0 {
							return positionOAuthChallenge(cfg, params.Name, info, missing), nil
						}
					}
				}
			}
			result, err := next(ctx, method, request)
			if err == nil && method == "tools/list" {
				if tools, ok := result.(*mcp.ListToolsResult); ok {
					return &oauthToolsResult{tools}, nil
				}
			}
			return result, err
		}
	}
}

func positionOAuthChallenge(cfg config.Config, name string, info *auth.TokenInfo, missing []string) *mcp.CallToolResult {
	// Keep existing valid grants so upgrading a financial permission does not
	// discard this connection's previous read/write permissions.
	requested := []string{}
	supported := coresauth.SupportedScopes(cfg.EnableWrites)
	if info != nil {
		for _, scope := range info.Scopes {
			if containsString(supported, scope) && !containsString(requested, scope) {
				requested = append(requested, scope)
			}
		}
	}
	for _, scope := range missing {
		if !containsString(requested, scope) {
			requested = append(requested, scope)
		}
	}
	code := "insufficient_scope"
	if info == nil {
		code = "invalid_token"
	}
	message := "Cores OAuth consent required for " + strings.Join(missing, " ") + ". Authenticate and explicitly allow the requested financial/read/write access, then prepare the operation again."
	challenge := fmt.Sprintf("Bearer resource_metadata=%q, error=%q, error_description=%q, scope=%q", strings.TrimRight(cfg.PublicURL, "/")+"/.well-known/oauth-protected-resource", code, message, strings.Join(requested, " "))
	return &mcp.CallToolResult{
		IsError:           true,
		Meta:              mcp.Meta{"mcp/www_authenticate": []string{challenge}},
		Content:           []mcp.Content{&mcp.TextContent{Text: message}},
		StructuredContent: Output{AsOf: time.Now().UTC().Format(time.RFC3339), Summary: message, Sources: []Source{{Service: "cores-mcp", Entity: "authorization", ID: name}}, Warnings: []string{"No business data was returned or changed. New scopes require explicit OAuth consent; existing tokens and confirmations are unchanged."}},
	}
}
