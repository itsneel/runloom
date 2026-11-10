import test from "node:test";
import assert from "node:assert/strict";
import {
  definitionFor,
  scenarios,
  siteConfig,
  faqs,
} from "../src/app/content.ts";

test("every demo has a complete, independent JSON draft", () => {
  assert.equal(new Set(scenarios.map((s) => s.id)).size, scenarios.length);
  for (const scenario of scenarios) {
    const definition = definitionFor(scenario);
    assert.equal(definition.schema_version, "1.0");
    assert.equal(definition.version, 0);
    assert.equal(definition.trigger.type, "manual");
    assert.equal(scenario.nodes.length, 5);
    assert.equal(definition.steps.length, 3);
    assert.equal(new Set(definition.steps.map((s) => s.id)).size, 3);
    assert(["approval", "callback"].includes(definition.steps[1].type));
    assert.deepEqual(JSON.parse(JSON.stringify(definition)), definition);
  }
});
test("examples do not share mutable generated definition wrappers", () => {
  const first = definitionFor(scenarios[0]);
  first.version = 5;
  assert.equal(definitionFor(scenarios[0]).version, 0);
});
test("external destinations are optional, valid HTTP(S) URLs", () => {
  for (const url of Object.values(siteConfig)) {
    if (url) assert(["http:", "https:"].includes(new URL(url).protocol));
  }
});
test("product limitations and browser-only demo are disclosed", () => {
  assert(faqs.some((faq) => faq.answer.includes("browser-only")));
  assert(faqs.some((faq) => faq.answer.includes("OAuth configuration")));
  assert(faqs.some((faq) => faq.answer.includes("operator responsibilities")));
});
