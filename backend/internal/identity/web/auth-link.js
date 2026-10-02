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
      "Choose a new password for your Holy Hymns account.",
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
      ? action === "reset-password"
        ? "If the app did not open, choose Reset in browser instead."
        : "If the app did not open, use your phone's browser or copy the code below."
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
  if (action === "reset-password") {
    const form = get("reset-form");
    const password = get("password");
    const confirmation = get("confirm-password");
    const reset = get("reset");
    const browser = get("continue-browser");
    // Device hints only choose the initial view; the browser path stays available.
    // iPadOS can report a desktop Mac user agent, with multiple touch points.
    const mobile =
      navigator.userAgentData?.mobile === true ||
      /Android|iPhone|iPad|iPod/i.test(navigator.userAgent) ||
      (/Macintosh/i.test(navigator.userAgent) && navigator.maxTouchPoints > 1);
    const showForm = () => {
      form.hidden = false;
      get("controls").hidden = true;
      get("description").textContent = description;
      status.textContent = "Enter and confirm your new password.";
    };
    const discardReset = () => {
      get("token").value = token = "";
      params.delete("token");
      password.value = confirmation.value = "";
      form.hidden = true;
      get("manual").hidden = true;
      get("controls").hidden = true;
      openApp.href = "holyhymns://";
      get("return-web").hidden = false;
    };
    browser.addEventListener("click", () => {
      showForm();
      password.focus();
    });
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      if (reset.disabled || form.hidden || !token) return;
      if (
        [...password.value].length < 12 ||
        new TextEncoder().encode(password.value).length > 1024
      ) {
        status.textContent = "Use at least 12 characters (maximum 1024 bytes).";
        password.focus();
        return;
      }
      if (password.value !== confirmation.value) {
        status.textContent = "Your passwords do not match. Enter them again.";
        confirmation.focus();
        return;
      }
      reset.disabled = password.disabled = confirmation.disabled = true;
      status.textContent = "Resetting your password…";
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 15000);
      try {
        // Only an explicit form submission can consume the reset token.
        const response = await fetch("../v1/auth/reset-password", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ token, password: password.value }),
          credentials: "omit",
          cache: "no-store",
          redirect: "error",
          referrerPolicy: "no-referrer",
          signal: controller.signal,
        });
        if (response.ok) {
          discardReset();
          get("title").textContent = "Password reset";
          document.title = "Password reset | Holy Hymns";
          get("description").textContent =
            "Sign in to Holy Hymns with your new password.";
          status.textContent = "Your password has been updated.";
        } else if (response.status === 400) {
          const error = await response.json();
          if (error.code === "invalid_password") {
            status.textContent =
              "Use at least 12 characters (maximum 1024 bytes).";
          } else if (error.code === "invalid_token") {
            discardReset();
            status.textContent =
              "This link is invalid, expired, or already used. Return to Holy Hymns and request a new password reset email.";
          } else {
            status.textContent =
              "Could not reset your password. Try again shortly.";
          }
        } else if (response.status === 429) {
          status.textContent =
            "Too many attempts. Wait a while before trying again.";
        } else {
          status.textContent =
            "Password reset is unavailable right now. Try again shortly.";
        }
      } catch {
        status.textContent =
          "Could not confirm the reset. Check your connection and try again. If it already succeeded, sign in with your new password.";
      } finally {
        clearTimeout(timeout);
        reset.disabled = password.disabled = confirmation.disabled = false;
      }
    });
    if (mobile) {
      browser.hidden = false;
      get("description").textContent =
        "Continue in Holy Hymns to choose a new password.";
      get("app-help").textContent =
        "If the app does not open, tap Open Holy Hymns or reset in your browser instead.";
      status.textContent = "Opening Holy Hymns…";
      try {
        location.assign(openApp.href);
      } catch {
        status.textContent =
          "Tap Open Holy Hymns or reset in your browser instead.";
      }
    } else {
      showForm();
    }
    return;
  }
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
