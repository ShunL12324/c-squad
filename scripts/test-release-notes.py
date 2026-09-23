#!/usr/bin/env python3
"""Checks for Release notes extracted from CHANGELOG.md."""

import importlib.util
from pathlib import Path
import unittest


spec = importlib.util.spec_from_file_location("release_notes", Path(__file__).with_name("release-notes.py"))
release_notes = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release_notes)

SHA = "8c74280c108b92ef37f8b4edd14416e8c587d41b"
CHANGELOG = """# Changelog

Intro text.

## v0.10.0 — 2026-09-23

### Added

- New thing.

## v0.9.1 — 2026-09-22

### Fixed

- Old fix.

## [v0.7.1](https://github.com/ShunL12324/c-squad/releases/tag/v0.7.1) — 2026-09-21

- Linked heading.
"""


class ReleaseNotesTests(unittest.TestCase):
    def test_extracts_only_matching_section_and_appends_sha(self):
        self.assertEqual(release_notes.notes(CHANGELOG, "v0.10.0", SHA),
                         f"### Added\n\n- New thing.\n\nSource commit: {SHA}\n")
        self.assertEqual(release_notes.notes(CHANGELOG, "v0.9.1", SHA),
                         f"### Fixed\n\n- Old fix.\n\nSource commit: {SHA}\n")

    def test_linked_heading(self):
        self.assertIn("- Linked heading.", release_notes.notes(CHANGELOG, "v0.7.1", SHA))

    def test_version_prefix_is_not_a_match(self):
        with self.assertRaisesRegex(ValueError, "no section for v0.10.1"):
            release_notes.notes(CHANGELOG, "v0.10.1", SHA)
        with self.assertRaisesRegex(ValueError, "no section for v0.1.0"):
            release_notes.notes(CHANGELOG, "v0.1.0", SHA)

    def test_rejects_unfinished_sections(self):
        cases = {
            "## v1.0.0 — Unreleased\n\n- Thing.\n": "UTC release date",
            "## v1.0.0 — 2026-09-23\n\n## v0.9.0 — 2026-09-01\n\n- Old.\n": "empty",
            "## v1.0.0 — 2026-09-23\n\n### Pending integration\n\n- Thing.\n": "preparation marker",
        }
        for changelog, message in cases.items():
            with self.subTest(changelog=changelog), self.assertRaisesRegex(ValueError, message):
                release_notes.notes(changelog, "v1.0.0", SHA)

    def test_rejects_bad_version_or_sha(self):
        with self.assertRaisesRegex(ValueError, "stable vX.Y.Z"):
            release_notes.notes(CHANGELOG, "0.10.0", SHA)
        with self.assertRaisesRegex(ValueError, "40-character"):
            release_notes.notes(CHANGELOG, "v0.10.0", SHA[:12])

    def test_repository_changelog_has_current_release(self):
        changelog = Path(__file__).resolve().parent.parent / "CHANGELOG.md"
        self.assertIn(f"Source commit: {SHA}", release_notes.notes(changelog.read_text(), "v0.10.0", SHA))


if __name__ == "__main__":
    unittest.main()
