export interface IntegrationOperation {
  id: string;
  provider: string;
  name: string;
  description: string;
  documentation: string;
  mutates: boolean;
  parameters: Array<{
    name: string;
    label: string;
    type: 'string' | 'array';
    required: boolean;
  }>;
}

export interface NodeCatalog {
  integrations: IntegrationOperation[];
}
