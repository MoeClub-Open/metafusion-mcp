package server

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
	"regexp"
	"strings"
)

var referencePath = regexp.MustCompile(`^(skills/[a-z0-9-]+/(SKILL|reference-[a-z0-9-]+)\.md|docs/[a-z0-9-]+\.md)$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func (s *Server) addSkillTools() {
	s.MCP.AddTool(&mcp.Tool{Name: "metafusion_reference", Description: "Read canonical MetaFusion skills or API manuals in the cloud. source=skills, path=skills/metafusion-curator/SKILL.md or linked reference-*.md; source=docs, path=docs/api-storage.md, docs/api-community.md, docs/api-auth.md. Source revisions are pinned by deployment. No local execution required.", InputSchema: map[string]any{"type": "object", "required": []string{"source", "path"}, "properties": map[string]any{"source": map[string]any{"type": "string", "enum": []string{"skills", "docs"}}, "path": map[string]string{"type": "string"}}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in struct {
			Source string `json:"source"`
			Path   string `json:"path"`
		}
		if e := decodeArguments(r.Params.Arguments, &in); e != nil {
			return toolError("invalid_arguments"), nil
		}
		text, revision, e := s.reference(ctx, in.Source, in.Path)
		if e != nil {
			return toolError("reference_unavailable"), nil
		}
		return result(map[string]any{"text": text, "revision": revision}, false), nil
	})
	s.MCP.AddResource(&mcp.Resource{URI: "metafusion://skills/curator", Name: "MetaFusion curator skill", MIMEType: "text/markdown"}, func(ctx context.Context, r *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		text, _, e := s.reference(ctx, "skills", "skills/metafusion-curator/SKILL.md")
		if e != nil {
			return nil, e
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "metafusion://skills/curator", MIMEType: "text/markdown", Text: text}}}, nil
	})
}
func (s *Server) reference(ctx context.Context, source, p string) (string, string, error) {
	revision := s.Config.SkillsRevision
	repo := "metafusion-skills"
	if source == "docs" {
		revision = s.Config.DocsRevision
		repo = "metafusion-docs"
	}
	if (source != "skills" && source != "docs") || !strings.HasPrefix(p, source+"/") || !referencePath.MatchString(p) || !revisionPattern.MatchString(revision) {
		return "", "", url.InvalidHostError("invalid reference")
	}
	status, b, e := s.request(ctx, "GET", "https://raw.githubusercontent.com/MoeclubM/"+repo+"/"+revision+"/"+p, "", "", nil)
	if e != nil {
		return "", "", e
	}
	if status != 200 {
		return "", "", url.InvalidHostError("reference unavailable")
	}
	return string(b), revision, nil
}
