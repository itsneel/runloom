// This website has no runtime dependency on the studio, API, or identity provider.
// Set real public HTTPS URLs when publishing; blank URLs hide external links.
export const siteConfig = {
  // Local Docker Compose Studio. Set the public Studio URL before publishing.
  studioUrl: "http://localhost:24200/",
  repositoryUrl: "",
};

export interface DemoNode {
  label: string;
  kind: string;
  icon: string;
  detail: string;
}
export interface Scenario {
  id: string;
  name: string;
  description: string;
  gate: string;
  action: string;
  nodes: DemoNode[];
  steps: Record<string, unknown>[];
}

export const scenarios: Scenario[] = [
  {
    id: "onboarding",
    name: "Employee onboarding",
    description: "A warm welcome, with a human in the loop.",
    gate: "Waiting for manager approval",
    action: "Approve request",
    nodes: [
      {
        label: "New team member",
        kind: "MANUAL TRIGGER",
        icon: "bolt",
        detail:
          "Start with a name and a department. Structured inputs keep every run consistent.",
      },
      {
        label: "Prepare welcome",
        kind: "ACTIVITY",
        icon: "text",
        detail:
          "A registered activity creates the welcome message from workflow input.",
      },
      {
        label: "Manager approval",
        kind: "HUMAN APPROVAL",
        icon: "person",
        detail:
          "Work waits durably until a person approves or rejects. Click Approve request to continue this illustration.",
      },
      {
        label: "Record outcome",
        kind: "ACTIVITY",
        icon: "check",
        detail:
          "The sample echo activity records the outcome. Real provisioning requires a registered integration activity.",
      },
      {
        label: "Ready for day one",
        kind: "OUTPUT",
        icon: "flag",
        detail:
          "Return structured output and keep the run available for inspection.",
      },
    ],
    steps: [
      {
        id: "welcome",
        type: "activity",
        activity: "runloom.compose",
        with: { template: "Welcome ${input.name}!" },
      },
      {
        id: "approval",
        type: "approval",
        prompt: "Approve onboarding for ${input.name}?",
      },
      {
        id: "outcome",
        type: "activity",
        activity: "runloom.echo",
        with: { approval: "${steps.approval.output}" },
      },
    ],
  },
  {
    id: "documents",
    name: "Document handoff",
    description:
      "Keep the process moving, even when another system takes its time.",
    gate: "Waiting for the external callback",
    action: "Simulate callback",
    nodes: [
      {
        label: "Start a review",
        kind: "MANUAL TRIGGER",
        icon: "bolt",
        detail:
          "Start a review with structured input. Authenticated webhooks can also start workflows.",
      },
      {
        label: "Prepare request",
        kind: "ACTIVITY",
        icon: "text",
        detail:
          "Prepare data for the next stage. Connect real external systems using registered activities.",
      },
      {
        label: "Await documents",
        kind: "CALLBACK",
        icon: "return",
        detail:
          "Wait for a callback addressed to this run and step. This demo simulates delivery entirely in your browser.",
      },
      {
        label: "Record receipt",
        kind: "ACTIVITY",
        icon: "check",
        detail: "Pass the received callback payload into the next activity.",
      },
      {
        label: "Handoff complete",
        kind: "OUTPUT",
        icon: "flag",
        detail: "Return the result so the next person can see what happened.",
      },
    ],
    steps: [
      {
        id: "request",
        type: "activity",
        activity: "runloom.echo",
        with: { name: "${input.name}" },
      },
      {
        id: "documents",
        type: "callback",
        callback_key: "documents.received",
        prompt: "Waiting for documents",
      },
      {
        id: "receipt",
        type: "activity",
        activity: "runloom.echo",
        with: { documents: "${steps.documents.output}" },
      },
    ],
  },
  {
    id: "ai-review",
    name: "AI-assisted review",
    description: "Let AI draft. Let a person make the call.",
    gate: "Waiting for human review",
    action: "Approve draft",
    nodes: [
      {
        label: "New request",
        kind: "MANUAL TRIGGER",
        icon: "bolt",
        detail: "Supply the text to review as structured workflow input.",
      },
      {
        label: "Generate a draft",
        kind: "AI / LLM",
        icon: "spark",
        detail:
          "The product calls a configured OpenAI-compatible provider through a retryable activity. This demo does not call a model.",
      },
      {
        label: "Review the draft",
        kind: "HUMAN APPROVAL",
        icon: "person",
        detail:
          "Keep a human decision point after the AI step. Review and approve the result before continuing.",
      },
      {
        label: "Record decision",
        kind: "ACTIVITY",
        icon: "check",
        detail:
          "Store the review outcome as structured data using the sample echo activity.",
      },
      {
        label: "Review complete",
        kind: "OUTPUT",
        icon: "flag",
        detail:
          "Inspect the execution result and its activity history in the workflow studio.",
      },
    ],
    steps: [
      {
        id: "draft",
        type: "llm",
        prompt: "Draft a short response for ${input.name}.",
      },
      {
        id: "review",
        type: "approval",
        prompt: "Approve the generated draft?",
      },
      {
        id: "decision",
        type: "activity",
        activity: "runloom.echo",
        with: { review: "${steps.review.output}" },
      },
    ],
  },
];

export function definitionFor(scenario: Scenario) {
  return {
    schema_version: "1.0",
    id: scenario.id,
    name: scenario.name,
    version: 0,
    trigger: { type: "manual" },
    input_schema: {
      type: "object",
      required: ["name"],
      properties: { name: { type: "string" } },
    },
    steps: scenario.steps,
  };
}

export const faqs = [
  {
    question: "Do I have to write JSON to build a workflow?",
    answer:
      "No. The Angular studio gives you a visual canvas and guided node settings. JSON is the underlying definition, so technical teammates can also inspect, edit, and version the same workflow.",
  },
  {
    question: "What happens if the work takes hours or days?",
    answer:
      "The product uses Temporal for durable execution. Timers, approvals, callbacks, and signals can wait without keeping an HTTP request open. Correctly operated Temporal infrastructure and compatible workers are required for recovery.",
  },
  {
    question: "Can I connect AI and my own systems?",
    answer:
      "The AI node supports a configured OpenAI-compatible chat-completions provider. Callbacks, named signals, and authenticated webhooks support coordination. Additional business integrations must be implemented as registered Go activities; there is no unlimited connector catalog.",
  },
  {
    question: "Can my whole team use it?",
    answer:
      "Yes. Create organizations and teams, then assign admin, editor, or viewer access within each team. Organization owners and admins manage membership. Keycloak provides email login and password recovery; Google login needs OAuth configuration.",
  },
  {
    question: "Is this a hosted service?",
    answer:
      "This project is self-hostable, not a hosted subscription. The full platform needs the Go API and workers, Angular studio, Temporal, identity services, and application storage. Production security, backups, monitoring, and email delivery remain operator responsibilities.",
  },
  {
    question: "Is the website demo running a real workflow?",
    answer:
      "No. It is a browser-only illustration of a workflow and its approval or callback gate. It sends no requests to Temporal, an AI provider, or the product API. Your selections reset when you reload the page.",
  },
];
