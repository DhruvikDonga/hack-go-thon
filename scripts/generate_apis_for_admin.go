package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// APIEndpoint represents a single router endpoint for the admin explorer.
type APIEndpoint struct {
	ID          string            `json:"id"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Summary     string            `json:"summary"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Tags        []string          `json:"tags"`
	Auth        string            `json:"auth"` // "open", "token", "api_key"
	AuthLevel   int               `json:"auth_level"`
	Params      []ParamInfo       `json:"params"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body,omitempty"`
	Handler     string            `json:"handler,omitempty"`
	SourceLine  int               `json:"source_line,omitempty"`
}

// ParamInfo represents a path or query parameter.
type ParamInfo struct {
	Name        string `json:"name"`
	In          string `json:"in"` // "path", "query", "header"
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Default     string `json:"default,omitempty"`
}

var httpMethods = map[string]bool{
	"GET":     true,
	"POST":    true,
	"PUT":     true,
	"DELETE":  true,
	"PATCH":   true,
	"OPTIONS": true,
	"HEAD":    true,
}

func main() {
	rootDir := "."
	if len(os.Args) > 1 && os.Args[1] != "" {
		rootDir = os.Args[1]
	}

	routerPath := filepath.Join(rootDir, "internal", "api", "router.go")
	handlerDir := filepath.Join(rootDir, "internal", "api", "handler")
	outputPath := filepath.Join(rootDir, "web", "admin_apis.json")

	if _, err := os.Stat(routerPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: router.go not found at %s\n", routerPath)
		os.Exit(1)
	}

	fset := token.NewFileSet()

	// 1. Parse Handlers to index handler method doc comments
	handlerDocs := make(map[string]string)
	if entries, err := os.ReadDir(handlerDir); err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
				hPath := filepath.Join(handlerDir, entry.Name())
				if hFile, err := parser.ParseFile(fset, hPath, nil, parser.ParseComments); err == nil {
					for _, decl := range hFile.Decls {
						if fn, ok := decl.(*ast.FuncDecl); ok && fn.Doc != nil {
							var recvType string
							if fn.Recv != nil && len(fn.Recv.List) > 0 {
								t := fn.Recv.List[0].Type
								if star, ok := t.(*ast.StarExpr); ok {
									if ident, ok := star.X.(*ast.Ident); ok {
										recvType = ident.Name
									}
								} else if ident, ok := t.(*ast.Ident); ok {
									recvType = ident.Name
								}
							}
							key := fn.Name.Name
							if recvType != "" {
								key = recvType + "." + fn.Name.Name
							}
							handlerDocs[key] = fn.Doc.Text()
						}
					}
				}
			}
		}
	}

	// 2. Parse router.go
	routerFile, err := parser.ParseFile(fset, routerPath, nil, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse router.go: %v\n", err)
		os.Exit(1)
	}

	// Collect all comments in router.go indexed by end line
	commentsByLine := make(map[int]string)
	for _, cg := range routerFile.Comments {
		endLine := fset.Position(cg.End()).Line
		commentsByLine[endLine] = cg.Text()
	}

	groupPrefixes := map[string]string{
		"engine": "",
		"r":      "",
	}
	groupAuth := make(map[string]string)
	groupLevel := make(map[string]int)

	var endpoints []APIEndpoint

	// Helper to resolve string literal from expression
	getStringLit := func(expr ast.Expr) string {
		if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			s, _ := strconv.Unquote(lit.Value)
			return s
		}
		return ""
	}

	// Helper to inspect expression for middleware
	detectAuthInExpr := func(args []ast.Expr) (string, int) {
		for _, arg := range args {
			var buf bytes.Buffer
			printer.Fprint(&buf, fset, arg)
			str := buf.String()
			if strings.Contains(str, "JWTAuth") {
				return "token", 1
			}
			if strings.Contains(str, "RequireAuthLevel") {
				re := regexp.MustCompile(`RequireAuthLevel\((\d+)\)`)
				m := re.FindStringSubmatch(str)
				if len(m) > 1 {
					lvl, _ := strconv.Atoi(m[1])
					return "token", lvl
				}
				return "token", 50
			}
			if strings.Contains(str, "APIKeyAuth") {
				return "api_key", 0
			}
		}
		return "", 0
	}

	// Helper to clean URL paths
	cleanPath := func(p string) string {
		p = strings.ReplaceAll(p, "//", "/")
		if len(p) > 1 && strings.HasSuffix(p, "/") {
			p = strings.TrimSuffix(p, "/")
		}
		return p
	}

	// Walk statements inside SetupRouter
	ast.Inspect(routerFile, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			// e.g. v1 := engine.Group("/api/v1") or sfu := webrtcGroup.Group("/sfu")
			if len(node.Lhs) == 1 && len(node.Rhs) == 1 {
				varName := ""
				if ident, ok := node.Lhs[0].(*ast.Ident); ok {
					varName = ident.Name
				}
				if call, ok := node.Rhs[0].(*ast.CallExpr); ok && varName != "" {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Group" {
						parentName := ""
						if pIdent, ok := sel.X.(*ast.Ident); ok {
							parentName = pIdent.Name
						}
						subPath := ""
						if len(call.Args) > 0 {
							subPath = getStringLit(call.Args[0])
						}
						parentPrefix := groupPrefixes[parentName]
						prefix := cleanPath(parentPrefix + subPath)
						groupPrefixes[varName] = prefix

						// Check group-level auth
						if gAuth, gLvl := detectAuthInExpr(call.Args[1:]); gAuth != "" {
							groupAuth[varName] = gAuth
							groupLevel[varName] = gLvl
						} else if pAuth, exists := groupAuth[parentName]; exists {
							groupAuth[varName] = pAuth
							groupLevel[varName] = groupLevel[parentName]
						}
					}
				}
			}

		case *ast.ExprStmt:
			if call, ok := node.X.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					recvName := ""
					if ident, ok := sel.X.(*ast.Ident); ok {
						recvName = ident.Name
					}

					// Check group.Use(...) middleware
					if sel.Sel.Name == "Use" && recvName != "" {
						if a, lvl := detectAuthInExpr(call.Args); a != "" {
							groupAuth[recvName] = a
							if lvl > 0 {
								groupLevel[recvName] = lvl
							}
						}
					}

					// Check HTTP methods
					method := strings.ToUpper(sel.Sel.Name)
					if httpMethods[method] && len(call.Args) > 0 {
						subPath := getStringLit(call.Args[0])
						parentPrefix := groupPrefixes[recvName]
						fullPath := cleanPath(parentPrefix + subPath)
						if fullPath == "" {
							fullPath = "/"
						}

						// Skip internal preflight, wildcard, and admin html serving routes
						if method == "OPTIONS" || strings.Contains(fullPath, "*path") || fullPath == "/" || fullPath == "/admin" {
							return true
						}

						callLine := fset.Position(call.Pos()).Line

						// Find leading comment (check lines immediately preceding callLine)
						var commentText string
						for l := callLine - 1; l >= callLine-6; l-- {
							if c, ok := commentsByLine[l]; ok {
								commentText = c
								break
							}
						}

						// Handler name reference
						handlerRef := ""
						if len(call.Args) > 1 {
							lastArg := call.Args[len(call.Args)-1]
							if hSel, ok := lastArg.(*ast.SelectorExpr); ok {
								handlerRef = fmt.Sprintf("%v.%s", hSel.X, hSel.Sel.Name)
							} else if hIdent, ok := lastArg.(*ast.Ident); ok {
								handlerRef = hIdent.Name
							}
						}

						// If no leading comment on router, check handler doc comments
						if commentText == "" && handlerRef != "" {
							parts := strings.Split(handlerRef, ".")
							if len(parts) >= 2 {
								key := parts[len(parts)-2] + "." + parts[len(parts)-1]
								key = strings.TrimPrefix(key, "rc.")
								key = strings.TrimPrefix(key, "Handler.")
								for k, doc := range handlerDocs {
									if strings.HasSuffix(k, parts[len(parts)-1]) {
										commentText = doc
										break
									}
								}
							}
						}

						// Parse tags from comment
						ep := parseCommentToEndpoint(commentText, method, fullPath, handlerRef, callLine)

						// If auth wasn't explicitly set in comment, infer from arguments or group
						if ep.Auth == "" {
							if a, lvl := detectAuthInExpr(call.Args); a != "" {
								ep.Auth = a
								ep.AuthLevel = lvl
							} else if gAuth, ok := groupAuth[recvName]; ok && gAuth != "" {
								ep.Auth = gAuth
								ep.AuthLevel = groupLevel[recvName]
							} else {
								ep.Auth = "open"
							}
						}

						// Infer category if empty
						if ep.Category == "" {
							ep.Category = inferCategory(fullPath)
						}
						if len(ep.Tags) == 0 {
							ep.Tags = []string{ep.Category}
						}

						// Auto-detect path parameters like :id, :filename, etc.
						ep.Params = extractPathParams(fullPath, ep.Params)

						// Default Headers based on Auth and Method
						if ep.Headers == nil {
							ep.Headers = make(map[string]string)
						}
						if ep.Auth == "token" {
							ep.Headers["Authorization"] = "Bearer <jwt-token>"
						} else if ep.Auth == "api_key" {
							ep.Headers["X-API-Key"] = "<api-key>"
						}
						if (method == "POST" || method == "PUT" || method == "PATCH") && ep.Headers["Content-Type"] == "" {
							if strings.Contains(fullPath, "/upload") {
								ep.Headers["Content-Type"] = "multipart/form-data"
							} else {
								ep.Headers["Content-Type"] = "application/json"
							}
						}

						endpoints = append(endpoints, ep)
					}
				}
			}
		}
		return true
	})

	// Sort endpoints by category, path, then method
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].Category != endpoints[j].Category {
			return endpoints[i].Category < endpoints[j].Category
		}
		if endpoints[i].Path != endpoints[j].Path {
			return endpoints[i].Path < endpoints[j].Path
		}
		return endpoints[i].Method < endpoints[j].Method
	})

	// Serialize to JSON
	jsonData, err := json.MarshalIndent(endpoints, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal endpoints: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, jsonData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write %s: %v\n", outputPath, err)
		os.Exit(1)
	}

	// Print beautiful terminal output
	fmt.Println("==========================================================================================")
	fmt.Println("                           ADMIN ROUTER APIS GENERATOR                                    ")
	fmt.Println("==========================================================================================")
	fmt.Printf("%-7s %-42s %-12s %-16s %s\n", "METHOD", "PATH", "AUTH", "CATEGORY", "SUMMARY")
	fmt.Println("------------------------------------------------------------------------------------------")

	var openCount, tokenCount, apiKeyCount int
	for _, ep := range endpoints {
		authStr := strings.ToUpper(ep.Auth)
		if ep.Auth == "token" && ep.AuthLevel > 0 {
			authStr = fmt.Sprintf("TOKEN(L%d)", ep.AuthLevel)
		}
		switch ep.Auth {
		case "token":
			tokenCount++
		case "api_key":
			apiKeyCount++
		default:
			openCount++
		}

		summary := ep.Summary
		if len(summary) > 35 {
			summary = summary[:32] + "..."
		}
		fmt.Printf("%-7s %-42s %-12s %-16s %s\n", ep.Method, ep.Path, authStr, ep.Category, summary)
	}

	fmt.Println("==========================================================================================")
	fmt.Printf("Total APIs: %d | Open: %d | Token (JWT): %d | API Key: %d\n", len(endpoints), openCount, tokenCount, apiKeyCount)
	fmt.Printf("Saved catalog to: %s\n", outputPath)
	fmt.Println("==========================================================================================")
}

func parseCommentToEndpoint(comment, method, path, handlerRef string, line int) APIEndpoint {
	id := strings.ToLower(fmt.Sprintf("%s-%s", method, strings.ReplaceAll(strings.ReplaceAll(path, "/", "-"), ":", "")))
	id = strings.Trim(id, "-")

	ep := APIEndpoint{
		ID:         id,
		Method:     method,
		Path:       path,
		Handler:    handlerRef,
		SourceLine: line,
		Headers:    make(map[string]string),
	}

	lines := strings.Split(comment, "\n")
	var descLines []string

	for _, l := range lines {
		l = strings.TrimSpace(l)
		l = strings.TrimPrefix(l, "//")
		l = strings.TrimPrefix(l, "*")
		l = strings.TrimSpace(l)

		if l == "" {
			continue
		}

		if strings.HasPrefix(l, "@Summary") {
			ep.Summary = strings.TrimSpace(strings.TrimPrefix(l, "@Summary"))
		} else if strings.HasPrefix(l, "@Description") {
			ep.Description = strings.TrimSpace(strings.TrimPrefix(l, "@Description"))
		} else if strings.HasPrefix(l, "@Tags") || strings.HasPrefix(l, "@Category") {
			tagStr := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(l, "@Tags"), "@Category"))
			tags := strings.Split(tagStr, ",")
			for _, t := range tags {
				t = strings.TrimSpace(t)
				if t != "" {
					ep.Tags = append(ep.Tags, t)
				}
			}
			if len(ep.Tags) > 0 {
				ep.Category = ep.Tags[0]
			}
		} else if strings.HasPrefix(l, "@Auth") {
			authVal := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(l, "@Auth")))
			if strings.Contains(authVal, "token") || strings.Contains(authVal, "jwt") {
				ep.Auth = "token"
				if strings.Contains(authVal, "50") {
					ep.AuthLevel = 50
				} else if ep.AuthLevel == 0 {
					ep.AuthLevel = 1
				}
			} else if strings.Contains(authVal, "api_key") || strings.Contains(authVal, "apikey") {
				ep.Auth = "api_key"
			} else if strings.Contains(authVal, "open") || strings.Contains(authVal, "public") || strings.Contains(authVal, "none") {
				ep.Auth = "open"
			} else {
				ep.Auth = authVal
			}
		} else if strings.HasPrefix(l, "@Level") {
			lvlStr := strings.TrimSpace(strings.TrimPrefix(l, "@Level"))
			if lvl, err := strconv.Atoi(lvlStr); err == nil {
				ep.AuthLevel = lvl
			}
		} else if strings.HasPrefix(l, "@Body") {
			ep.Body = strings.TrimSpace(strings.TrimPrefix(l, "@Body"))
		} else if strings.HasPrefix(l, "@Param") {
			paramStr := strings.TrimSpace(strings.TrimPrefix(l, "@Param"))
			parts := strings.Fields(paramStr)
			if len(parts) >= 3 {
				// @Param name query string false "Description"
				pName := parts[0]
				pIn := parts[1]
				pType := parts[2]
				pReq := false
				if len(parts) >= 4 {
					pReq = strings.ToLower(parts[3]) == "true" || strings.ToLower(parts[3]) == "required"
				}
				pDesc := ""
				if len(parts) >= 5 {
					pDesc = strings.Join(parts[4:], " ")
					pDesc = strings.Trim(pDesc, "\"")
				}
				ep.Params = append(ep.Params, ParamInfo{
					Name:        pName,
					In:          pIn,
					Type:        pType,
					Required:    pReq,
					Description: pDesc,
				})
			}
		} else {
			// Plain comment line
			if ep.Summary == "" {
				ep.Summary = l
			} else {
				descLines = append(descLines, l)
			}
		}
	}

	if ep.Description == "" && len(descLines) > 0 {
		ep.Description = strings.Join(descLines, " ")
	}
	if ep.Summary == "" {
		ep.Summary = fmt.Sprintf("%s %s", method, path)
	}

	return ep
}

func inferCategory(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "api" && parts[1] == "v1" {
		parts = parts[2:]
	}
	if len(parts) == 0 {
		return "General"
	}
	switch parts[0] {
	case "health":
		return "Health"
	case "ws":
		return "WebSocket"
	case "items":
		return "Items"
	case "llm":
		return "LLM"
	case "rag":
		return "RAG"
	case "jobs":
		return "Jobs"
	case "webrtc":
		return "WebRTC"
	case "auth":
		return "Auth"
	case "users":
		return "Users"
	case "protected":
		return "Protected"
	case "webhooks":
		return "Webhooks"
	case "upload", "files":
		return "Uploads"
	case "secure":
		return "Secure"
	default:
		return strings.Title(parts[0])
	}
}

func extractPathParams(path string, existing []ParamInfo) []ParamInfo {
	existingMap := make(map[string]bool)
	for _, p := range existing {
		existingMap[p.Name] = true
	}

	re := regexp.MustCompile(`:([a-zA-Z0-9_]+)`)
	matches := re.FindAllStringSubmatch(path, -1)
	for _, m := range matches {
		if len(m) > 1 {
			paramName := m[1]
			if !existingMap[paramName] {
				existing = append(existing, ParamInfo{
					Name:        paramName,
					In:          "path",
					Type:        "string",
					Required:    true,
					Description: fmt.Sprintf("URL parameter :%s", paramName),
				})
				existingMap[paramName] = true
			}
		}
	}
	return existing
}
