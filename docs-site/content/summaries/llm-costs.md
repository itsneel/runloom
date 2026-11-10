# LLM costs & budgets

Track estimated model spend by run, team and model. These estimates are not provider invoices.

## Review spending

1. Open **Cost analytics** in Management.
2. Choose a period: day, week, month, MTD, YTD, six months or year.
3. Filter accessible teams and test/production runs.
4. Sort the breakdown and open a run for request-level details.

## Set budget policies

Reusable policies give each project its own monthly UTC allowance. They apply by default unless a workflow is excluded.

A soft limit logs a warning and continues. A reached hard limit blocks the next model call, or pauses immediately after a completed call crosses it. The workflow pauses with a reason—not cancellation or completion. In-flight calls can exceed the budget.

After adjusting the allowance or waiting for the monthly reset, explicitly recheck and resume. Runs do not resume automatically.

Unknown or unpriced usage is not zero. Retries may incur extra charges, and parent totals include child costs. Do not add parent and child totals together.
