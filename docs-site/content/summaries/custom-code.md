# JavaScript

Use **Custom Code** to transform JSON when ordinary field mapping is not enough.

## Add a transformation

1. Add a Custom Code node.
2. Map the input data it needs.
3. Write JavaScript that returns the expected JSON result.
4. Test representative inputs, including missing or invalid values.

## What it can do

The node runs in a separate, bounded sandbox. It supports JavaScript only—not TypeScript, Node.js packages, arbitrary processes or direct network/database access.

Use dedicated External API and Database nodes for integrations. Never include credentials in code. The reference contains the exact code contract, examples, timeouts and memory limits.
