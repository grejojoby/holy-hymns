#!/usr/bin/env python3
"""Bounded, encrypted PostgreSQL backups to an existing, verified-free OCI prefix.

No cloud resources are created. Remote deletion is limited to this application's
own timestamped archive names, after a new archive is uploaded and hash-verified.
The private age identity stays off the VM.
"""
import datetime as dt
import hashlib
import json
import os
import re
import signal
import subprocess
import sys
import tempfile
import threading
import uuid
from dataclasses import dataclass
from pathlib import Path

UTC = dt.timezone.utc
NAME = re.compile(r"^holy-hymns-(\d{8}T\d{6}Z)-[a-f0-9]{8}\.dump\.age$")
REMOTE = re.compile(r"^[A-Za-z0-9_-]+:[A-Za-z0-9._-]+/holy-hymns$")
STOP = threading.Event()


@dataclass(frozen=True)
class Config:
    remote: str
    recipient: str
    cap: int
    max_archive: int
    hour: int


def configuration(env=os.environ):
    if env.get("OCI_FREE_QUOTA_VERIFIED") != "true":
        raise ValueError("Backups disabled: verify existing unused OCI free capacity first")
    remote = env.get("BACKUP_REMOTE", "")
    if not REMOTE.fullmatch(remote):
        raise ValueError("BACKUP_REMOTE must be a configured remote:existing-bucket/holy-hymns")
    recipient = env.get("BACKUP_AGE_RECIPIENT", "")
    if not re.fullmatch(r"age1[0-9a-z]{58}", recipient):
        raise ValueError("BACKUP_AGE_RECIPIENT must be an age X25519 public recipient")
    cap = int(env.get("BACKUP_STORAGE_CAP_BYTES", "104857600"))
    free = int(env.get("OCI_VERIFIED_FREE_BYTES", "0"))
    maximum = int(env.get("BACKUP_MAX_ARCHIVE_BYTES", "12582912"))
    hour = int(env.get("BACKUP_HOUR_UTC", "2"))
    if not 0 < cap <= free or not 0 < maximum <= cap // 8:
        raise ValueError("Storage cap must fit verified free capacity and reserve eight full archives")
    if maximum > 32 * 1024 * 1024:
        raise ValueError("Archive limit exceeds the bounded backup container's 32 MiB safety limit")
    if not 0 <= hour <= 23:
        raise ValueError("BACKUP_HOUR_UTC must be 0 through 23")
    return Config(remote, recipient, cap, maximum, hour)


def command(args):
    # Config files contain credentials; never echo subprocess environments or logs.
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
    if result.returncode:
        raise RuntimeError(f"{Path(args[0]).name} failed (exit {result.returncode}); inspect credentials/connectivity")
    return result.stdout


def listing(config):
    result = json.loads(command(["rclone", "lsjson", config.remote, "--files-only", "--recursive"]))
    if not isinstance(result, list):
        raise ValueError("Unexpected remote listing")
    for item in result:
        if not isinstance(item, dict) or not isinstance(item.get("Size"), int) or item["Size"] < 0:
            raise ValueError("Unknown object size; refusing to estimate storage")
    return result


def expired_archives(items, now):
    cutoff = now - dt.timedelta(days=7)
    expired = []
    for item in items:
        name = item.get("Path", "")
        match = NAME.fullmatch(name)
        if not match:
            continue
        try:
            created = dt.datetime.strptime(match.group(1), "%Y%m%dT%H%M%SZ").replace(tzinfo=UTC)
        except ValueError:
            continue
        if created < cutoff:
            expired.append(name)
    return expired


def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.digest()


def backup_once(config):
    now = dt.datetime.now(UTC)
    items = listing(config)
    existing_bytes = sum(item["Size"] for item in items)
    # Reserve a complete worst-case archive before generating or sending anything.
    if existing_bytes + config.max_archive > config.cap:
        raise ValueError("Backup storage cap reached; review quota/retention manually before continuing")
    with tempfile.TemporaryDirectory(prefix="holy-hymns-backup-") as directory:
        raw = Path(directory) / "database.dump"
        encrypted = Path(directory) / "database.dump.age"
        check = Path(directory) / "verification.dump.age"
        # PGHOST/PGUSER/PGDATABASE/PGPASSWORD are passed through the environment.
        command(["pg_dump", "--format=custom", "--compress=6", "--no-owner", "--no-acl", "--file", str(raw)])
        if not 0 < raw.stat().st_size <= config.max_archive - 4096:
            raise ValueError("Database dump exceeds configured archive cap; no upload attempted")
        command(["pg_restore", "--list", str(raw)])
        command(["age", "--recipient", config.recipient, "--output", str(encrypted), str(raw)])
        raw.unlink()
        size = encrypted.stat().st_size
        if size > config.max_archive:
            raise ValueError("Encrypted archive exceeds configured cap")
        name = f"holy-hymns-{now:%Y%m%dT%H%M%SZ}-{uuid.uuid4().hex[:8]}.dump.age"
        destination = config.remote + "/" + name
        command(["rclone", "copyto", str(encrypted), destination, "--immutable", "--retries", "2", "--s3-no-check-bucket"])
        command(["rclone", "copyto", destination, str(check), "--retries", "2"])
        if sha256(encrypted) != sha256(check):
            raise RuntimeError("Uploaded archive verification failed; previous archives were retained")
        for old in expired_archives(items, now):
            command(["rclone", "deletefile", config.remote + "/" + old])
        print(f"Backup verified: {name}, {size} encrypted bytes", flush=True)


def schedule(config):
    print(f"Encrypted backups scheduled daily at {config.hour:02}:00 UTC", flush=True)
    while not STOP.is_set():
        now = dt.datetime.now(UTC)
        target = now.replace(hour=config.hour, minute=0, second=0, microsecond=0)
        if target <= now:
            target += dt.timedelta(days=1)
        if STOP.wait((target - now).total_seconds()):
            return
        try:
            backup_once(config)
        except Exception as error:
            print(f"BACKUP FAILED: {error}", file=sys.stderr, flush=True)


def main():
    os.umask(0o077)
    config = configuration()
    mode = sys.argv[1] if len(sys.argv) > 1 else "once"
    if mode == "once":
        backup_once(config)
    elif mode == "schedule":
        for sig in (signal.SIGTERM, signal.SIGINT):
            signal.signal(sig, lambda *_: STOP.set())
        schedule(config)
    else:
        raise ValueError("Usage: backup.py once|schedule")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(f"BACKUP FAILED: {error}", file=sys.stderr)
        sys.exit(1)
