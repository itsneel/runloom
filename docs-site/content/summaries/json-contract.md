# Node reference

Each node performs a task or controls what runs next. Choose the node, configure its inputs, then check the workflow.

## Choose a node

| Goal | Node or capability |
| --- | --- |
| Call another system | External API, Database, Email |
| Transform data | Custom Code (JavaScript) |
| Generate or choose an action | AI / LLM, AI Decision |
| Split or route work | Condition, Parallel |
| Wait for time or an event | Wait, Callback, Signal |
| Ask a person or operator | Approval, Pause |
| Reuse or finish work | Subworkflow, End workflow |

## Pass data between steps

Use expressions such as `${input.customer}` and `${steps.previous.output.field}` in supported input fields. Keep credentials in saved secrets or connections, not JSON.

LLM responses follow a defined output format and include `hitl_required` and `hitl_reason`. Test with representative input; integration tests can make real changes or incur charges.

Expand the technical reference for every node's fields, examples, validation rules and limits.
