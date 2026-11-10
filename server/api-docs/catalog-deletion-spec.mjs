// Authoring operations and catalog metadata must survive documentation regeneration.
export const deletionPaths = {
  '/definitions/{id}': {
    delete: {
      operationId: 'deleteDraftWorkflow',
      tags: ['Workflows'],
      summary: 'Delete a never-published workflow',
      description:
        'Required access: team editor/admin. Removes the saved draft and project membership only if no published version exists. A published workflow with newer draft edits is not eligible. Commit, run and audit history are retained. Active editing leases are enforced; no token is needed when there is no active editor. Returns 404 on repeat deletion. Security audit completion errors may report an unknown outcome; refresh the workflow list before retrying.',
      'x-required-role': 'team editor/admin',
      parameters: [
        {
          name: 'id',
          in: 'path',
          required: true,
          schema: {
            type: 'string',
          },
        },
        {
          name: 'X-Runloom-Team',
          in: 'header',
          required: true,
          schema: {
            type: 'string',
          },
        },
        {
          name: 'X-Runloom-Edit-Token',
          in: 'header',
          required: false,
          description: 'Owned editing-session token when an editor holds this workflow.',
          schema: {
            type: 'string',
          },
        },
      ],
      responses: {
        204: {
          description: 'Draft removed; retained histories are unchanged',
        },
        401: {
          description: 'Missing or invalid Studio credentials',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
        403: {
          description: 'Viewer or unauthorized team',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
        404: {
          description: 'Workflow not found',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
        409: {
          description: 'Workflow has a published version',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
        422: {
          description: 'Storage operation failed',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
        423: {
          description: 'Editing lease is held by another session or expired',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
        503: {
          description: 'Security audit unavailable or outcome unknown',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
      },
    },
  },
  '/projects/{project}': {
    delete: {
      operationId: 'deleteEmptyProject',
      tags: ['Projects'],
      summary: 'Delete an empty project',
      description:
        'Required access: team editor/admin. The project must contain zero workflows, including saved drafts. The built-in default project cannot be deleted. Membership and deletion share the same compare-and-swap revision. Retains run and audit history. Returns 404 on repeat deletion.',
      'x-required-role': 'team editor/admin',
      parameters: [
        {
          name: 'project',
          in: 'path',
          required: true,
          schema: {
            type: 'string',
          },
        },
        {
          name: 'X-Runloom-Team',
          in: 'header',
          required: true,
          schema: {
            type: 'string',
          },
        },
      ],
      responses: {
        204: {
          description: 'Empty project deleted',
        },
        401: {
          description: 'Missing or invalid Studio credentials',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
        403: {
          description: 'Viewer or unauthorized team',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
        404: {
          description: 'Project not found in this team',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
        409: {
          description: 'Project contains workflows or is the built-in default',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
        503: {
          description: 'Storage or audit unavailable; refresh before retrying',
          content: {
            'application/json': {
              schema: {
                $ref: '#/components/schemas/Error',
              },
            },
          },
        },
      },
    },
  },
};

export const integrationCatalog = {
  type: 'array',
  description:
    'Worker-allowlisted token-based actions. Fixed public HTTPS endpoints; no OAuth provisioning or automatic token refresh. Mutating operations also affect external systems during tests.',
  items: {
    type: 'object',
    required: ['id', 'provider', 'name', 'description', 'parameters', 'documentation', 'mutates'],
    properties: {
      id: {
        type: 'string',
      },
      provider: {
        type: 'string',
      },
      name: {
        type: 'string',
      },
      description: {
        type: 'string',
      },
      documentation: {
        type: 'string',
        format: 'uri',
      },
      mutates: {
        type: 'boolean',
      },
      parameters: {
        type: 'array',
        items: {
          type: 'object',
          required: ['name', 'label', 'type', 'required'],
          properties: {
            name: {
              type: 'string',
            },
            label: {
              type: 'string',
            },
            type: {
              type: 'string',
              enum: ['string', 'array'],
            },
            required: {
              type: 'boolean',
            },
          },
          additionalProperties: false,
        },
      },
    },
    additionalProperties: false,
  },
};
