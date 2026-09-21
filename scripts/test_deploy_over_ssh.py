"""Verify deployment boundaries without contacting a host or using credentials."""

from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import deploy_over_ssh as deploy


class DeployOverSSHTests(unittest.TestCase):
    def setUp(self):
        self.env = {
            "VM_HOST": "vm.example.com", "VM_USER": "ubuntu",
            "VM_SSH_KEY": "test-private-key", "VM_KNOWN_HOSTS": "test-pinned-host-key",
            "RELEASE_ID": "a" * 40 + "-123-1",
            "API_IMAGE": "ghcr.io/grejojoby/holy-hymns@sha256:" + "b" * 64,
            "BACKUP_IMAGE": "ghcr.io/grejojoby/holy-hymns-backup@sha256:" + "c" * 64,
            "GHCR_USERNAME": "grejojoby", "GHCR_TOKEN": "test-short-lived-token",
        }
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        for name in deploy.RELEASE_FILES:
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("test deployment file\n")
        (self.root / ".env").write_text("PRIVATE=must-not-upload\n")

    def test_bundle_contains_only_deployment_files_and_immutable_images(self):
        archive = self.root / "bundle.tar.gz"
        deploy.create_bundle(archive, deploy.configuration(self.env), self.root)
        with tarfile.open(archive) as bundle:
            self.assertEqual(set(bundle.getnames()), {*deploy.RELEASE_FILES, "images.env"})
            images = bundle.extractfile("images.env").read().decode()
            self.assertIn("HOLY_HYMNS_IMAGE=" + self.env["API_IMAGE"], images)
            self.assertIn("HOLY_HYMNS_BACKUP_IMAGE=" + self.env["BACKUP_IMAGE"], images)
            self.assertNotIn(self.env["GHCR_TOKEN"], images)
            self.assertEqual(bundle.getmember("images.env").mode, 0o600)

    def test_rejects_missing_secrets_and_injected_or_mutable_configuration(self):
        invalid = {
            "VM_HOST": ["", "-oProxyCommand=bad", "vm; touch /tmp/unwanted", "user@host"],
            "VM_USER": ["root;bad", "user name"],
            "VM_DEPLOY_PATH": ["/", "relative", "/opt/../tmp", "/opt/app;bad", "/opt/with space"],
            "VM_SSH_PORT": ["0", "65536", "22;bad"],
            "RELEASE_ID": ["../../bad", "a" * 40 + "-1-1;bad"],
            "API_IMAGE": ["ghcr.io/grejojoby/holy-hymns:latest", "attacker/image@sha256:" + "b" * 64],
            "BACKUP_IMAGE": ["ghcr.io/grejojoby/holy-hymns-backup:latest"],
            "GHCR_USERNAME": ["someone;bad"], "GHCR_TOKEN": ["", "token\ninjected"],
            "VM_SSH_KEY": [""], "VM_KNOWN_HOSTS": [""],
        }
        for name, values in invalid.items():
            for value in values:
                with self.subTest(name=name, value=value), self.assertRaises(ValueError):
                    deploy.configuration({**self.env, name: value})

    def test_rejects_symlink_bundle_members(self):
        path = self.root / "compose.yaml"
        path.unlink()
        path.symlink_to(self.root / ".env")
        with self.assertRaises(ValueError):
            deploy.create_bundle(self.root / "bundle.tar.gz", deploy.configuration(self.env), self.root)

    def test_host_is_verified_and_token_uses_only_final_ssh_stdin(self):
        calls = []
        local_files = []

        def run(args, **kwargs):
            calls.append((args, kwargs))
            self.assertTrue(kwargs["check"])
            self.assertIn("StrictHostKeyChecking=yes", args)
            self.assertIn("IdentitiesOnly=yes", args)
            self.assertNotIn(self.env["GHCR_TOKEN"], " ".join(args))
            self.assertNotIn("GHCR_TOKEN", kwargs["env"])
            self.assertNotIn("VM_SSH_KEY", kwargs["env"])
            key = Path(args[args.index("-i") + 1])
            local_files.append(key)
            self.assertEqual(key.stat().st_mode & 0o777, 0o600)
            self.assertEqual(key.read_text().strip(), self.env["VM_SSH_KEY"])
            if args[0] == "scp":
                self.assertEqual(kwargs["stdin"], subprocess.DEVNULL)
                local_files.append(Path(args[-2]))
            return subprocess.CompletedProcess(args, 0)

        with patch.object(deploy.subprocess, "run", side_effect=run):
            deploy.deploy(self.env, self.root)
        self.assertEqual([args[0] for args, _ in calls], ["ssh", "scp", "ssh"])
        self.assertEqual(calls[0][1]["input"], b"")
        self.assertEqual(calls[2][1]["input"], b"test-short-lived-token\n")
        self.assertIn("exec bash /opt/holy-hymns/releases/", calls[2][0][-1])
        self.assertTrue(all(not path.exists() for path in local_files))

    def test_failed_transfer_never_runs_remote_deployment_and_cleans_credentials(self):
        calls = []
        keys = []

        def run(args, **kwargs):
            calls.append(args)
            keys.append(Path(args[args.index("-i") + 1]))
            if args[0] == "scp":
                raise subprocess.CalledProcessError(1, args)
            return subprocess.CompletedProcess(args, 0)

        with patch.object(deploy.subprocess, "run", side_effect=run), self.assertRaises(subprocess.CalledProcessError):
            deploy.deploy(self.env, self.root)
        self.assertEqual([args[0] for args in calls], ["ssh", "scp"])
        self.assertTrue(all(not key.exists() for key in keys))

    def test_nondefault_port_is_used_for_both_transports(self):
        with patch.object(deploy.subprocess, "run") as run:
            deploy.deploy({**self.env, "VM_SSH_PORT": "2222"}, self.root)
        for call in run.call_args_list:
            args = call.args[0]
            flag = "-P" if args[0] == "scp" else "-p"
            self.assertEqual(args[args.index(flag) + 1], "2222")


if __name__ == "__main__":
    unittest.main()
