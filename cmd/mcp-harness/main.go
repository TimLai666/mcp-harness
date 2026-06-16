package main

import (
	"context"
	"log"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type harnessArgs struct {
	Message    string             `json:"message" jsonschema:"natural language instructions and optional harness tool call blocks"`
	Project    string             `json:"project,omitempty" jsonschema:"optional project id, project name, or absolute path"`
	Mode       harness.Mode       `json:"mode,omitempty" jsonschema:"inspect or work"`
	AccessMode harness.AccessMode `json:"access_mode,omitempty" jsonschema:"default, auto, or full_access"`
	SessionID  string             `json:"session_id,omitempty" jsonschema:"optional existing session id"`
}

func main() {
	runtime := harness.NewRuntime()
	server := mcp.NewServer(&mcp.Implementation{Name: "mcp-harness", Version: "0.1.0"}, nil)
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "harness",
			Description: "Run a local harness turn. The message may contain <harness_tool_call> JSON blocks.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args harnessArgs) (*mcp.CallToolResult, any, error) {
			result, err := runtime.Run(ctx, harness.RunRequest{
				Message:    args.Message,
				Project:    args.Project,
				Mode:       args.Mode,
				AccessMode: args.AccessMode,
				SessionID:  args.SessionID,
			})
			return nil, result, err
		},
	)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
