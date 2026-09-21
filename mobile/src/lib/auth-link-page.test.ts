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
  handlers: Record<string, () => void | Promise<void>>;
  addEventListener(event: string, handler: () => void | Promise<void>): void;
  focus(): void;
  select(): void;
};

function page(
  action = "verify",
  fragment = "token=" + token,
  respond = async () => ({ ok: true, status: 200 }),
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
  ]) {
    elements.set(id, {
      hidden: ["controls", "manual", "verify"].includes(id),
      disabled: false,
      value: "",
      textContent: "",
      href: "",
      handlers: {},
      addEventListener(event: string, handler: () => void | Promise<void>) {
        this.handlers[event] = handler;
      },
      focus() {},
      select() {},
    });
  }
  const calls: { url: string; options: RequestInit }[] = [];
  const history: string[] = [];
  const navigation = { handlers: {} as Record<string, () => void>, reloads: 0 };
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
    },
    history: {
      replaceState: (_state: unknown, _title: string, url: string) =>
        history.push(url),
    },
    URLSearchParams,
    AbortController,
    setTimeout,
    clearTimeout,
    navigator: {
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
    return handler();
  };
  return { get, click, calls, history, navigation };
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
    assert.equal(p.get("controls").hidden, false);
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
