import assert from "node:assert/strict";
import test from "node:test";
import type { ErrorEvent } from "@sentry/react-native";
import { scrubEvent, scrubText } from "./telemetry-privacy";

test("error reports exclude auth links, credentials, request bodies and user data", () => {
  const event: ErrorEvent = {
    type: undefined,
    message: "Failed holyhymns://auth/reset-password?token=private-link",
    request: {
      url: "https://api.example/auth?token=private-query",
      data: "private-body",
    },
    user: { email: "private@example.com" },
    extra: { lyrics: "private-lyrics" },
    breadcrumbs: [{ message: "private-console" }],
    exception: {
      values: [
        {
          type: "Error",
          value:
            "Bearer private-bearer password=private-password private@example.com",
          stacktrace: {
            frames: [
              {
                function: "saveSong",
                lineno: 42,
                vars: { token: "private-var" },
              },
            ],
          },
        },
      ],
    },
    tags: { request_id: "ABCDEFG", status: "500" },
  };
  const result = scrubEvent(event);
  assert.doesNotMatch(JSON.stringify(result), /private/);
  assert.equal(
    result.exception?.values?.[0]?.stacktrace?.frames?.[0]?.lineno,
    42,
  );
  assert.equal(result.tags?.request_id, "ABCDEFG");
  assert.equal(
    scrubText("Cannot read property length of undefined"),
    "Cannot read property length of undefined",
  );
});
