# Runloom: product specifications and documentation

These instructions apply throughout this repository. Follow them when creating or updating product specifications, discovery notes, design documents, API guides, user guides, runbooks, and release documentation. Follow [CONTRIBUTING.md](CONTRIBUTING.md) for implementation and contribution conventions. More specific directory instructions may add requirements; do not silently override explicit user constraints.

## 1. Scope and authority

1. Treat a request for a specification or document as a documentation task. It does not authorize code changes, dependency installation, account creation, credential rotation, commits, pushes, or deployments.
2. Inspect the repository and relevant existing documents before writing. Preserve unrelated work and do not rewrite existing documents merely to standardize their appearance.
3. Establish the requested audience, problem, scope, and deliverable. If a missing decision materially changes the result, ask one focused question at a time. Start with the business problem, then product behavior, then technical details.
4. When the user says to assume and proceed, use reasonable assumptions, label them, and continue. Do not invent approval, evidence, ownership, deadlines, or commitments. Security-sensitive or irreversible actions still require appropriate authorization.
5. Keep the requested initiative or feature boundary literal. Put adjacent ideas in an explicitly deferred section instead of expanding the release scope.
6. Do not create extra documents, tickets, Pages, or external artifacts unless requested or necessary for the agreed deliverable. A specification is not permission to implement it.

## 2. Writing and formatting

1. Never use bullet points or unordered lists, including task-list checkboxes, in documents or responses. Prefer concise paragraphs. When a list is needed, use explicit sequential numbering: `1.`, `2.`, `3.`. Use hierarchical numbering such as `1.1.` and `1.2.` for nested items.
2. Use descriptive headings, with a blank line after each heading and before each list. Use Markdown by default unless another format is requested.
3. Write for the intended reader. Explain user-facing behavior in plain language before introducing architecture, storage, APIs, or implementation terms. Define acronyms and domain-specific terms on first use.
4. Lead with the problem, intended outcome, and important decisions. Avoid marketing filler, unsupported superlatives, and vague requirements such as “fast,” “secure,” or “user-friendly” without observable criteria.
5. Keep terminology and UI labels consistent. Use Runloom for the product and Runloom Studio for the builder. Distinguish an organization, team, project, workflow definition, draft, published version, and run.
6. Use tables only when they clarify comparisons, permissions, states, or requirement traceability. Use diagrams only when prose would obscure a meaningful sequence or relationship. Explain what a diagram establishes.
7. Use repository-relative links in repository documents and verify their targets. In chat handoffs, link to local files using absolute paths. Avoid machine-specific absolute paths in reusable instructions.
8. Use ISO dates (`YYYY-MM-DD`). Specify time zones for schedules and operational times. Avoid undated claims such as “currently” or “latest” when the information is likely to change.

## 3. Evidence and document truthfulness

1. Separate verified current behavior, proposed requirements, assumptions, and unresolved questions. Never describe a proposal as shipped merely because it appears in a specification.
2. For current behavior, inspect relevant implementation, contracts, configuration, and tests. Existing documentation is a starting point, not proof. Tests demonstrate only the cases and environments they cover; a passing mocked test is not a live integration verification.
3. If documents and implementation disagree, identify the discrepancy. Do not silently treat either an old product overview or an isolated code path as the intended product contract. Record the evidence and decision needed; fix related documentation only within the authorized scope.
4. Cite source files, contracts, test names, decisions, or primary external documentation near the claims they support. Verify external references when relying on changeable vendor capabilities, security guidance, pricing, or licensing.
5. Identify unverified statements explicitly. Use `TBD` with the question, required evidence or decision, and owner if known. An unknown owner remains `TBD`; never assign someone without evidence or authorization.
6. Do not invent user research, customer quotes, usage numbers, performance results, regulatory compliance, delivery dates, or approval history. Mark hypothetical examples as examples.
7. Record the verification date and relevant revision/environment for implementation-dependent claims. Distinguish commands actually run from commands reviewed or suggested.

## 4. Document organization

1. Keep [docs/product.md](docs/product.md) as the product-level overview. Link detailed feature specifications instead of turning the overview into an implementation log.
2. Put new feature specifications in `docs/specifications/<feature-slug>.md`, using lowercase kebab-case. Create the directory when the first specification is requested. Reuse an existing feature specification where one already owns the subject; do not create parallel sources of truth.
3. Keep user and operator guides in `docs/`, following the existing topic-based structure. Keep component-specific setup instructions in `studio/README.md`, `Website/README.md`, or the relevant component README.
4. Reserve `spec/` for machine-readable contracts, including workflow and schedule JSON schemas. A prose requirement belongs in a product specification, not a new pseudo-schema file.
5. Put substantial architectural decisions in `docs/decisions/<sequence>-<decision-slug>.md` when a separate decision record is warranted. Determine the next sequence from existing records. Include status, context, alternatives, decision, consequences, and superseding links.
6. Link a new document from the nearest relevant overview or index when this is in scope. Do not create empty directory trees, duplicate indexes, or placeholder documents solely to satisfy this layout.
7. Prefer stable filenames with revision history in the document and Git. Avoid competing `final`, `final-v2`, and `latest` copies. Use separate versioned files only when independently supported contracts require them.

## 5. Product specification requirements

Every feature specification must address the following sections. A short specification may combine related sections, but must not silently omit applicable requirements. Mark an inapplicable section `Not applicable` and explain why.

1. **Metadata and summary.** Include title, stable specification ID, document revision, status, author/owner if known, created/updated dates, intended audience, related documents, and target release if agreed. Give a short explanation of the proposed outcome.
2. **Problem and evidence.** Describe who has the problem, their current journey, the observed pain, its consequence, and why it matters. Separate supplied evidence from hypotheses. Do not start with a preferred solution and invent a problem to justify it.
3. **Users and use cases.** Identify primary and secondary personas, their permissions, prerequisites, and the jobs they need to complete. Cover non-technical builders and operators where relevant, as well as administrators and API consumers.
4. **Goals and success measures.** Define observable outcomes, how they will be measured, known baselines, and agreed targets. Label proposed targets as proposed. Distinguish product adoption/outcome measures from technical service targets.
5. **Scope and non-goals.** State what is included, excluded, and deferred. Identify dependencies, constraints, rollout boundaries, and what existing behavior must remain unchanged.
6. **User journeys and experience.** Describe entry points, navigation, default values, primary actions, completion, and recovery. Specify loading, empty, validation, permission-denied, error, success, and interrupted states. Include responsive behavior, keyboard access, focus management, clear labels, and unsaved-change handling where applicable.
7. **Functional requirements.** Give each requirement a stable ID such as `FR-001`. State the actor, preconditions, action or trigger, expected behavior, and failure behavior. Identify priority and whether it is required for this release. Use `must` for requirements and `may` for optional behavior.
8. **State and lifecycle rules.** Define allowed states, transitions, terminal states, and invalid actions. Explain concurrency, duplicate requests, retries, timeouts, cancellation, resume behavior, ownership, and recovery wherever relevant. Do not leave critical behavior implicit in a UI mockup.
9. **Data and interfaces.** Specify entities, ownership, validation, required/optional fields, defaults, limits, and retention. Link the authoritative JSON/API contracts. Document request/response examples, authentication, authorization, error behavior, idempotency, pagination, and compatibility when applicable. Label proposed endpoints and schemas as proposals.
10. **Permissions, privacy, and security.** Define access by role and tenant boundary, trusted actor attribution, secret handling, redaction, and audit events. Include abuse cases and forbidden actions, not only successful access. Identify sensitive information that could enter logs, exports, history, screenshots, or analytics.
11. **Non-functional requirements.** State measurable availability, latency, throughput, concurrency, durability, accessibility, operability, and cost requirements that actually apply. Record measurement conditions and uncertainty. Do not fabricate numeric targets to make a document look complete.
12. **Acceptance criteria and verification.** Assign stable acceptance IDs such as `AC-001` and map them to requirement IDs. Use Given/When/Then or an equivalent precise scenario. Include normal, invalid-input, denied-access, cross-tenant, duplicate, interruption, and recovery cases where applicable. State the verification method and expected result.
13. **Release and compatibility plan.** Describe migrations, feature/configuration prerequisites, rollout order, backward compatibility, rollback limitations, and user/operator communication. Separate launch readiness from permission to deploy.
14. **Risks, alternatives, and open decisions.** Explain meaningful options and tradeoffs, the chosen or proposed approach, dependencies, mitigations, and unresolved questions. Mark which questions block implementation or release and identify decision owners only when known.
15. **References and revision history.** Link evidence, designs, contracts, and related decisions. Record substantive changes with dates and rationale. Preserve superseded decisions by linking their replacements rather than silently erasing their context.

## 6. Reusable specification outline

Use this outline as a starting point, adapting depth to the feature rather than filling it with boilerplate. Replace placeholders with evidence, explicit assumptions, or actionable open questions.

```markdown
# <Feature title>

Spec ID: <stable ID>
Document revision: <revision; separate from workflow/product versions>
Status: Draft
Owner: TBD
Created: YYYY-MM-DD
Updated: YYYY-MM-DD
Audience: <readers>
Target release: TBD
Related documents: <links>

## 1. Summary

## 2. Problem, evidence, and current behavior

## 3. Users and use cases

## 4. Goals and success measures

## 5. Scope and non-goals

## 6. User journeys and UI states

## 7. Functional requirements

## 8. States and lifecycle rules

## 9. Data and interfaces

## 10. Permissions, privacy, and security

## 11. Non-functional requirements

## 12. Acceptance criteria and verification

## 13. Release, migration, and rollback

## 14. Risks, alternatives, and open decisions

## 15. References and revision history
```

For an acceptance criterion, use this form: `AC-001 (FR-001): Given <state and actor>, when <action>, then <observable result>. Verification: <method>.` Do not use “works as expected” as an acceptance result.

## 7. Supporting document rules

1. A discovery note must distinguish the problem statement, supplied evidence, confirmed facts, assumptions, unresolved decisions, and next steps. Do not promote an unvalidated idea into an approved requirement.
2. A technical design must link its product requirements, identify affected components and contracts, explain alternatives and tradeoffs, and cover failure handling, compatibility, migration, security, observability, and verification. Keep implementation detail out of the main user journey when it distracts from behavior.
3. An API guide must state the audience and credential type, version/base path, permission checks, inputs, outputs, status codes, error envelope, retry/idempotency semantics, pagination, limits, and examples as applicable. Do not imply Studio and consumer tokens are interchangeable.
4. A user guide must use actual UI labels, prerequisites, ordered steps, expected outcomes, and recovery instructions. Disclose when a test sends real email, calls an external API, incurs model costs, or otherwise causes real side effects.
5. An operational runbook must state the environment, prerequisites, access required, working directory, exact commands, configuration sources, expected results, verification, rollback/recovery, and stop conditions. Identify destructive or data-changing steps before presenting them.
6. A release note must distinguish delivered behavior, fixes, known limitations, breaking changes, migrations, and required operator action. An approved specification or merged change alone is not proof that a feature has been deployed.
7. Examples must be minimal, realistic, and consistent with the stated contract. Use synthetic data and placeholders. Never copy real tokens, client secrets, private keys, reset links, or customer payloads into documentation. Public local-only defaults belong only in clearly labeled local setup guidance.
8. Explain environment differences. Do not present a developer's currently running port, cached image, database selection, or account as a universal installation default. Check commands against the current Makefile and configuration without running side-effecting commands just to validate prose.

## 8. Runloom-specific documentation checks

1. Verify the current repository structure before referencing paths. The established components are the Go backend in `server/`, Angular Studio in `studio/`, and independent Angular product website in `Website/`. Do not reintroduce the retired Python implementation or old directory names as current architecture.
2. Cross-check changes to workflow behavior against [the JSON contract](docs/json-contract.md), [workflow schema](spec/workflow.schema.json), backend validation, Studio models, and relevant examples. A prose specification does not itself add a supported node or schema field.
3. Distinguish draft saving, commit history, publishing an immutable version, restoring as a new draft, and starting a run. Do not confuse specification revision numbers with workflow version numbers or execution history.
4. Specify timer waits, callbacks, named signals, approvals, pauses, resumes, cancellation, and completion separately when relevant. Document who can act, what identifies the target, what happens on invalid or repeated actions, and what survives interruption; verify behavior rather than assuming these operations are equivalent.
5. Consult [schedules and subworkflows](docs/schedules-and-subworkflows.md) before describing scheduled starts or child execution. Distinguish production parent-driven child execution from authenticated independent Studio child tests, and explain version pinning and real test side effects.
6. Keep [Studio authentication](docs/authentication.md) and the [consumer execution API](docs/consumer-api.md) distinct. Describe organization/team scope, service-account grants, owned-run visibility, credential lifecycle, and audit identity without implying broader access than is implemented.
7. Do not repeat historical blanket claims that workflow definitions cannot contain code or external API configuration. Check the current [JavaScript Custom Code](docs/custom-code.md) and [External API/secrets](docs/api-nodes-and-secrets.md) contracts and restrictions. Credentials must not be embedded in workflow examples.
8. Distinguish application database records and audit history from Temporal replay history. Check MongoDB/PostgreSQL support, retention, and redaction claims against current implementation rather than promising identical operational guarantees without evidence.
9. For AI/LLM features, distinguish reported usage, estimates, pricing configuration, unknown cost, retries, and parent/child aggregation. Do not present estimated cost as a provider invoice or treat missing usage as zero. Consult [LLM accounting](docs/llm-costs.md).
10. Keep product website claims aligned with supported behavior and documented limitations. Preserve the website's independent build/hosting boundary; do not add Docker or Helm requirements for it through documentation alone.

## 9. Review, status, and completion

1. Use explicit document statuses: Draft, In review, Approved, Superseded, or Archived. Record the approver and date only after actual approval. Approval of a document is not proof of implementation, testing, or deployment.
2. Before declaring a specification ready for review, verify that scope is bounded, requirements are testable, acceptance criteria cover the requirements, contradictions are resolved or exposed, and blocking questions are visible. A review draft may retain clearly identified open questions.
3. Before handing off any document, check paths and links, terminology, numbered-list formatting, examples, commands, UI labels, and consistency with related contracts. Run relevant lightweight checks such as `git diff --check`; report what was and was not validated.
4. Documentation-only work normally does not require rebuilding applications or starting services. Do not run migrations, live workflow tests, deployment commands, or credential provisioning merely to check a document.
5. Review the final diff for unrelated changes, accidental secrets, unsupported claims, and duplicated sources of truth. When behavior changes are authorized separately, update the affected specification, user/API guides, examples, and operational guidance together.
6. In the handoff, link the files created or changed, summarize important decisions, identify unresolved blockers, and report verification honestly. Do not claim a feature is implemented because its specification is complete.

## 10. Staging deployment is a separate process

Writing, reviewing, or approving documentation never starts a staging deployment. Follow the process below only when a staging deployment is explicitly requested. Resolve the target repository, working branch, staging branch, and JX staging repository first; if those are unknown, ask rather than inventing targets. Do not discard uncommitted work or force-push without the appropriate explicit authority.

1. Standard staging deployment.
   1.1. Pull the latest staging branch from remote.
   1.2. Merge the current working branch into the staging branch.
   1.3. Push the updated staging branch to remote.
   1.4. Monitor the JX staging repository.
   1.5. Once a PR for the current project is raised in the JX staging repository, monitor it until the run verify check completes successfully.
   1.6. Merge that JX staging repository PR only after the run verify check passes.
2. Force staging deployment.
   2.1. Start from a fresh main or master branch.
   2.2. Check out or recreate the staging branch from that fresh main or master branch.
   2.3. Merge the current working branch into the staging branch.
   2.4. Push the updated staging branch to remote.
   2.5. Monitor the JX staging repository.
   2.6. Once a PR for the current project is raised in the JX staging repository, monitor it until the run verify check completes successfully.
   2.7. Merge that JX staging repository PR only after the run verify check passes.
