import { api, ApiError } from "./api";

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) {
    throw new Error(message);
  }
}

async function testSwitchModelRequestPayload() {
  let capturedUrl = "";
  let capturedInit: RequestInit | undefined;

  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    capturedUrl = String(input);
    capturedInit = init;
    return new Response(JSON.stringify({ status: "ok", model: "gpt-5" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;

  const result = await api.switchConversationModel("conv-123", "gpt-5");

  assert(capturedUrl === "/api/conversation/conv-123/switch-model", "wrong switch-model URL");
  assert(capturedInit?.method === "POST", "expected POST method");
  assert(capturedInit?.headers && (capturedInit.headers as Record<string, string>)["Content-Type"] === "application/json", "expected JSON content-type");
  assert(capturedInit?.body === JSON.stringify({ model: "gpt-5", cancel_current_turn: false }), "wrong request payload");
  assert(result.status === "ok" && result.model === "gpt-5", "wrong response payload");
}

async function testSwitchModelConflictErrorHasStatus() {
  globalThis.fetch = (async () => {
    return new Response("cannot switch", { status: 409, statusText: "Conflict" });
  }) as typeof fetch;

  let threw = false;
  try {
    await api.switchConversationModel("conv-123", "gpt-5", false);
  } catch (err) {
    threw = true;
    assert(err instanceof ApiError, "expected ApiError");
    assert(err.status === 409, "expected 409 status on ApiError");
    assert(err.message.includes("cannot switch"), "expected response body in error message");
  }

  assert(threw, "expected switchConversationModel to throw on 409");
}

async function run() {
  await testSwitchModelRequestPayload();
  await testSwitchModelConflictErrorHasStatus();
  console.log("api.switchConversationModel tests: PASS");
}

run().catch((err) => {
  console.error("api.switchConversationModel tests: FAIL");
  console.error(err);
  process.exit(1);
});
