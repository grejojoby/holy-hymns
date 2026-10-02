import * as Sentry from "@sentry/react-native";
import { scrubEvent } from "./telemetry-privacy";

const dsn = process.env.EXPO_PUBLIC_SENTRY_DSN;

if (dsn) {
  Sentry.init({
    dsn,
    enabled: !__DEV__,
    environment: process.env.EXPO_PUBLIC_SENTRY_ENVIRONMENT || "production",
    // Native releases default to the installed app's version and build number.
    release: process.env.EXPO_PUBLIC_SENTRY_RELEASE || undefined,
    sendDefaultPii: false,
    tracesSampleRate: 0,
    profilesSampleRate: 0,
    replaysSessionSampleRate: 0,
    replaysOnErrorSampleRate: 0,
    enableLogs: false,
    enableAutoPerformanceTracing: false,
    enableAutoSessionTracking: false,
    enableAutoBreadcrumbTracking: false,
    enableCaptureFailedRequests: false,
    attachScreenshot: false,
    attachViewHierarchy: false,
    maxBreadcrumbs: 0,
    // Distinct failed requests can have identical stacks but different IDs.
    integrations: (defaults) =>
      defaults.filter((item) => item.name !== "Dedupe"),
    beforeBreadcrumb: () => null,
    beforeSend: scrubEvent,
  });
}

export function reportApiFailure(status: number, requestID: string | null) {
  Sentry.captureException(new Error("Holy Hymns API request failed"), {
    tags: {
      status: String(status),
      // Only accept the backend's generated base32 identifier.
      ...(requestID && /^[A-Z2-7]{26}$/.test(requestID)
        ? { request_id: requestID }
        : {}),
    },
  });
}

export { Sentry };
