//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package server

import (
	"net/http"
)

// OpenAPISpec represents the OpenAPI v3 specification.
type OpenAPISpec struct {
	OpenAPI    string                 `json:"openapi"`
	Info       OpenAPIInfo            `json:"info"`
	Servers    []OpenAPIServer        `json:"servers"`
	Paths      map[string]OpenAPIPath `json:"paths"`
	Components OpenAPIComponents      `json:"components"`
}

// OpenAPIInfo contains API metadata.
type OpenAPIInfo struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// OpenAPIServer describes a server.
type OpenAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// OpenAPIPath contains operations for a path.
type OpenAPIPath struct {
	Get    *OpenAPIOperation `json:"get,omitempty"`
	Post   *OpenAPIOperation `json:"post,omitempty"`
	Put    *OpenAPIOperation `json:"put,omitempty"`
	Delete *OpenAPIOperation `json:"delete,omitempty"`
}

// OpenAPIOperation describes an API operation.
type OpenAPIOperation struct {
	Summary     string                     `json:"summary"`
	Description string                     `json:"description,omitempty"`
	OperationID string                     `json:"operationId"`
	Tags        []string                   `json:"tags,omitempty"`
	Parameters  []OpenAPIParameter         `json:"parameters,omitempty"`
	RequestBody *OpenAPIRequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]OpenAPIResponse `json:"responses"`
}

// OpenAPIParameter describes a parameter.
type OpenAPIParameter struct {
	Name        string        `json:"name"`
	In          string        `json:"in"`
	Description string        `json:"description,omitempty"`
	Required    bool          `json:"required"`
	Schema      OpenAPISchema `json:"schema"`
}

// OpenAPIRequestBody describes a request body.
type OpenAPIRequestBody struct {
	Description string                      `json:"description,omitempty"`
	Required    bool                        `json:"required"`
	Content     map[string]OpenAPIMediaType `json:"content"`
}

// OpenAPIResponse describes a response.
type OpenAPIResponse struct {
	Description string                      `json:"description"`
	Content     map[string]OpenAPIMediaType `json:"content,omitempty"`
}

// OpenAPIMediaType describes a media type.
type OpenAPIMediaType struct {
	Schema OpenAPISchema `json:"schema"`
}

// OpenAPISchema describes a schema.
type OpenAPISchema struct {
	Type        string                   `json:"type,omitempty"`
	Format      string                   `json:"format,omitempty"`
	Description string                   `json:"description,omitempty"`
	Properties  map[string]OpenAPISchema `json:"properties,omitempty"`
	Items       *OpenAPISchema           `json:"items,omitempty"`
	Required    []string                 `json:"required,omitempty"`
	Default     any                      `json:"default,omitempty"`
	Ref         string                   `json:"$ref,omitempty"`
}

// OpenAPIComponents contains reusable components.
type OpenAPIComponents struct {
	Schemas map[string]OpenAPISchema `json:"schemas"`
}

// handleOpenAPI handles the GET /v1/openapi.json endpoint.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	spec := BuildOpenAPISpec()
	s.respondJSON(w, http.StatusOK, spec)
}

// BuildOpenAPISpec constructs the OpenAPI v3 specification.
// This is exported so it can be used to generate static documentation.
func BuildOpenAPISpec() OpenAPISpec {
	return OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       "pgEdge RAG Server API",
			Description: "REST API for querying RAG (Retrieval-Augmented Generation) pipelines",
			Version:     "1.0.0",
		},
		Servers: []OpenAPIServer{
			{
				URL:         "/v1",
				Description: "API v1",
			},
		},
		Paths: map[string]OpenAPIPath{
			"/health": {
				Get: &OpenAPIOperation{
					Summary:     "Health check",
					Description: "Check if the server is running and healthy",
					OperationID: "getHealth",
					Tags:        []string{"System"},
					Responses: map[string]OpenAPIResponse{
						"200": {
							Description: "Server is healthy",
							Content: map[string]OpenAPIMediaType{
								"application/json": {
									Schema: OpenAPISchema{
										Ref: "#/components/schemas/HealthResponse",
									},
								},
							},
						},
					},
				},
			},
			"/pipelines": {
				Get: &OpenAPIOperation{
					Summary:     "List pipelines",
					Description: "Get a list of all available RAG pipelines",
					OperationID: "listPipelines",
					Tags:        []string{"Pipelines"},
					Responses: map[string]OpenAPIResponse{
						"200": {
							Description: "List of pipelines",
							Content: map[string]OpenAPIMediaType{
								"application/json": {
									Schema: OpenAPISchema{
										Ref: "#/components/schemas/PipelinesResponse",
									},
								},
							},
						},
					},
				},
			},
			"/pipelines/{name}": {
				Post: &OpenAPIOperation{
					Summary:     "Query pipeline",
					Description: "Execute a RAG query against a specific pipeline",
					OperationID: "queryPipeline",
					Tags:        []string{"Pipelines"},
					Parameters: []OpenAPIParameter{
						{
							Name:        "name",
							In:          "path",
							Description: "Pipeline name",
							Required:    true,
							Schema: OpenAPISchema{
								Type: "string",
							},
						},
					},
					RequestBody: &OpenAPIRequestBody{
						Description: "Query request",
						Required:    true,
						Content: map[string]OpenAPIMediaType{
							"application/json": {
								Schema: OpenAPISchema{
									Ref: "#/components/schemas/QueryRequest",
								},
							},
						},
					},
					Responses: map[string]OpenAPIResponse{
						"200": {
							Description: "Query response",
							Content: map[string]OpenAPIMediaType{
								"application/json": {
									Schema: OpenAPISchema{
										Ref: "#/components/schemas/QueryResponse",
									},
								},
								"text/event-stream": {
									Schema: OpenAPISchema{
										Type:        "string",
										Description: "Server-Sent Events stream",
									},
								},
							},
						},
						"400": {
							Description: "Invalid request",
							Content: map[string]OpenAPIMediaType{
								"application/json": {
									Schema: OpenAPISchema{
										Ref: "#/components/schemas/ErrorResponse",
									},
								},
							},
						},
						"404": {
							Description: "Pipeline not found",
							Content: map[string]OpenAPIMediaType{
								"application/json": {
									Schema: OpenAPISchema{
										Ref: "#/components/schemas/ErrorResponse",
									},
								},
							},
						},
						"500": {
							Description: "Server error",
							Content: map[string]OpenAPIMediaType{
								"application/json": {
									Schema: OpenAPISchema{
										Ref: "#/components/schemas/ErrorResponse",
									},
								},
							},
						},
					},
				},
			},
		},
		Components: OpenAPIComponents{
			Schemas: map[string]OpenAPISchema{
				"HealthResponse": {
					Type: "object",
					Properties: map[string]OpenAPISchema{
						"status": {
							Type:        "string",
							Description: "Health status",
						},
					},
					Required: []string{"status"},
				},
				"PipelinesResponse": {
					Type: "object",
					Properties: map[string]OpenAPISchema{
						"pipelines": {
							Type:        "array",
							Description: "List of available pipelines",
							Items: &OpenAPISchema{
								Ref: "#/components/schemas/PipelineInfo",
							},
						},
					},
					Required: []string{"pipelines"},
				},
				"PipelineInfo": {
					Type: "object",
					Properties: map[string]OpenAPISchema{
						"name": {
							Type:        "string",
							Description: "Pipeline name",
						},
						"description": {
							Type:        "string",
							Description: "Pipeline description",
						},
					},
					Required: []string{"name"},
				},
				"Message": {
					Type: "object",
					Properties: map[string]OpenAPISchema{
						"role": {
							Type:        "string",
							Description: "Message role (user or assistant)",
						},
						"content": {
							Type:        "string",
							Description: "Message content",
						},
					},
					Required: []string{"role", "content"},
				},
				"QueryRequest": {
					Type: "object",
					Properties: map[string]OpenAPISchema{
						"query": {
							Type:        "string",
							Description: "The question to answer",
						},
						"stream": {
							Type:        "boolean",
							Description: "Enable streaming response (SSE)",
							Default:     false,
						},
						"top_n": {
							Type:        "integer",
							Description: "Override default result limit",
						},
						"filter": {
							Type:        "string",
							Description: "SQL WHERE clause to filter search results (e.g., \"product = 'pgAdmin' AND version = 'v9.0'\")",
						},
						"include_sources": {
							Type:        "boolean",
							Description: "Include source documents in response",
							Default:     false,
						},
						"messages": {
							Type:        "array",
							Description: "Previous conversation history for context",
							Items: &OpenAPISchema{
								Ref: "#/components/schemas/Message",
							},
						},
					},
					Required: []string{"query"},
				},
				"QueryResponse": {
					Type: "object",
					Properties: map[string]OpenAPISchema{
						"answer": {
							Type:        "string",
							Description: "The generated answer",
						},
						"sources": {
							Type:        "array",
							Description: "Source documents (only if include_sources=true)",
							Items: &OpenAPISchema{
								Ref: "#/components/schemas/Source",
							},
						},
						"tokens_used": {
							Type:        "integer",
							Description: "Total tokens consumed",
						},
					},
					Required: []string{"answer", "tokens_used"},
				},
				"Source": {
					Type: "object",
					Properties: map[string]OpenAPISchema{
						"id": {
							Type:        "string",
							Description: "Document identifier",
						},
						"content": {
							Type:        "string",
							Description: "Document content",
						},
						"score": {
							Type:        "number",
							Format:      "double",
							Description: "Relevance score",
						},
					},
					Required: []string{"content", "score"},
				},
				"ErrorResponse": {
					Type: "object",
					Properties: map[string]OpenAPISchema{
						"error": {
							Ref: "#/components/schemas/ErrorDetail",
						},
					},
					Required: []string{"error"},
				},
				"ErrorDetail": {
					Type: "object",
					Properties: map[string]OpenAPISchema{
						"code": {
							Type:        "string",
							Description: "Error code",
						},
						"message": {
							Type:        "string",
							Description: "Error message",
						},
					},
					Required: []string{"code", "message"},
				},
			},
		},
	}
}
