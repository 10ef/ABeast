import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest.mock import patch

import release


class ReleaseTests(unittest.TestCase):
    def test_tag_must_match_version(self):
        release.check_tag("v" + release.version())
        for tag in ("v999.0.0", "v0.1.0-rc.1", "v0.1.0;echo nope"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release.check_tag(tag)

    def test_portable_zip_keeps_resources_and_excludes_installer(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            stage = root / "build/windows-amd64"
            stage.mkdir(parents=True)
            (stage / "ABeast.exe").write_bytes(b"test executable")
            (stage / "runtime.dll").write_bytes(b"companion library")
            (stage / "ABeast Setup 0.1.0 amd64.exe").write_bytes(b"installer")
            release.package("windows-amd64", root / "build", root / "dist")
            archive = next((root / "dist").glob("*.zip"))
            with zipfile.ZipFile(archive) as package:
                self.assertIn("ABeast/ABeast.exe", package.namelist())
                self.assertEqual(package.read("ABeast/runtime.dll"), b"companion library")
                self.assertFalse(any("Setup" in name for name in package.namelist()))

    def test_missing_executable_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                release.package("windows-arm64", Path(directory), Path(directory) / "dist")

    def test_checksums_require_exact_complete_nonempty_asset_set(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            with patch.object(release, "version", return_value="1.2.3"):
                with self.assertRaises(ValueError):
                    release.checksums(output)
                for target, suffix in (("darwin-universal", "dmg"),
                                       ("windows-amd64", "zip"), ("windows-arm64", "zip")):
                    (output / f"ABeast-1.2.3-{target}.{suffix}").write_bytes(b"abc")
                release.checksums(output)
                lines = (output / "SHA256SUMS.txt").read_text().splitlines()
                self.assertEqual(len(lines), 3)
                self.assertTrue(all(line.startswith(
                    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad") for line in lines))
                extra = output / "config.json"
                extra.write_text('{}')
                with self.assertRaises(ValueError):
                    release.checksums(output)
                extra.unlink()
                (output / "ABeast-1.2.3-windows-arm64.zip").write_bytes(b"")
                with self.assertRaises(ValueError):
                    release.checksums(output)


if __name__ == "__main__":
    unittest.main()
