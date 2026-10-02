import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { runInNewContext } from "node:vm";

const source = readFileSync(
  new URL(
    "../../../backend/internal/identity/web/auth-link.js",
    import.meta.url,
  ),
  "utf8",
);
const token = "A".repeat(43); // Synthetic token, never used with a real account.

type PageElement = {
  hidden: boolean;
  disabled: boolean;
  value: string;
  textContent: string;
  href: string;
  handlers: Record<
    string,
    (event: { preventDefault(): void }) => void | Promise<void>
  >;
  addEventListener(
    event: string,
    handler: PageElement["handlers"][string],
  ): void;
  focus(): void;
  select(): void;
};

function page(
  action = "verify",
  fragment = "token=" + token,
  respond: () => Promise<{
    ok: boolean;
    status: number;
    json?(): Promise<unknown>;
  }> = async () => ({ ok: true, status: 200 }),
  device: {
    userAgent?: string;
    maxTouchPoints?: number;
    userAgentData?: { mobile: boolean };
    blockApp?: boolean;
  } = {},
) {
  const elements = new Map<string, PageElement>();
  for (const id of [
    "title",
    "description",
    "controls",
    "manual",
    "manual-help",
    "token",
    "open-app",
    "app-help",
    "status",
    "copy",
    "verify",
    "reset-form",
    "password",
    "confirm-password",
    "reset",
    "continue-browser",
    "return-web",
  ]) {
    elements.set(id, {
      hidden: [
        "controls",
        "manual",
        "verify",
        "reset-form",
        "continue-browser",
        "return-web",
      ].includes(id),
      disabled: false,
      value: "",
      textContent: "",
      href: "",
      handlers: {},
      addEventListener(event, handler) {
        this.handlers[event] = handler;
      },
      focus() {},
      select() {},
    });
  }
  const calls: { url: string; options: RequestInit }[] = [];
  const history: string[] = [];
  const navigation = {
    handlers: {} as Record<string, () => void>,
    reloads: 0,
    appLinks: [] as string[],
  };
  runInNewContext(source, {
    document: { getElementById: (id: string) => elements.get(id), title: "" },
    window: {
      addEventListener: (event: string, handler: () => void) => {
        navigation.handlers[event] = handler;
      },
    },
    location: {
      pathname: "/auth/" + action,
      hash: "#" + fragment,
      reload: () => {
        navigation.reloads++;
      },
      assign: (url: string) => {
        navigation.appLinks.push(url);
        if (device.blockApp) throw new Error("App opening blocked");
      },
    },
    history: {
      replaceState: (_state: unknown, _title: string, url: string) =>
        history.push(url),
    },
    URLSearchParams,
    AbortController,
    TextEncoder,
    setTimeout,
    clearTimeout,
    navigator: {
      userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
      maxTouchPoints: 0,
      ...device,
      clipboard: {
        writeText: async () => {
          throw new Error("Clipboard blocked");
        },
      },
    },
    fetch: async (url: string, options: RequestInit) => {
      calls.push({ url, options });
      return respond();
    },
  });
  const get = (id: string) => {
    const element = elements.get(id);
    assert.ok(element, `Missing test element: ${id}`);
    return element;
  };
  const click = (id: string) => {
    const handler = get(id).handlers.click;
    assert.ok(handler, `Missing click handler: ${id}`);
    return handler({ preventDefault() {} });
  };
  const submit = () => {
    const handler = get("reset-form").handlers.submit;
    assert.ok(handler, "Missing reset form handler");
    return handler({ preventDefault() {} });
  };
  return { get, click, submit, calls, history, navigation };
}

test("opening another email in the same tab reloads the passive page instead of retaining the previous token", () => {
  const p = page();
  const handler = p.navigation.handlers.hashchange;
  assert.ok(handler);
  handler();
  assert.equal(p.navigation.reloads, 1);
  assert.equal(p.calls.length, 0);
});

test("email landing keeps previews passive and hands each action to the existing app scheme", () => {
  for (const action of ["verify", "reset-password", "accept-invitation"]) {
    const p = page(action);
    assert.equal(p.calls.length, 0);
    assert.deepEqual(p.history, ["/auth/" + action]);
    assert.equal(
      p.get("open-app").href,
      `holyhymns://auth/${action}?token=${token}`,
    );
    assert.equal(p.get("controls").hidden, action === "reset-password");
    assert.equal(p.get("verify").hidden, action !== "verify");
  }
});

test("missing, malformed and ambiguous link tokens cannot be submitted or launched", () => {
  for (const fragment of [
    "",
    "token=bad",
    "token=" + token + "&token=" + token,
    "token=%3Cscript%3E",
  ]) {
    const p = page("verify", fragment);
    assert.equal(p.get("controls").hidden, true);
    assert.equal(p.calls.length, 0);
    assert.match(p.get("status").textContent, /No account changes/);
  }
  assert.equal(page("unknown").get("controls").hidden, true);
});

test("browser verification posts only on a click, then discards the consumed token", async () => {
  const p = page();
  await p.click("verify");
  assert.equal(p.calls.length, 1);
  const call = p.calls[0];
  assert.ok(call);
  assert.equal(call.url, "../v1/auth/verify");
  assert.equal(call.options.method, "POST");
  assert.equal(call.options.credentials, "omit");
  assert.equal(call.options.redirect, "error");
  assert.deepEqual(JSON.parse(call.options.body as string), { token });
  assert.equal(p.get("title").textContent, "Email verified");
  assert.equal(p.get("manual").hidden, true);
  assert.equal(p.get("verify").hidden, true);
  assert.equal(p.get("token").value, "");
  assert.equal(p.get("open-app").href, "holyhymns://");
  await p.click("open-app");
  assert.match(p.get("status").textContent, /sign in from the Account screen/);
});

test("expired links, rate limits and temporary failures have actionable states", async () => {
  for (const [status, message] of [
    [400, /expired/],
    [429, /Too many attempts/],
    [503, /unavailable/],
  ] as const) {
    const p = page("verify", "token=" + token, async () => ({
      ok: false,
      status,
    }));
    await p.click("verify");
    assert.match(p.get("status").textContent, message);
    assert.equal(p.get("verify").disabled, false);
    assert.equal(p.get("verify").hidden, status === 400);
  }
});

test("network errors allow retry without claiming verification succeeded", async () => {
  const p = page("verify", "token=" + token, async () => {
    throw new Error("offline");
  });
  await p.click("verify");
  assert.match(p.get("status").textContent, /Could not confirm/);
  assert.equal(p.get("verify").disabled, false);
  assert.equal(p.get("token").value, token);
});

test("clipboard denial falls back to selecting the manual code", async () => {
  const p = page();
  await p.click("copy");
  assert.match(p.get("status").textContent, /Select and copy/);
});

test("desktop reset links show a browser form without attempting an app launch", () => {
  for (const device of [
    {},
    {
      userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
      maxTouchPoints: 10,
    },
    { userAgent: "Mozilla/5.0 (X11; Linux x86_64)" },
  ]) {
    const p = page("reset-password", undefined, undefined, device);
    assert.equal(p.get("reset-form").hidden, false);
    assert.equal(p.get("controls").hidden, true);
    assert.deepEqual(p.navigation.appLinks, []);
    assert.equal(p.calls.length, 0);
  }
});

test("mobile reset links attempt app handoff and retain an explicit browser fallback", async () => {
  for (const device of [
    { userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)" },
    { userAgent: "Mozilla/5.0 (Linux; Android 15; Pixel 9)" },
    { userAgent: "Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X)" },
    {
      userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
      maxTouchPoints: 5,
    },
    { userAgent: "", userAgentData: { mobile: true } },
    { userAgent: "Android", blockApp: true },
  ]) {
    const p = page("reset-password", undefined, undefined, device);
    assert.deepEqual(p.navigation.appLinks, [
      `holyhymns://auth/reset-password?token=${token}`,
    ]);
    assert.equal(p.calls.length, 0);
    assert.equal(p.get("controls").hidden, false);
    assert.equal(p.get("reset-form").hidden, true);
    assert.equal(p.get("continue-browser").hidden, false);
    await p.click("continue-browser");
    assert.equal(p.get("reset-form").hidden, false);
    p.get("password").value = p.get("confirm-password").value =
      "new valid password";
    await p.submit();
    assert.equal(p.get("title").textContent, "Password reset");
  }
});

test("invalid reset links cannot display a form or launch the app", () => {
  for (const fragment of ["", "token=bad", `token=${token}&token=${token}`]) {
    const p = page("reset-password", fragment, undefined, {
      userAgent: "iPhone",
    });
    assert.equal(p.get("reset-form").hidden, true);
    assert.equal(p.get("controls").hidden, true);
    assert.deepEqual(p.navigation.appLinks, []);
    assert.equal(p.calls.length, 0);
  }
});

test("browser reset requires a valid matching password before posting", async () => {
  const p = page("reset-password");
  for (const password of [
    "",
    "short",
    "😀".repeat(6),
    "a".repeat(1025),
    "അ".repeat(342),
  ]) {
    p.get("password").value = p.get("confirm-password").value = password;
    await p.submit();
    assert.equal(p.calls.length, 0);
    assert.match(p.get("status").textContent, /12 characters.*1024 bytes/);
  }
  p.get("password").value = "new valid password";
  p.get("confirm-password").value = "different valid password";
  await p.submit();
  assert.equal(p.calls.length, 0);
  assert.match(p.get("status").textContent, /do not match/);
});

test("browser reset posts once on submit and clears credentials after success", async () => {
  let resolve!: (response: { ok: boolean; status: number }) => void;
  const p = page(
    "reset-password",
    undefined,
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const password = "  a new valid password  ";
  p.get("password").value = p.get("confirm-password").value = password;
  const pending = p.submit();
  assert.equal(p.get("reset").disabled, true);
  await p.submit();
  assert.equal(p.calls.length, 1);
  const call = p.calls[0]!;
  assert.equal(call.url, "../v1/auth/reset-password");
  assert.equal(call.options.method, "POST");
  assert.equal(call.options.credentials, "omit");
  assert.equal(call.options.redirect, "error");
  assert.equal(call.options.cache, "no-store");
  assert.equal(call.options.referrerPolicy, "no-referrer");
  assert.deepEqual(JSON.parse(call.options.body as string), {
    token,
    password,
  });
  resolve({ ok: true, status: 200 });
  await pending;
  assert.equal(p.get("title").textContent, "Password reset");
  assert.equal(p.get("reset-form").hidden, true);
  assert.equal(p.get("manual").hidden, true);
  assert.equal(p.get("return-web").hidden, false);
  for (const id of ["token", "password", "confirm-password"])
    assert.equal(p.get(id).value, "");
  assert.equal(p.get("open-app").href, "holyhymns://");
  await p.submit();
  assert.equal(p.calls.length, 1);
});

test("reset API failures distinguish expired tokens from retryable errors", async () => {
  for (const [status, code, message] of [
    [400, "invalid_token", /expired/],
    [400, "invalid_password", /12 characters/],
    [429, "rate_limited", /Too many attempts/],
    [503, "internal_error", /unavailable/],
  ] as const) {
    const p = page("reset-password", undefined, async () => ({
      ok: false,
      status,
      json: async () => ({ code }),
    }));
    p.get("password").value = p.get("confirm-password").value =
      "new valid password";
    await p.submit();
    assert.match(p.get("status").textContent, message);
    assert.equal(p.get("reset").disabled, false);
    assert.equal(p.get("reset-form").hidden, code === "invalid_token");
    assert.equal(p.get("return-web").hidden, code !== "invalid_token");
    assert.equal(p.get("token").value, code === "invalid_token" ? "" : token);
  }
});

test("reset network failures retain a retry path without claiming success", async () => {
  const p = page("reset-password", undefined, async () => {
    throw new Error("offline");
  });
  p.get("password").value = p.get("confirm-password").value =
    "new valid password";
  await p.submit();
  assert.match(p.get("status").textContent, /Could not confirm/);
  assert.equal(p.get("reset").disabled, false);
  assert.equal(p.get("reset-form").hidden, false);
  assert.equal(p.get("token").value, token);
});
