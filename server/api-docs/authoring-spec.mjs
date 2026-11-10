// Reviewed draft authoring contract; independent API documentation source.
export const authoringPath = {
  "get": {
    "operationId": "getAuthoringContext",
    "summary": "Read a root and its authorized child graph",
    "description": "Required access: team viewer/editor/admin. Returns structural outlines and a revision by default. With X-Runloom-Authoring-Context: full, also returns the saved root and authorized reachable child definitions, including prompts, code and input mappings. Does not read credential stores or database connection records. Definition content is sensitive and is sent to the configured model by Lumi. Context is limited to 64 KiB.",
    "parameters": [
      {"name":"X-Runloom-Authoring-Context","in":"header","required":false,"schema":{"type":"string","enum":["full"]},"description":"Opt in to full authorized definition content on context reads. Older callers remain structural-only."},
      {
        "name": "id",
        "in": "path",
        "required": true,
        "schema": {
          "type": "string"
        }
      },
      {
        "name": "X-Runloom-Team",
        "in": "header",
        "required": true,
        "schema": {
          "type": "string"
        }
      }
    ],
    "responses": {
      "200": {
        "description": "Safe structural graph or committed draft-only receipt"
      },
      "400": {
        "description": "Invalid request or missing explicit approval"
      },
      "401": {
        "description": "Human Studio authentication required"
      },
      "403": {
        "description": "Insufficient team permissions"
      },
      "409": {
        "description": "Stale graph or request ID reused with changed content"
      },
      "422": {
        "description": "Invalid child proposal or resulting graph"
      },
      "423": {
        "description": "A graph draft has an active editor or write reservation"
      },
      "503": {
        "description": "Dependency unavailable; retry identical apply request"
      }
    }
  },
  "post": {
    "operationId": "applyReviewedAuthoring",
    "summary": "Apply a reviewed child attachment or selected-workflow draft update",
    "description": "Required access: team editor/admin. Explicit approval and exact graph revision required. Durable request_id binds actor, root and proposal. Replays return the same receipt. Reserves all mutable graph definitions. PostgreSQL or replica-set MongoDB required. Writes drafts, commit history, project assignment and audit atomically. Does not publish or execute. Child version 0 must be explicitly published and pinned before parent execution. With proposal.operation=update and a complete proposal.definition, replaces only the selected root draft while preserving its identity, kind and project. The existing child-attachment shape remains supported. Published snapshots and run histories are unchanged. restore=true restores a prior root draft (including incomplete node configuration) and requires expected_parent_hash, or expected_definition for older receipts. The owner compares the applied draft under the edit reservation before writing; newer saved drafts return 409. Restore writes a new history/audit entry. For fingerprint-anchored retries, request identity excludes the freshly read graph revision, but still binds the actor, root, expected draft and exact restoration content. Receipts include parent_hash for subsequent restoration. Request bodies are bounded to 128 KiB.",
    "parameters": [
      {
        "name": "id",
        "in": "path",
        "required": true,
        "schema": {
          "type": "string"
        }
      },
      {
        "name": "X-Runloom-Team",
        "in": "header",
        "required": true,
        "schema": {
          "type": "string"
        }
      }
    ],
    "responses": {
      "200": {
        "description": "Safe structural graph or committed draft-only receipt"
      },
      "400": {
        "description": "Invalid request or missing explicit approval"
      },
      "401": {
        "description": "Human Studio authentication required"
      },
      "403": {
        "description": "Insufficient team permissions"
      },
      "409": {
        "description": "Stale graph or request ID reused with changed content"
      },
      "422": {
        "description": "Invalid workflow configuration, forbidden reference or resulting graph"
      },
      "423": {
        "description": "A graph draft has an active editor or write reservation"
      },
      "503": {
        "description": "Dependency unavailable; retry identical apply request"
      }
    },
    "requestBody": {
      "required": true,
      "content": {
        "application/json": {
          "schema": {
            "type": "object",
            "additionalProperties": false,
            "required": [
              "request_id",
              "revision",
              "approved",
              "proposal"
            ],
            "properties": {
              "restore":{"type":"boolean","default":false},
              "expected_parent_hash":{"type":"string","minLength":64,"maxLength":64},
              "expected_definition":{"type":"object","description":"Full applied definition for matching an older receipt without parent_hash."},
              "request_id": {
                "type": "string",
                "format": "uuid"
              },
              "revision": {
                "type": "string",
                "minLength": 64,
                "maxLength": 64
              },
              "approved": {
                "const": true
              },
              "proposal": {
                "type": "object",
                "additionalProperties": false,
                "properties": {
                  "name": {
                    "type": "string",
                    "maxLength": 160
                  },
                  "before_step_id": {
                    "type": "string"
                  },
                  "steps": {
                    "type": [
                      "array",
                      "null"
                    ],
                    "minItems": 1,
                    "items": {
                      "type": "object",
                      "description": "Runloom node definition validated against the execution node contract before any writes."
                    }
                  },
                  "operation": {
                    "type": "string",
                    "enum": [
                      "create",
                      "update"
                    ]
                  },
                  "definition": {
                    "type": "object",
                    "description": "Complete workflow definition using the workflow contract. Required for create/update. Draft child references may retain version 0."
                  }
                },
                "anyOf": [
                  {
                    "required": [
                      "name",
                      "steps",
                      "before_step_id"
                    ],
                    "not": {
                      "required": [
                        "operation"
                      ]
                    }
                  },
                  {
                    "required": [
                      "operation",
                      "definition"
                    ]
                  }
                ]
              }
            }
          }
        }
      }
    }
  }
};

export const authoringCreatePath = structuredClone(authoringPath);
authoringCreatePath.get.operationId = "getWorkflowCreationContext";
authoringCreatePath.get.summary = "Read default-project workflow creation context";
authoringCreatePath.get.description = "Required access: team viewer/editor/admin. Returns an empty root and a creation revision for the current authenticated team. No existing workflow definitions are exposed.";
authoringCreatePath.post.operationId = "applyWorkflowCreation";
authoringCreatePath.post.summary = "Atomically create a reviewed standalone main workflow draft";
authoringCreatePath.post.description = "Required access: team editor/admin. Accepts only proposal.operation=create with complete definition, explicit approval, creation revision and UUID request_id. Assigns workflow-<request_id> in the default project. Draft, commit, project assignment, audit and idempotency receipt commit atomically. No publish or execution. PostgreSQL or replica-set MongoDB required.";
for (const operation of Object.values(authoringCreatePath)) operation.parameters = operation.parameters.filter(parameter => parameter.in !== "path");
