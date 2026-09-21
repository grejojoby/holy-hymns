#!/usr/bin/env python3
"""Send a small release bundle to the VM; credentials exist only for this job."""

from __future__ import annotations

import io
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tarfile
import tempfile


ROOT = Path(__file__).resolve().parents[1]
RELEASE_FILES = ("compose.yaml", "deploy/Caddyfile", "scripts/deploy-vm.sh")
SECRET_NAMES = {"VM_SSH_KEY", "VM_KNOWN_HOSTS", "GHCR_TOKEN"}


def required(env: dict[str, str], name: str) -> str:
    value = env.get(name, "")
    if not value.strip():
        raise ValueError(f"Configure {name} before enabling deployment")
    return value


def checked(value: str, pattern: str, name: str) -> str:
    if not re.fullmatch(pattern, value):
        raise ValueError(f"Invalid {name}")
    return value


def configuration(env: dict[str, str]) -> dict[str, str]:
    config = {name: required(env, name) for name in (
        "VM_HOST", "VM_USER", "VM_SSH_KEY", "VM_KNOWN_HOSTS", "RELEASE_ID",
        "API_IMAGE", "BACKUP_IMAGE", "GHCR_USERNAME", "GHCR_TOKEN",
    )}
    checked(config["VM_HOST"], r"[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?", "VM_HOST (hostname or IPv4)")
    checked(config["VM_USER"], r"[a-z_][a-z0-9_-]{0,31}", "VM_USER")
    checked(config["RELEASE_ID"], r"[a-f0-9]{40}-[1-9][0-9]*-[1-9][0-9]*", "RELEASE_ID")
    checked(config["GHCR_USERNAME"], r"[A-Za-z0-9][A-Za-z0-9-]{0,38}(\[bot\])?", "GHCR_USERNAME")
    checked(config["API_IMAGE"], r"ghcr\.io/grejojoby/holy-hymns@sha256:[a-f0-9]{64}", "API_IMAGE")
    checked(config["BACKUP_IMAGE"], r"ghcr\.io/grejojoby/holy-hymns-backup@sha256:[a-f0-9]{64}", "BACKUP_IMAGE")
    path = env.get("VM_DEPLOY_PATH") or "/opt/holy-hymns"
    checked(path, r"/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+", "VM_DEPLOY_PATH (absolute path without spaces)")
    port = env.get("VM_SSH_PORT") or "22"
    checked(port, r"[0-9]{1,5}", "VM_SSH_PORT")
    if not 1 <= int(port) <= 65535:
        raise ValueError("VM_SSH_PORT must be between 1 and 65535")
    if "\n" in config["GHCR_TOKEN"] or "\r" in config["GHCR_TOKEN"]:
        raise ValueError("Invalid GHCR_TOKEN")
    config.update(VM_DEPLOY_PATH=path, VM_SSH_PORT=port)
    return config


def create_bundle(destination: Path, config: dict[str, str], root: Path = ROOT) -> None:
    with tarfile.open(destination, "w:gz") as bundle:
        for relative in RELEASE_FILES:
            path = root / relative
            if path.is_symlink() or not path.is_file():
                raise ValueError(f"Missing regular deployment file: {relative}")
            bundle.add(path, arcname=relative, recursive=False)
        images = (
            f"HOLY_HYMNS_IMAGE={config['API_IMAGE']}\n"
            f"HOLY_HYMNS_BACKUP_IMAGE={config['BACKUP_IMAGE']}\n"
        ).encode()
        info = tarfile.TarInfo("images.env")
        info.size = len(images)
        info.mode = 0o600
        bundle.addfile(info, io.BytesIO(images))


def deploy(env: dict[str, str], root: Path = ROOT) -> None:
    config = configuration(env)
    app_root = config["VM_DEPLOY_PATH"]
    release = f"{app_root}/releases/{config['RELEASE_ID']}"
    target = f"{config['VM_USER']}@{config['VM_HOST']}"
    child_env = {key: value for key, value in env.items() if key not in SECRET_NAMES}
    with tempfile.TemporaryDirectory(prefix="holy-hymns-deploy-") as temporary:
        directory = Path(temporary)
        key = directory / "id_deploy"
        hosts = directory / "known_hosts"
        for file, value in ((key, config["VM_SSH_KEY"]), (hosts, config["VM_KNOWN_HOSTS"])):
            file.write_text(value.rstrip() + "\n", encoding="utf-8")
            file.chmod(0o600)
        bundle = directory / "release.tar.gz"
        create_bundle(bundle, config, root)
        options = [
            "-F", "/dev/null", "-i", str(key), "-o", "BatchMode=yes",
            "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=yes",
            "-o", f"UserKnownHostsFile={hosts}", "-o", "ConnectTimeout=20",
            "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
        ]
        ssh = ["ssh", *options, "-p", config["VM_SSH_PORT"], target]

        def remote(command: str, token: bool = False) -> None:
            subprocess.run(
                [*ssh, command], check=True, env=child_env,
                input=(config["GHCR_TOKEN"] + "\n").encode() if token else b"",
            )

        q = shlex.quote
        remote(
            f"set -eu; umask 077; test -d {q(app_root)}; test -f {q(app_root + '/.env')}; "
            f"mkdir -p {q(app_root + '/releases')}; mkdir {q(release)}"
        )
        subprocess.run(
            ["scp", *options, "-P", config["VM_SSH_PORT"], str(bundle), f"{target}:{release}/release.tar.gz"],
            check=True, env=child_env, stdin=subprocess.DEVNULL,
        )
        remote(
            f"set -eu; umask 077; tar -xzf {q(release + '/release.tar.gz')} -C {q(release)}; "
            f"rm {q(release + '/release.tar.gz')}; "
            f"exec bash {q(release + '/scripts/deploy-vm.sh')} "
            f"{q(app_root)} {q(release)} {q(config['GHCR_USERNAME'])}",
            token=True,
        )


def main() -> int:
    try:
        deploy(dict(os.environ))
    except (ValueError, OSError) as error:
        print(f"Deployment setup failed: {error}", file=sys.stderr)
        return 1
    except subprocess.CalledProcessError as error:
        print(f"SSH deployment failed (exit {error.returncode}); inspect VM state before retrying.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
