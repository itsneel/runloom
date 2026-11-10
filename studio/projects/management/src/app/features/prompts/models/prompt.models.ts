export interface PromptVariable {
  name: string;
  type: string;
  required: boolean;
  description?: string;
  default?: unknown;
}
export interface PromptTemplate {
  instructions: string;
  text: string;
  variables: PromptVariable[];
}
export interface PromptEntry {
  id: string;
  name: string;
  description: string;
  tags?: string[];
  revision: number;
  version: number;
  published: boolean;
  template: PromptTemplate;
  hash?: string;
  actor?: string;
  updated_at?: string;
}
