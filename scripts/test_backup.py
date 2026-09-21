import datetime as dt
import importlib.util
import json
import sys
import unittest
from unittest.mock import patch
from pathlib import Path

spec = importlib.util.spec_from_file_location("backup", Path(__file__).with_name("backup.py"))
backup = importlib.util.module_from_spec(spec)
sys.modules["backup"] = backup
spec.loader.exec_module(backup)


class BackupPolicyTests(unittest.TestCase):
    def configuration(self):
        return {
            "OCI_FREE_QUOTA_VERIFIED": "true",
            "OCI_VERIFIED_FREE_BYTES": "104857600",
            "BACKUP_STORAGE_CAP_BYTES": "104857600",
            "BACKUP_MAX_ARCHIVE_BYTES": "12582912",
            "BACKUP_REMOTE": "oci:private-existing-bucket/holy-hymns",
            "BACKUP_AGE_RECIPIENT": "age1" + "q" * 58,
        }

    def test_disabled_without_explicit_verification(self):
        with self.assertRaises(ValueError):
            backup.configuration({})

    def test_storage_cap_and_reserve(self):
        env = self.configuration()
        self.assertEqual(backup.configuration(env).hour, 2)
        env["OCI_VERIFIED_FREE_BYTES"] = "1"
        with self.assertRaises(ValueError):
            backup.configuration(env)
        env = self.configuration()
        env["BACKUP_MAX_ARCHIVE_BYTES"] = "20000000"
        with self.assertRaises(ValueError):
            backup.configuration(env)

    def test_retention_does_not_delete_unrelated_or_recent_objects(self):
        now = dt.datetime(2026, 9, 19, tzinfo=dt.timezone.utc)
        old = "holy-hymns-20260910T020000Z-1234abcd.dump.age"
        paths = [old, "holy-hymns-20260918T020000Z-1234abcd.dump.age", "important.txt", "../" + old,
                 "other-app-20200101.dump.age", "holy-hymns-20269999T020000Z-1234abcd.dump.age"]
        self.assertEqual(backup.expired_archives([{"Path": p} for p in paths], now), [old])

    def test_remote_prefix_cannot_target_bucket_root(self):
        for remote in ["oci:bucket", "oci:bucket/", "oci:bucket/../holy-hymns", "oci:bucket/other", "/tmp/backups"]:
            env = self.configuration()
            env["BACKUP_REMOTE"] = remote
            with self.assertRaises(ValueError):
                backup.configuration(env)


    def test_new_archive_verified_before_retention(self):
        config = backup.configuration(self.configuration())
        commands = []
        uploaded = {}
        old = "holy-hymns-20000101T020000Z-1234abcd.dump.age"

        def run(args):
            commands.append(args)
            if args[0:2] == ["rclone", "lsjson"]:
                return json.dumps([{"Path": old, "Size": 100}]).encode()
            if args[0] == "pg_dump":
                Path(args[-1]).write_bytes(b"valid test dump")
            elif args[0] == "age":
                Path(args[args.index("--output") + 1]).write_bytes(b"encrypted test dump")
            elif args[0:2] == ["rclone", "copyto"]:
                if args[2].startswith(config.remote):
                    Path(args[3]).write_bytes(uploaded[args[2]])
                else:
                    uploaded[args[3]] = Path(args[2]).read_bytes()
            return b""

        with patch.object(backup, "command", side_effect=run):
            backup.backup_once(config)
        deletion = commands[-1]
        self.assertEqual(deletion, ["rclone", "deletefile", config.remote + "/" + old])
        self.assertEqual(commands[-2][0:2], ["rclone", "copyto"])

    def test_corrupt_upload_does_not_delete_previous_archives(self):
        config = backup.configuration(self.configuration())
        commands = []

        def run(args):
            commands.append(args)
            if args[0:2] == ["rclone", "lsjson"]:
                return b'[{"Path":"holy-hymns-20000101T020000Z-1234abcd.dump.age","Size":100}]'
            if args[0] == "pg_dump":
                Path(args[-1]).write_bytes(b"dump")
            elif args[0] == "age":
                Path(args[args.index("--output") + 1]).write_bytes(b"encrypted")
            elif args[0:2] == ["rclone", "copyto"] and args[2].startswith(config.remote):
                Path(args[3]).write_bytes(b"corrupted")
            return b""

        with patch.object(backup, "command", side_effect=run):
            with self.assertRaises(RuntimeError):
                backup.backup_once(config)
        self.assertFalse(any(args[0:2] == ["rclone", "deletefile"] for args in commands))

    def test_full_cap_does_not_upload_or_delete(self):
        config = backup.configuration(self.configuration())
        with patch.object(backup, "command", return_value=json.dumps([{"Size": config.cap}]).encode()) as run:
            with self.assertRaises(ValueError):
                backup.backup_once(config)
            self.assertEqual(run.call_count, 1)


if __name__ == "__main__":
    unittest.main()
