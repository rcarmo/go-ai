import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
import { classify } from "../src/api/pi-decision.ts";
import type { ClassifierModel } from "../src/types.ts";

const model: ClassifierModel<"pi-decision"> = {
  type: "classifier", id: "jev-latest", name: "Jev Latest", api: "pi-decision", provider: "radius",
  baseUrl: "https://radius.example/v1", input: ["text"], contextWindow: 262144,
  cost: { input: 0.5, output: 1.5, cacheRead: 0, cacheWrite: 0, imageOutput: 10 },
};
const context = { state: {}, questions: { approved: { type: "bool", instructions: "Approval?", criteria: { true: "yes", false: "no" } } } } as const;
const fixtures: {name: string, usage: unknown, want: unknown}[] = JSON.parse(readFileSync(new URL("./pi-usage.fixture.json", import.meta.url), "utf8"));

it.each(fixtures)("uses shared Pi usage: $name", async ({usage, want}) => {
  let requests = 0;
  const result = await classify(model, context, { apiKey: "test", maxRetries: 0, fetch: async () => {
    requests++;
    return new Response(JSON.stringify({ usage, responseId: "gen-usage", answers: { approved: {type: "bool", probability: 0.96} } }));
  } });
  expect(requests).toBe(1);
  expect(result.stopReason).toBe("stop");
  expect(result.responseId).toBe("gen-usage");
  expect(result.answers).toStrictEqual({approved: {type: "bool", probability: 0.96}});
  if (want === null) expect(result).not.toHaveProperty("usage");
  else expect(result.usage).toStrictEqual(want);
});
