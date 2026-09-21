import { fetch } from "expo/fetch";

export const API_URL = (
  process.env.EXPO_PUBLIC_API_URL || "http://localhost:8080/v1"
).replace(/\/$/, "");
let sessionToken: string | null = null;
let onUnauthorized: (() => void) | undefined;
export function setSessionToken(token: string | null) {
  sessionToken = token;
}
export function setUnauthorizedHandler(handler: () => void) {
  onUnauthorized = handler;
}
export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public code?: string,
  ) {
    super(message);
  }
}
export async function api<T = void>(
  path: string,
  options: {
    method?: string;
    body?: unknown;
    signal?: AbortSignal;
    authenticated?: boolean;
  } = {},
): Promise<T> {
  const requestToken = sessionToken;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 15000);
  const abort = () => controller.abort();
  options.signal?.addEventListener("abort", abort, { once: true });
  if (options.signal?.aborted) controller.abort();
  try {
    const response = await fetch(`${API_URL}${path}`, {
      method: options.method || "GET",
      headers: {
        Accept: "application/json",
        ...(options.body !== undefined
          ? { "Content-Type": "application/json" }
          : {}),
        ...(requestToken && options.authenticated !== false
          ? { Authorization: `Bearer ${requestToken}` }
          : {}),
      },
      body:
        options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: controller.signal,
    });
    const text = await response.text();
    let data: any;
    try {
      data = text ? JSON.parse(text) : undefined;
    } catch {
      throw new ApiError(
        "The server returned an unreadable response. Please try again.",
        response.status,
      );
    }
    if (!response.ok) {
      if (
        response.status === 401 &&
        (!data?.code ||
          data.code === "unauthorized" ||
          data.code === "reauthentication_required") &&
        requestToken &&
        requestToken === sessionToken &&
        options.authenticated !== false
      )
        onUnauthorized?.();
      throw new ApiError(
        data?.error || `Request failed (${response.status}).`,
        response.status,
        data?.code,
      );
    }
    return data as T;
  } catch (error) {
    if (error instanceof ApiError) throw error;
    if (options.signal?.aborted) throw error;
    throw new Error(
      controller.signal.aborted
        ? "The connection timed out. Please try again."
        : "Unable to connect. Check your internet connection and try again.",
    );
  } finally {
    clearTimeout(timeout);
    options.signal?.removeEventListener("abort", abort);
  }
}
export const post = <T = void>(path: string, body?: unknown) =>
  api<T>(path, { method: "POST", body });
export const put = <T = void>(path: string, body?: unknown) =>
  api<T>(path, { method: "PUT", body });
export function query(
  values: Record<string, string | number | boolean | undefined>,
) {
  const params = new URLSearchParams();
  Object.entries(values).forEach(([key, value]) => {
    if (value !== undefined && value !== "") params.set(key, String(value));
  });
  return params.toString();
}
export function message(error: unknown) {
  return error instanceof Error
    ? error.message
    : "Something went wrong. Please try again.";
}
