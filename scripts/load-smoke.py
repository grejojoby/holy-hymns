#!/usr/bin/env python3
"""Local-only, bounded API smoke load: 25 SSE readers, searches, optional sign-ins.

No catalogue changes are made. Optional sign-ins create sessions and immediately
log them out. Credentials come only from environment variables and are never
printed. This is a diagnostic smoke check, not a capacity benchmark.
"""
import argparse
import http.client
import json
import os
import statistics
import sys
import threading
import time
import urllib.parse


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:8080/v1")
    parser.add_argument("--duration", type=int, default=8, help="Concurrent hold duration, 3–15 seconds")
    args = parser.parse_args()
    parsed = urllib.parse.urlsplit(args.base_url)
    if parsed.scheme not in ("http", "https") or parsed.hostname not in ("localhost", "127.0.0.1", "::1") or parsed.username or parsed.password or parsed.query or parsed.fragment:
        parser.error("Use a local HTTP(S) API URL only, such as http://127.0.0.1:8080/v1")
    if not 3 <= args.duration <= 15:
        parser.error("duration must be 3–15 seconds; the run remains below 30 seconds")
    email = os.environ.get("HOLY_HYMNS_SMOKE_EMAIL", "")
    password = os.environ.get("HOLY_HYMNS_SMOKE_PASSWORD", "")
    if bool(email) != bool(password):
        parser.error("Set both HOLY_HYMNS_SMOKE_EMAIL and HOLY_HYMNS_SMOKE_PASSWORD, or neither")
    base = parsed.path.rstrip("/")
    started = time.monotonic()
    stop_at = started + args.duration
    deadline = started + args.duration + 8
    lock = threading.Lock()
    ready = threading.Event()
    result = {"sse_connected": 0, "search_requests": 0, "sign_ins": 0, "failures": []}
    latencies = []
    connections = set()

    def connect():
        cls = http.client.HTTPSConnection if parsed.scheme == "https" else http.client.HTTPConnection
        conn = cls(parsed.hostname, parsed.port, timeout=2)
        with lock:
            connections.add(conn)
        return conn

    def close(conn):
        conn.close()
        with lock:
            connections.discard(conn)

    def failure(operation, detail):
        # Never include server response bodies, tokens, credentials or query values.
        with lock:
            for item in result["failures"]:
                if item["operation"] == operation and item["reason"] == detail:
                    item["count"] += 1
                    return
            result["failures"].append({"operation": operation, "reason": detail, "count": 1})

    def json_request(method, path, body=None, token=None):
        conn = connect()
        try:
            headers = {"Accept": "application/json"}
            encoded = None
            if body is not None:
                encoded = json.dumps(body).encode()
                headers["Content-Type"] = "application/json"
            if token:
                headers["Authorization"] = "Bearer " + token
            conn.request(method, base + path, body=encoded, headers=headers)
            response = conn.getresponse()
            payload = response.read(1024 * 1024)
            if response.status != 200:
                raise RuntimeError("HTTP " + str(response.status))
            return json.loads(payload)
        finally:
            close(conn)

    def stream():
        conn = connect()
        try:
            conn.request("GET", base + "/events", headers={"Accept": "text/event-stream"})
            response = conn.getresponse()
            if response.status != 200 or not response.getheader("Content-Type", "").startswith("text/event-stream"):
                raise RuntimeError("Unexpected SSE status/content type")
            content_event = False
            valid = False
            for _ in range(12):
                line = response.readline(4096).decode("utf-8").strip()
                if line == "event: content":
                    content_event = True
                if line.startswith("data:"):
                    data = json.loads(line[5:])
                    if content_event and isinstance(data.get("revision"), int):
                        valid = True
                        break
            if not valid:
                raise RuntimeError("Missing initial content revision")
            with lock:
                result["sse_connected"] += 1
                if result["sse_connected"] == 25:
                    ready.set()
            # Keep all reader sockets open while the search/login requests execute.
            while time.monotonic() < stop_at:
                time.sleep(0.05)
        except Exception as error:
            failure("sse", str(error) if isinstance(error, RuntimeError) else type(error).__name__)
        finally:
            close(conn)

    def search(worker):
        ready.wait(timeout=3)
        queries = ["", "കർത്താവേ", "Karthave", "yeshu", "not-a-song-smoke-check"]
        for iteration in range(20):
            if time.monotonic() >= stop_at:
                return
            before = time.monotonic()
            try:
                params = urllib.parse.urlencode({"q": queries[(worker + iteration) % len(queries)], "limit": 5})
                payload = json_request("GET", "/songs?" + params)
                if not isinstance(payload.get("items"), list) or not isinstance(payload.get("total"), int):
                    raise RuntimeError("Invalid song list response")
                with lock:
                    result["search_requests"] += 1
                    latencies.append((time.monotonic() - before) * 1000)
            except Exception as error:
                failure("search", str(error) if isinstance(error, RuntimeError) else type(error).__name__)
            time.sleep(0.1)

    def sign_in():
        ready.wait(timeout=3)
        for _ in range(3):
            if time.monotonic() >= stop_at:
                return
            token = None
            try:
                payload = json_request("POST", "/auth/login", {"email": email, "password": password})
                token = payload.get("token")
                if not isinstance(token, str) or not token:
                    raise RuntimeError("Missing session token")
                json_request("GET", "/auth/me", token=token)
                json_request("POST", "/auth/logout", token=token)
                token = None
                with lock:
                    result["sign_ins"] += 1
            except Exception as error:
                failure("sign_in", str(error) if isinstance(error, RuntimeError) else type(error).__name__)
            finally:
                if token:
                    try:
                        json_request("POST", "/auth/logout", token=token)
                    except Exception:
                        failure("logout_cleanup", "Session cleanup failed; use the test account's logout-all")

    threads = [threading.Thread(target=stream, daemon=True) for _ in range(25)]
    threads += [threading.Thread(target=search, args=(i,), daemon=True) for i in range(5)]
    if email:
        threads.append(threading.Thread(target=sign_in, daemon=True))
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join(max(0, deadline - time.monotonic()))
    if any(thread.is_alive() for thread in threads):
        failure("run", "Bounded deadline exceeded")
        with lock:
            active = list(connections)
        for conn in active:
            conn.close()
    if result["sse_connected"] != 25:
        failure("run", "Fewer than 25 live readers connected")
    if not result["search_requests"]:
        failure("run", "No successful search requests")
    if email and result["sign_ins"] != 3:
        failure("run", "Fewer than three completed sign-in/sign-out cycles")
    result["duration_seconds"] = round(time.monotonic() - started, 2)
    result["login_test"] = "enabled" if email else "skipped: no environment credentials"
    if latencies:
        values = sorted(latencies)
        result["search_latency_ms"] = {"median": round(statistics.median(values), 1), "p95": round(values[min(len(values) - 1, int(len(values) * 0.95))], 1), "max": round(max(values), 1)}
    print(json.dumps(result, indent=2))
    return 1 if result["failures"] else 0


if __name__ == "__main__":
    sys.exit(main())
