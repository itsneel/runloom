# LLM run costs and usage

Open a run in Studio to see **Cost & Usage**: estimated USD spend, input/output tokens, direct versus child-workflow subtotals, node totals and individual provider requests. Expand a request for attempt number, duration, cached/reasoning subsets, provider request ID and the saved pricing version. Run history shows a cost label and filters its latest 20 loaded runs by workflow, version, model, UTC date and recorded test classification.

## Cost analytics dashboard

Choose **Cost analytics** in Studio's left sidebar. It reads the usage ledger across your authorized teams, independently of the latest-20 run-history view. Organization owners and admins can compare every team in the selected organization. Team admins, editors, and viewers can compare only teams they currently belong to; team admin status does not grant organization-wide access. All dashboard operations are read-only. Consumer service-account credentials are rejected. Team permissions are checked on the server before opening stores and again before returning results, including when a membership is revoked during a request.

Select **All accessible teams** or a specific team. Switching the global organization clears old results; switching the global team selects that team in the dashboard. The run classification selector uses the recorded test flag: **Not marked as tests** is not a guarantee that a historical/manual run was a production execution.

| Period | Included request start times |
| --- | --- |
| 1 day | Previous 24 hours; hourly buckets |
| 1 week | Previous seven days; daily buckets |
| 1 month | Previous calendar month interval; daily buckets |
| MTD | UTC start of this month to now; daily buckets |
| YTD | UTC start of this year to now; monthly buckets |
| 6 months | Previous six calendar months; monthly buckets |
| 1 year | Previous twelve calendar months; monthly buckets |

All ranges use UTC, include the start, exclude the end, and end at the last whole second. Month subtraction preserves the day where possible and clamps to the target month's last day otherwise. First/last chart buckets may be partial. Comparison uses the immediately preceding interval of equal duration, not an entire preceding calendar month/year. Percentage change is shown only when both intervals have recorded, fully priced requests and the preceding estimate is nonzero. Refresh advances the range; pagination and sorting retain its end time. Accounting may still finalize between requests: this is a live report, not an immutable invoice snapshot.

| KPI | Exact meaning |
| --- | --- |
| Estimated spend | Sum of recorded estimated request costs; decimal USD, including priced failed attempts and retries |
| Recorded tokens | Input plus output tokens; cached/reasoning subsets are not added again |
| Metered runs | Distinct team/workflow/Temporal-run combinations with requests in the range; excludes runs without ledger records |
| Average priced spend / run | Recorded estimated spend divided by runs with at least one priced request; incomplete runs contribute only their known subtotal |
| Provider requests | Recorded attempts, with separate failed and retry counts; a retry is an attempt greater than one |
| Pricing coverage | Priced requests divided by all recorded requests, not the fraction of actual provider spend recovered |

The dashboard includes a spend trend with an accessible data table and **Runs**, **Teams**, and **Models** breakdowns. Click column headers to sort by name, estimated spend, requests, tokens, run count, or last request time. Sorting happens on the server before pagination (50 rows per page), with a stable identifier tie-breaker. Missing cost estimates sort last in both directions. Expand a run row for its workflow and Temporal run identifiers; amounts are direct request costs, not descendant-inclusive run subtotals. Each child request contributes once to totals, never again through its parent.

No recorded requests, unpriced requests, unknown outcomes, and pending accounting are distinct states. When no request has a price, spend and the average display a dash rather than fabricated zero dollars. A genuinely priced zero remains zero. Pending intents older than one hour count as unknown in analytics; unlike the individual-run view, this aggregate does not fetch terminal execution status to reclassify more recent pending entries. A successful workflow does not prove zero or fully reconciled cost.

The report is bounded to 100 selected teams and 50,000 ledger entries across the current and comparison intervals before the test classification filter. Exceeding a limit rejects the whole report and asks for a shorter range or a specific team. An unavailable store or expired eight-second query deadline fails the report; it never returns a successful partial aggregate. Historical uninstrumented calls remain undetectable. No model calls, repricing, budget enforcement, exports, or invoice reconciliation occur when loading analytics.

### Analytics API

`GET /api/v1/studio/orgs/{org_id}/llm-analytics` requires a verified human Studio token. The server derives permitted teams from current memberships; a supplied `X-Runloom-Team` header never grants analytics access.

| Query | Values / default |
| --- | --- |
| `period` | `1d`, `1w`, `1m` (default), `mtd`, `ytd`, `6m`, `1y` |
| `team` | Optional authorized team ID; omitted means all accessible teams in this organization |
| `mode` | `all` (default), `test`, `production` (not marked as tests) |
| `group` | `runs` (default), `teams`, `models` |
| `sort` | `cost` (default), `name`, `requests`, `tokens`, `runs`, `latest` |
| `direction` | `desc` (default), `asc` |
| `page` | Zero-based page, default 0; page size fixed at 50 |
| `as_of` | Optional returned range end as RFC3339; must not be in the future or more than 24 hours old |

Response fields are `window`, `as_of`, `currency: "USD"`, `timezone: "UTC"`, `summary`, `previous`, `trend`, `rows`, `total_rows`, `page`, `page_size`, `group`, `sort`, `direction`, `allowed_teams`, `scope: "authorized_teams_direct_requests"`, and `recorded_requests_only: true`. Monetary values are decimal strings; an unavailable average is null. Responses contain usage metadata, not prompts, outputs, credentials, or provider response bodies. Studio errors use `{"detail":"..."}`: 400 for invalid filters, 401 for invalid/non-Studio credentials, 403 for denied organization/team access, 422 for report limits, and 503 for unavailable accounting. Retrying this read is safe; refresh the range if `as_of` expires.

Example request: `GET /api/v1/studio/orgs/<organization-id>/llm-analytics?period=6m&group=teams&sort=cost&direction=desc`. Replace the organization ID with an authorized organization; use an interactive Studio token, not a service-account client secret.

Deploy the updated API before Studio. Existing UTC usage records are used without backfilling prices. Store initialization adds timestamp indexes for PostgreSQL and MongoDB; allow for index creation time on large collections. Filesystem storage uses a bounded, deadline-controlled scan for local development. The worker accounting contract is unchanged; already-instrumented workers do not need a rollout for this dashboard. Reverting Studio/API leaves existing usage and additive indexes intact.

## Accounting contract

1. The Go worker meters both AI / LLM nodes and Action nodes invoking `runloom.llm`. Generic External API nodes and custom activities are not automatically metered. The adapter currently understands OpenAI-compatible chat-completions usage, not every provider's native response format.
2. Each provider request starts with a durable intent. Its ID identifies the Temporal namespace, run, activity and attempt. A retried activity is a separate potentially billable request; repeating a write for the same attempt cannot count it twice. An existing attempt intent prevents that same attempt from calling the provider again.
3. Usage is recorded before checking the response's message content, so a charged response with invalid content can still be priced. Failures and cancelled requests may incur charges. If the provider does not report usable usage, the amount is **unknown**, never assumed free.
4. An unavailable database before the request prevents the provider call. A completion-write failure does not cause a successful provider call to be retried: its pending intent remains, and the successful node output includes `usage_recorded: false`. Pending entries are displayed as unknown once their run ends or they are more than one hour old. A crashed worker or an uncertain network outcome still needs operator/provider reconciliation; this is not exactly-once billing.
5. Parent totals include descendants using ancestry obtained from Temporal, not workflow-supplied metadata. A child's own view shows that child and its descendants. Use the direct subtotal when combining parent and child reports yourself, otherwise you will double-count.
6. Prices and arithmetic use decimal USD strings, with twelve decimal places in the stored estimate. Cached input is a subset of input; reasoning tokens are a subset of output and are not charged twice. Unsupported modalities or missing cached-input pricing stay unpriced. No provider invoice, tax, discount, currency conversion or tool-call fees are inferred.
7. The metadata-only ledger contains run/version/node IDs, provider/model, timestamps, status, token counts, request ID and the applied price snapshot. It does not copy prompts, response content or credentials. Normal workflow input/output history is unchanged and may contain sensitive content; existing team access and redaction rules still apply.

## Soft and Hard Limit Policies

AI Decision nodes use the same metered LLM adapter for every decision round. Each provider attempt is separately accounted for, including charged responses rejected by structured-output validation. Valid selections also emit `llm.decision_selected` audit events with the usage identity, selected action IDs, outcome and brief model reason. Unlike the metadata-only cost ledger, this audit reason is model content and may be sensitive. Audit-write failures produce an operational warning without retrying a paid call; the recorded Temporal result remains the execution source of truth. Hard-limit pauses retain the paid choice and block selected actions until a successful budget recheck. Replaying history does not call the model or count its cost again.

Open **LLM budgets** in Studio. Policies belong to a team and can be reused across its projects. Each project has its own allowance; sharing a policy never pools project budgets. All limits use recorded estimated USD spend in the current calendar month, from 00:00 UTC on the first day to the next month's first day. There are no reservations or automatic currency conversions.

Enforcement covers built-in LLM nodes and action nodes invoking `runloom.llm`, not arbitrary External API calls to model providers. Requests count toward the UTC month in which they started, even if their responses arrive in the next month.

1. An enabled policy applies to all current and future projects by default. Select specific projects to narrow it. All workflows in covered projects inherit the policy unless explicitly excluded. Multiple policies apply together; any applicable hard limit can block execution.
2. Set a soft limit, hard limit, or both. Blank means unset; zero is an immediate threshold. A configured soft limit must not exceed the hard limit. At or above a soft limit, the worker writes `llm_budget.soft_limit_reached` events and continues. Warnings can repeat across attempts; they are not a once-per-month notification.
3. Before every covered LLM activity attempt, the worker checks current policy and recorded project spend. A reached hard limit prevents the provider call and leaves the workflow in a durable `paused` state. After a successful call is accounted for, a reached hard limit also pauses the workflow, retaining the response before advancing—even if this was its last node. The budget pause itself never completes, fails, or cancels the workflow.
4. Studio shows **LLM budget pause**, the affected node, policy, project, month, spend, limit and reason. **Recheck budget & resume** checks current settings and spend again. An exhausted allowance remains paused. Raise/disable the relevant policy, authorize an exclusion, or wait for the monthly reset, then explicitly resume. Resetting the allowance or editing a policy does not automatically resume runs. A completed provider response is reused, not billed again.
5. Team admins, including organization administrators with inherited team-admin access, can create/change policies and exclusions. Members with viewer/editor access can read budgets but cannot change policies. Existing editor/admin run permissions control resume; resume never overrides the policy. Consumer tokens cannot access policy management. Consumer run-status reads expose only `paused` with `pause_reason: llm_budget_limit` or `llm_budget_accounting_unavailable`, not project totals or policy details. Consumer list entries retain their execution-ledger status and can still say `running`; retrieve a run's status for its budget pause.
6. Every policy save requires a reason and the last-read revision. Concurrent stale saves return 409. The saved rule, revision, actor, exclusions and reason are recorded atomically in the team project catalog. Disable a policy to retire it without deleting this history. Studio shows the latest 100 policy changes and budget events found in the latest 100 team audit records; individual run audit history remains available separately. Resume signal audits are named `workflow.resume_requested`: accepting a request is not proof that the run resumed.
7. Exclusions bypass enforcement, not cost accounting. Excluded workflow requests still contribute to their project's overall spend. Tests, retries, failed priced attempts, and child requests count. Children use their own workflow's project and exclusions; excluding a parent does not exempt its children. A paused child remains a child execution and its parent waits for it; open the child run to inspect or resume its budget checkpoint.

Hard limits are **recorded-spend thresholds, not guaranteed provider spending caps**. Already-running and concurrent requests may finish and exceed a limit. No charged result is discarded or refunded. Within a workflow, a budget pause blocks new sibling-branch steps, but activities/children already dispatched can continue. Existing explicit cancellation and execution/child timeouts still apply; budget pauses do not extend Temporal execution timeouts or suspend external systems.

Missing pricing, unknown outcomes and pending records are shown as incomplete accounting and are not assumed free. Limits compare the known estimated subtotal only, so configure the verified pricing catalog below before relying on monetary thresholds. Provider invoices can differ. A budget-check storage failure or scan overflow pauses at the LLM checkpoint with an accounting-unavailable reason, rather than allowing a request based on a partial total. A post-provider accounting failure never causes that successful paid call to be repeated.

New instrumented requests store their project ID at request time. Moving a workflow does not move those historical charges; future requests use its new project. Unattributed records use the workflow's current project mapping; this is not historical project reconstruction. PostgreSQL and MongoDB now aggregate monthly checks inside the database with decimal costs and incomplete-accounting flags. The former 50,000-request budget ceiling is removed; the eight-second read deadline remains. Analytics reports retain their independent limits. Very large ledgers may still require materialized totals. Policy definitions are limited to 100 per team, 100 explicitly selected projects per policy and 500 excluded workflows per policy.

### Budget policy API

`GET /api/v1/studio/orgs/{org}/teams/{team}/llm-budget-policies` returns `items`, `projects`, `workflows`, `spend`, `alerts`, recent `events`, recent policy `changes`, `month`, `resets_at`, `currency: "USD"`, and `timezone: "UTC"`. It requires a human Studio token with access to that team. Spend rows have `project_id`, `project_name`, decimal `spend_usd`, `requests`, and `incomplete`. No prompts, provider credentials or outputs are included.

`PUT /api/v1/studio/orgs/{org}/teams/{team}/llm-budget-policies/{policy-uuid}` requires team-admin access. Use a new UUID and revision 0 to create; send the returned revision to update. Supply `name`, `enabled`, decimal-string `soft_usd`/`hard_usd`, `projects` (empty array means all), `excluded_workflows` (empty array means none), and a nonempty `reason`. IDs are validated against the owning team's projects/workflows. The period is fixed to monthly UTC. Errors use the Studio `detail` envelope: 400 invalid input, 401 non-Studio authentication, 403 access denied, 409 stale revision, and 503 unavailable storage/accounting. Refresh after an uncertain save response before retrying.

Deploy the new worker, API and Studio together. The worker registers `runloom.internal.check_llm_budget`, which only checks accounting and never calls a provider. The `llm-budget-pause-v1` Temporal version marker preserves old activity-command replay; executions already committed to the legacy branch are not retroactively guaranteed enforcement. Upgrade every worker polling the queue before relying on policies. Do not roll back a worker to a build without the new marker/check activity while budget-aware executions remain open. Budget settings are project metadata, not executable/versioned workflow JSON; no workflow republishing is required.

Run `make test-llm-costs` for policy/store/API/runtime checks, `npm run test:budgets` in `studio/` for browser checks, and `make test-llm-costs-live` against local/disposable Temporal for the fake-provider pause/restart/resume/replay test. PostgreSQL and MongoDB policy CAS, audit persistence, month-boundary and attribution tests use the existing opt-in database test URLs. Live tests leave isolated team data/history for inspection and do not change application policies or call paid providers.

## Configure versioned pricing

Without a catalogue, usable token counts are recorded but displayed as **unpriced**. There are no built-in guessed prices. Copy the structure in [the synthetic pricing example](../deploy/llm-pricing.example.json), replacing its test provider, exact returned model ID and rates with your verified provider contract. Those example rates are not real provider prices.

Set `RUNLOOM_LLM_PROVIDER` to the catalogue's provider identifier; its default is `openai-compatible`. Set `RUNLOOM_LLM_PRICING_FILE` on the worker to a readable JSON file. Each rate requires a version, RFC3339 effective date, USD currency, exact model ID and decimal input/output prices per million tokens. Cached-input pricing is required when cached tokens are reported. The most recent effective rate at request start is selected. Returned model IDs, including dated versions, must be listed explicitly; aliases are not guessed.

The catalogue is validated and loaded at worker startup. Add new dated entries rather than altering historical rates, then restart/roll out the worker. Recorded entries retain their price snapshot. Updating the file does not reprice older requests or hot-reload a running worker. Unknown historical usage cannot be backfilled from workflow success alone.

For Compose, pass an absolute path and add the pricing overlay to your usual files:

```sh
RUNLOOM_LLM_PRICING_PATH=/absolute/path/pricing.json \
  docker compose --project-directory . --env-file deploy/local.env -p runloom-local \
  -f compose/compose.yaml -f compose/compose.pricing.yaml up -d worker
```

Preserve any other overlays used by your installation. The catalogue is mounted read-only into the worker. LLM credentials remain separate in `RUNLOOM_LLM_API_KEY_FILE`; do not put credentials in the catalogue.

For Helm, create a ConfigMap containing the catalogue and set `llm.pricingConfigMap`, `llm.pricingKey` (default `pricing.json`) and `llm.provider`. Only the worker receives the mount. Roll out the worker after changing external ConfigMap contents; the chart cannot hash a ConfigMap it does not own.

## Studio API

`GET /api/v1/studio/runs/{workflow_id}/llm-usage` requires a human Studio bearer token and `X-Runloom-Team`, with the same team/run visibility as run history. Consumer service-account tokens are explicitly rejected on this endpoint; no management data is exposed through the consumer API.

Query parameters are `limit` (1–100, default 50), `after` (the returned `next_cursor` UUID), `model` (exact returned model ID) and `step_id` (exact node ID). The response contains `summary`, `entries`, `next_cursor`, `currency: "USD"` and `scope: "run_and_descendants"`. Filters affect request rows, not the whole-run summary. Rows paginate in stable ledger-ID order, not chronological order. While a run is active, new random-ID entries can sort before a previously returned cursor; refresh from page one for a current view. Studio refreshes active runs every five seconds.

`GET /api/v1/studio/runs?include_usage=true` enriches up to the first 20 returned runs with `llm_cost`. Enrichment has a shared two-second deadline; a missing summary means unavailable/not recorded, not zero. Individual run summaries are bounded to 10,000 requests and five seconds. A larger run is clearly marked `truncated` and its total is a lower bound; request pagination remains available. Use the separately bounded analytics endpoint above for cross-run and cross-team reporting. Neither endpoint is an invoice export API.

The ledger supports MongoDB, PostgreSQL and development filesystem storage. Database structures are created by the existing store initialization path. No legacy costs are invented or backfilled. Existing general Studio manual runs are not retroactively classified as tests; the test filter uses the run's recorded test marker.

Upgrade all workers polling the workflow task queue to enable complete coverage; old workers do not write usage records. During a mixed-version rollout, recorded totals can omit requests handled by an old worker. The ledger cannot detect an uninstrumented request that never created an intent. Treat that transition period as incomplete even when every recorded row is priced.

## Verification and next steps

Run `make test-llm-costs` and `make test-ui-costs-email` (the latter needs the Angular development server and Playwright). Opt in to database integration tests with `RUNLOOM_TEST_POSTGRES_URL` and `RUNLOOM_TEST_MONGODB_URL` pointing to disposable/local databases. Test data uses isolated schemas/collection prefixes.

Analytics checks are included in `make test-llm-costs`; run `npm run test:analytics` in `studio/` against the development server for the dashboard's KPIs, seven date presets, sorting/paging, empty/error/incomplete states, stale response protection and mobile layout. `make test-ui-browser` includes that browser suite. Ledger date-boundary integration cases cover fractional-second timestamps in filesystem, PostgreSQL and MongoDB stores when the opt-in database URLs are provided.

Future extensions are provider-specific adapters, invoice reconciliation, exports, materialized aggregates for larger reporting volumes and retention policies. A budget check cannot guarantee an exact spending ceiling during concurrent or already-in-flight requests. For larger multi-team deployments, consider Temporal Task Queue Fairness alongside explicit per-team provider/concurrency quotas; budget enforcement does not provide either.

For real Temporal execution with a loopback-only fake LLM, set `RUNLOOM_TEST_TEMPORAL_ADDRESS` and run `make test-llm-costs-live`. Optional `RUNLOOM_TEST_DATABASE_DRIVER=mongodb` (or `postgres`) and `RUNLOOM_TEST_DATABASE_URL` exercise a live database. This creates an isolated team/task queue, parent and child runs, a billable synthetic retry, and replays history without another provider request. It never calls a paid provider or changes the running application's LLM credentials. Live integration fixtures remain in the test database/Temporal history for inspection; use local/disposable infrastructure.
