#!/usr/bin/env python3
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
MIRROR_TASKS = REPO_ROOT / "ansible/roles/harbor_config/tasks/mirror.yml"


class HarborMirrorValidationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tasks = MIRROR_TASKS.read_text(encoding="utf-8")

    def test_cached_reference_is_removed_before_validation_pull(self):
        lookup = self.tasks.index("- name: Find cached local registry mirror validation image")
        removal = self.tasks.index("- name: Remove cached local registry mirror validation image reference")
        pull = self.tasks.index("- name: Pull image through Harbor registry mirror without a local image reference")

        self.assertLess(lookup, removal)
        self.assertLess(removal, pull)

        removal_task = self.tasks[removal:pull]
        self.assertIn("- image\n      - rm", removal_task)
        self.assertIn("harbor_mirror_validation_image_ref", removal_task)
        self.assertIn("harbor_mirror_cached_validation_image.stdout", removal_task)

    def test_cache_refresh_is_limited_to_enabled_validation(self):
        reference = self.tasks.index("- name: Build Harbor registry mirror validation image reference")
        repository = self.tasks.index("- name: Build expected Harbor repository name for mirror validation")
        refresh_tasks = self.tasks[reference:repository]

        task_names = (
            "Build Harbor registry mirror validation image reference",
            "Find cached local registry mirror validation image",
            "Remove cached local registry mirror validation image reference",
            "Pull image through Harbor registry mirror without a local image reference",
        )
        for position, task_name in enumerate(task_names):
            task_start = refresh_tasks.index(f"- name: {task_name}")
            if position + 1 < len(task_names):
                task_end = refresh_tasks.index(f"- name: {task_names[position + 1]}")
            else:
                task_end = len(refresh_tasks)
            task = refresh_tasks[task_start:task_end]
            self.assertIn("harbor_validate_registry_mirrors | bool", task)
            self.assertIn("harbor_mirror_item.validation.enabled", task)
            self.assertIn("harbor_mirror_item.validation.image", task)

        lookup = self.tasks.index("- name: Find cached local registry mirror validation image")
        reference_task = self.tasks[reference:lookup]
        self.assertIn("harbor_mirror_validation_image_ref", reference_task)

    def test_validation_refresh_does_not_report_configuration_changes(self):
        removal = self.tasks.index("- name: Remove cached local registry mirror validation image reference")
        repository = self.tasks.index("- name: Build expected Harbor repository name for mirror validation")
        refresh_tasks = self.tasks[removal:repository]

        self.assertEqual(refresh_tasks.count("changed_when: false"), 2)


if __name__ == "__main__":
    unittest.main()
