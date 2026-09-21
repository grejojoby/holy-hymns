"""Deployment protocol tests using fake Docker; no daemon, registry or VM needed."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


PROJECT = Path(__file__).resolve().parents[1]
HELPER = PROJECT / "scripts" / "deploy-vm.sh"
API_IMAGE = "ghcr.io/grejojoby/holy-hymns@sha256:" + "a" * 64
BACKUP_IMAGE = "ghcr.io/grejojoby/holy-hymns-backup@sha256:" + "b" * 64
TOKEN = "ephemeral-test-token-do-not-log"


FAKE_DOCKER = r'''#!/usr/bin/env python3
import json, os, pathlib, sys
args = sys.argv[1:]
root = pathlib.Path(os.environ["DEPLOY_TEST_ROOT"])
record = {"args": args, "dockerConfig": os.environ.get("DOCKER_CONFIG"), "caddy": os.environ.get("CADDY_CONFIG_FILE")}
with (root / "calls.jsonl").open("a") as output:
    output.write(json.dumps(record) + "\n")
failure = os.environ.get("DEPLOY_TEST_FAIL", "")
state_file = root / "running.json"
running = set(json.loads(state_file.read_text())) if state_file.exists() else set()
if args[0] == "login":
    credential = sys.stdin.read().strip()
    if credential != os.environ["DEPLOY_TEST_TOKEN"]: sys.exit(10)
    path = pathlib.Path(os.environ["DOCKER_CONFIG"]) / "config.json"
    path.write_text(json.dumps({"auths":{"ghcr.io":{"auth":credential}}}))
    if failure == "login":
        print(credential, file=sys.stderr)
        sys.exit(11)
    sys.exit(0)
if args[:2] == ["compose", "version"] or args[0] == "info": sys.exit(0)
if args[0] != "compose": sys.exit(12)
envfiles = [pathlib.Path(args[i+1]) for i, a in enumerate(args) if a == "--env-file"]
values = {}
for file in envfiles:
    for line in file.read_text().splitlines():
        key, separator, value = line.partition("=")
        if separator: values[key] = value
image = values["HOLY_HYMNS_IMAGE"]
profiles = values.get("COMPOSE_PROFILES", "").split(",")
if "config" in args:
    if failure == "config":
        print(values.get("POSTGRES_PASSWORD"), file=sys.stderr)
        sys.exit(13)
    services = {
        "db":{"image":"postgres:17-alpine","healthcheck":{"test":["CMD","pg_isready"]}},
        "migrate":{"image":image},
        "api":{"image":image,"healthcheck":{"test":["CMD","health"]}},
    }
    if "edge" in profiles: services["caddy"] = {"image":"caddy:2-alpine","volumes":[{"type":"bind","source":os.environ["CADDY_CONFIG_FILE"],"target":"/etc/caddy/Caddyfile","read_only":True}]}
    if "backup" in profiles: services["backup"] = {"image":values["HOLY_HYMNS_BACKUP_IMAGE"]}
    if failure == "digest": services["api"]["image"] = "attacker/image:latest"
    print(json.dumps({"services":services,"secret":values.get("POSTGRES_PASSWORD")}))
elif "pull" in args:
    if failure == "pull": sys.exit(14)
elif "run" in args:
    if failure == "migration":
        print("diagnostic line\n" * 7000, file=sys.stderr)
        print(values.get("POSTGRES_PASSWORD"), file=sys.stderr)
        sys.exit(15)
elif "up" in args:
    if failure == "health" and args[-1] == "api": sys.exit(16)
    if failure == "optional" and "caddy" in args: sys.exit(17)
    running.update(name for name in ("db", "api", "caddy", "backup") if name in args)
    state_file.write_text(json.dumps(sorted(running)))
elif "stop" in args:
    if failure == "stop": sys.exit(19)
    running.difference_update(name for name in ("caddy", "backup") if name in args[args.index("stop") + 1:])
    state_file.write_text(json.dumps(sorted(running)))
else: sys.exit(18)
'''

FAKE_FLOCK = r'''#!/usr/bin/env python3
import fcntl, sys
fcntl.flock(int(sys.argv[-1]), fcntl.LOCK_EX)
'''


class DeployVMTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="holy-hymns-deploy-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve() / "app"
        self.root.mkdir()
        self.old = self.make_release("1" * 40 + "-11-1")
        self.older = self.make_release("2" * 40 + "-10-1")
        self.release = self.make_release("3" * 40 + "-12-1")
        (self.root / "current").symlink_to("releases/" + self.old.name)
        (self.root / "previous").symlink_to("releases/" + self.older.name)
        self.envfile = self.root / ".env"
        self.envfile.write_text("POSTGRES_PASSWORD=private-vm-password\nCOMPOSE_PROFILES=edge,backup\n")
        self.envfile.chmod(0o600)
        self.bin = Path(self.temporary.name) / "bin"
        self.bin.mkdir()
        for name, script in (("docker", FAKE_DOCKER), ("flock", FAKE_FLOCK)):
            path = self.bin / name
            path.write_text(script)
            path.chmod(0o755)

    def make_release(self, name):
        release = self.root / "releases" / name
        (release / "deploy").mkdir(parents=True)
        (release / "scripts").mkdir()
        shutil.copyfile(PROJECT / "compose.yaml", release / "compose.yaml")
        shutil.copyfile(PROJECT / "deploy" / "Caddyfile", release / "deploy" / "Caddyfile")
        shutil.copyfile(HELPER, release / "scripts" / "deploy-vm.sh")
        (release / "images.env").write_text(f"HOLY_HYMNS_IMAGE={API_IMAGE}\nHOLY_HYMNS_BACKUP_IMAGE={BACKUP_IMAGE}\n")
        return release

    def run_deploy(self, failure="", release=None, username="grejojoby"):
        env = os.environ.copy()
        env.update(PATH=str(self.bin) + os.pathsep + env["PATH"], DEPLOY_TEST_ROOT=str(self.root), DEPLOY_TEST_TOKEN=TOKEN, DEPLOY_TEST_FAIL=failure)
        # These inherited overrides must not replace the VM/release settings.
        env.update(COMPOSE_PROFILES="unexpected", HOLY_HYMNS_IMAGE="untrusted:latest")
        return subprocess.run(["bash", str(HELPER), str(self.root), str(release or self.release), username], input=TOKEN + "\n", text=True, capture_output=True, env=env, timeout=15)

    def calls(self):
        file = self.root / "calls.jsonl"
        return [json.loads(line) for line in file.read_text().splitlines()] if file.exists() else []

    def assert_private(self, result):
        self.assertNotIn(TOKEN, result.stdout + result.stderr)
        self.assertNotIn("private-vm-password", result.stdout + result.stderr)
        self.assertEqual(list(self.root.glob(".deploy-tmp.*")), [])
        for call in self.calls():
            self.assertNotIn(TOKEN, json.dumps(call))
            self.assertFalse(Path(call["dockerConfig"]).exists())
        diagnostic = self.root / "deploy-last-error.log"
        if diagnostic.exists():
            self.assertLessEqual(diagnostic.stat().st_size, 65536)
            self.assertEqual(diagnostic.stat().st_mode & 0o777, 0o600)
            self.assertNotIn(TOKEN, diagnostic.read_text())

    def test_success_orders_pull_migration_health_then_release_pointers(self):
        before = self.envfile.read_bytes()
        result = self.run_deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = [call["args"] for call in self.calls()]
        actions = [next((part for part in ("login", "config", "pull", "run", "up") if part in command), "preflight") for command in commands]
        self.assertLess(actions.index("pull"), actions.index("up"))
        run_index = actions.index("run")
        self.assertEqual(commands[run_index - 1][-1], "db")
        self.assertEqual(commands[run_index + 1][-1], "api")
        self.assertIn("--rm", commands[run_index])
        self.assertIn("--no-deps", commands[run_index])
        self.assertIn("--wait", commands[run_index + 1])
        self.assertIn("--no-deps", commands[run_index + 1])
        self.assertEqual(commands[-1][-2:], ["caddy", "backup"])
        self.assertEqual((self.root / "current").resolve(), self.release)
        self.assertEqual((self.root / "previous").resolve(), self.old)
        self.assertEqual(self.envfile.read_bytes(), before)
        self.assertEqual(self.envfile.stat().st_mode & 0o777, 0o600)
        for call in self.calls():
            command = call["args"]
            self.assertEqual(call["caddy"], str(self.release / "deploy" / "Caddyfile"))
            self.assertFalse(set(command) & {"down", "prune", "--build", "--platform", "--remove-orphans"})
        self.assert_private(result)

    def test_failures_do_not_advance_release_or_leak_credentials(self):
        for failure in ("login", "config", "digest", "pull", "migration", "health", "optional"):
            with self.subTest(failure=failure):
                result = self.run_deploy(failure)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual((self.root / "current").resolve(), self.old)
                self.assertEqual((self.root / "previous").resolve(), self.older)
                self.assert_private(result)
                (self.root / "calls.jsonl").unlink(missing_ok=True)

    def test_migration_failure_never_restarts_api(self):
        result = self.run_deploy("migration")
        self.assertNotEqual(result.returncode, 0)
        commands = [call["args"] for call in self.calls()]
        self.assertTrue(any("run" in command for command in commands))
        self.assertFalse(any("up" in command and command[-1] == "api" for command in commands))
        diagnostic = self.root / "deploy-last-error.log"
        self.assertIn("private-vm-password", diagnostic.read_text())
        self.assertEqual(diagnostic.stat().st_size, 65536)
        self.assertIn(str(diagnostic), result.stderr)
        self.assert_private(result)

    def test_disabled_profiles_are_not_pulled_or_started(self):
        self.envfile.write_text("POSTGRES_PASSWORD=private-vm-password\n")
        result = self.run_deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = [call["args"] for call in self.calls()]
        changes = [command for command in commands if "pull" in command or "up" in command]
        self.assertFalse(any("caddy" in command or "backup" in command for command in changes))

    def test_removed_profiles_stop_only_this_projects_optional_containers(self):
        self.assertEqual(self.run_deploy().returncode, 0)
        self.assertEqual(set(json.loads((self.root / "running.json").read_text())), {"db", "api", "caddy", "backup"})
        (self.root / "calls.jsonl").unlink()
        self.envfile.write_text("POSTGRES_PASSWORD=private-vm-password\n")
        result = self.run_deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(set(json.loads((self.root / "running.json").read_text())), {"db", "api"})
        stop = next(call["args"] for call in self.calls() if "stop" in call["args"])
        self.assertEqual(stop[-2:], ["caddy", "backup"])
        self.assertEqual(stop[stop.index("--project-name") + 1], "holy-hymns")
        self.assertFalse(set(stop) & {"down", "rm", "prune", "-v", "--volumes"})
        self.assert_private(result)

    def test_valid_bot_registry_usernames(self):
        for username in ("github-actions[bot]", "dependabot[bot]"):
            with self.subTest(username=username):
                result = self.run_deploy(username=username)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assert_private(result)

    def test_same_release_rerun_keeps_previous_pointer(self):
        result = self.run_deploy(release=self.old)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / "previous").resolve(), self.older)
        self.assert_private(result)

    def test_initial_deployment_creates_current_without_previous(self):
        (self.root / "current").unlink()
        (self.root / "previous").unlink()
        result = self.run_deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / "current").resolve(), self.release)
        self.assertFalse((self.root / "previous").exists())
        self.assert_private(result)

    def test_manual_bootstrap_pointers_promote_to_git_release(self):
        bootstrap = self.make_release("bcc50433836ba38d5f2803b31a7bb59d6763028884-1789837653-1")
        prior_bootstrap = self.make_release("4" * 42 + "-10-1")
        legacy_images = "HOLY_HYMNS_IMAGE=holy-hymns:vm-20260919\n"
        (bootstrap / "images.env").write_text(legacy_images)
        (self.root / "current").unlink()
        (self.root / "previous").unlink()
        (self.root / "current").symlink_to("releases/" + bootstrap.name)
        (self.root / "previous").symlink_to("releases/" + prior_bootstrap.name)
        result = self.run_deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / "current").resolve(), self.release)
        self.assertEqual((self.root / "previous").resolve(), bootstrap)
        self.assertEqual((bootstrap / "images.env").read_text(), legacy_images)
        self.assertTrue(prior_bootstrap.is_dir())
        self.assert_private(result)

    def test_incoming_release_still_requires_exact_git_sha_length(self):
        incoming = self.make_release("5" * 42 + "-12-1")
        result = self.run_deploy(release=incoming)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid release directory name", result.stderr)
        self.assertEqual((self.root / "current").resolve(), self.old)
        self.assertEqual((self.root / "previous").resolve(), self.older)
        self.assertEqual(self.calls(), [])

    def test_historical_pointer_still_must_be_direct_child_of_release_root(self):
        outside = Path(self.temporary.name) / ("6" * 42 + "-10-1")
        outside.mkdir()
        (self.root / "previous").unlink()
        (self.root / "previous").symlink_to(outside)
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.root / "current").resolve(), self.old)
        self.assertEqual(self.calls(), [])
        self.assert_private(result)

    def test_failed_optional_stop_does_not_advance_release(self):
        self.envfile.write_text("POSTGRES_PASSWORD=private-vm-password\n")
        result = self.run_deploy("stop")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.root / "current").resolve(), self.old)
        self.assertEqual((self.root / "previous").resolve(), self.older)
        self.assert_private(result)

    def test_rejects_mutable_images_before_docker(self):
        (self.release / "images.env").write_text("HOLY_HYMNS_IMAGE=ghcr.io/grejojoby/holy-hymns:latest\n")
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])
        self.assert_private(result)

    def test_dotenv_is_data_not_executable_shell(self):
        marker = self.root / "must-not-exist"
        self.envfile.write_text(self.envfile.read_text() + f"ARBITRARY=$(touch {marker})\n")
        result = self.run_deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(marker.exists())

    def test_rejects_release_outside_expected_directory(self):
        outside = Path(self.temporary.name) / self.release.name
        outside.mkdir()
        result = self.run_deploy(release=outside)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_refuses_non_symlink_release_pointer(self):
        (self.root / "current").unlink()
        (self.root / "current").write_text("do not replace this file")
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.root / "current").read_text(), "do not replace this file")
        self.assertEqual(self.calls(), [])


if __name__ == "__main__":
    unittest.main()
