(() => {
  "use strict";
  // A second email opened in this tab can be a fragment-only navigation.
  // Reload the read-only page to discard all state from the previous link.
  window.addEventListener("hashchange", () => {
    if (location.hash) location.reload();
  });
  const actions = {
    verify: [
      "Verify your email",
      "Verify in your browser or continue in the Holy Hymns app.",
    ],
    "reset-password": [
      "Reset your password",
      "Open Holy Hymns to choose a new password.",
    ],
    "accept-invitation": [
      "Accept your invitation",
      "Open Holy Hymns to accept. If you already have an account, sign in first.",
    ],
  };
  const get = (id) => document.getElementById(id);
  const action = location.pathname.split("/").pop();
  const params = new URLSearchParams(location.hash.slice(1));
  let token = params.get("token") || "";
  // The fragment never reaches the server; remove it from browser history too.
  history.replaceState(null, "", location.pathname);
  const status = get("status");
  if (
    !Object.hasOwn(actions, action) ||
    params.getAll("token").length !== 1 ||
    !/^[A-Za-z0-9_-]{43}$/.test(token)
  ) {
    get("title").textContent = "This link is incomplete";
    get("description").textContent =
      "Open the full link from your most recent email, or paste its code into Holy Hymns.";
    status.textContent = "No account changes have been made.";
    return;
  }
  const [title, description] = actions[action];
  document.title = title + " | Holy Hymns";
  get("title").textContent = title;
  get("description").textContent = description;
  get("controls").hidden = false;
  get("manual").hidden = false;
  get("manual-help").textContent =
    `Paste this code into the app's ${title.toLowerCase()} screen.`;
  get("token").value = token;
  const openApp = get("open-app");
  openApp.href =
    "holyhymns://auth/" + action + "?token=" + encodeURIComponent(token);
  openApp.addEventListener("click", () => {
    status.textContent = token
      ? "If the app did not open, use your phone's browser or copy the code below."
      : "Open Holy Hymns on your phone and sign in from the Account screen.";
  });
  status.textContent =
    "This one-time link expires at the time stated in your email.";
  get("copy").addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(token);
      status.textContent = "Code copied. Paste it into Holy Hymns.";
    } catch {
      get("token").focus();
      get("token").select();
      status.textContent =
        "Select and copy the code, then paste it into Holy Hymns.";
    }
  });
  const verify = get("verify");
  if (action !== "verify") return;
  verify.hidden = false;
  verify.addEventListener("click", async () => {
    if (verify.disabled) return;
    verify.disabled = true;
    status.textContent = "Verifying your email…";
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 15000);
    try {
      // Never consume a token on page load: email scanners also visit links.
      const response = await fetch("../v1/auth/verify", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token }),
        credentials: "omit",
        cache: "no-store",
        redirect: "error",
        referrerPolicy: "no-referrer",
        signal: controller.signal,
      });
      if (response.ok) {
        get("title").textContent = "Email verified";
        get("description").textContent =
          "Return to Holy Hymns and sign in with your email and password.";
        document.title = "Email verified | Holy Hymns";
        status.textContent = "You're ready to sign in.";
        verify.hidden = true;
        get("manual").hidden = true;
        get("token").value = token = "";
        openApp.href = "holyhymns://";
        get("app-help").textContent =
          "Tap Open Holy Hymns on your phone, or launch it yourself and choose Account to sign in.";
      } else if (response.status === 400) {
        status.textContent =
          "This link is invalid, expired, or already used. If you already verified, sign in. Otherwise request a new email in Holy Hymns.";
        verify.hidden = true;
      } else if (response.status === 429) {
        status.textContent =
          "Too many attempts. Wait a while before trying again, or continue in Holy Hymns.";
      } else {
        status.textContent =
          "Verification is unavailable right now. Try again shortly or continue in Holy Hymns.";
      }
    } catch {
      status.textContent =
        "Could not confirm verification. Check your connection and try again. If it already succeeded, sign in to Holy Hymns.";
    } finally {
      clearTimeout(timeout);
      verify.disabled = false;
    }
  });
})();
