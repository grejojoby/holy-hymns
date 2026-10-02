import type { ErrorEvent } from "@sentry/react-native";

// Auth links can contain one-time credentials. Keep no URL query or fragment.
export function scrubText(value: string): string {
  return value
    .replace(/(?:https?:\/\/|holyhymns:\/\/)[^\s)\]"']+/gi, "[url]")
    .replace(/\bBearer\s+\S+/gi, "Bearer [redacted]")
    .replace(/\b[\w.+-]+@[\w.-]+\.[a-z]{2,}\b/gi, "[email]")
    .replace(
      /\b(password|token|secret|authorization|cookie)\s*[=:]\s*[^\s,;]+/gi,
      "$1=[redacted]",
    );
}

export function scrubEvent(event: ErrorEvent): ErrorEvent {
  delete event.request;
  delete event.user;
  delete event.extra;
  delete event.breadcrumbs;
  if (event.message) event.message = scrubText(event.message);
  for (const exception of event.exception?.values || []) {
    if (exception.value) exception.value = scrubText(exception.value);
    for (const frame of exception.stacktrace?.frames || []) {
      delete frame.vars;
    }
  }
  return event;
}
