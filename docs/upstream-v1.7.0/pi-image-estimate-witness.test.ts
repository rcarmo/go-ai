import { expect, it } from "vitest";
import { generateImages } from "../src/api/pi-image.ts";
import type { ImageModel } from "../src/types.ts";

it("marks a Pi Image catalog estimate as not provider-reported", async () => {
  const model: ImageModel<"pi-image"> = {
    type: "image", id: "nano-banana", name: "Nano Banana", api: "pi-image", provider: "radius",
    baseUrl: "https://radius.example/v1", input: ["text", "image"], output: ["image", "text"],
    cost: { input: 0.3, output: 2.5, cacheRead: 0.03, cacheWrite: 0.083333 },
  };
  const result = await generateImages(model, {input: [{type: "text", text: "A red fox in the snow"}]}, {
    apiKey: "test", maxRetries: 0,
    fetch: async () => new Response(JSON.stringify({
      output: [{type: "image", data: "AAAA", mimeType: "image/png"}],
      usage: {input: 1000, output: 2000, cacheRead: 0, cacheWrite: 0, totalTokens: 3000},
    })),
  });
  expect(result.stopReason).toBe("stop");
  expect(result.usage?.cost.total).toBeCloseTo(0.0053, 12);
  expect(result.usage?.cost.totalFromProvider).toBe(false);
});
