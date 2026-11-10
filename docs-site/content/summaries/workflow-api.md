# Run lifecycle

A run is one execution of a workflow snapshot. Later draft edits do not change it.

## Operate a run

1. Start a published main workflow, or use **Test workflow** for a draft test.
2. Inspect its status, inputs and permitted output in run history.
3. Respond only to the event or gate the run is waiting for.
4. Cancel only when you want to end the execution permanently.

## Choose the right action

| Waiting for | What continues execution |
| --- | --- |
| Timer | Elapsed time |
| Callback or signal | The matching event |
| Approval | An authorized decision |
| Operator pause | Authorized resume, or its configured timeout rule |

Cancellation is not pause. Consumer APIs support cancellation but not pause/resume. Studio permissions still apply to run operations.

Before retrying a start, check whether a run already exists. Testing can send email, write data or incur model charges. See [Studio API](/api/studio).
