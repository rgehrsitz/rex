"""Regression tests for review findings; run with unittest discovery."""
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

import compare
import run


class ToolingTests(unittest.TestCase):
    def test_unknown_group_is_contextual(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            before, after = root / 'before', root / 'after'
            before.mkdir()
            after.mkdir()
            (after / 'samples.jsonl').write_text(json.dumps({
                'mode': 'memory', 'logging': 'disabled', 'fixture': {'name': 'unexpected'}, 'churn': 0}))
            with self.assertRaisesRegex(ValueError, 'candidate group has no baseline.*unexpected'):
                compare.compare(before, after)

    def test_churn_is_preserved_in_both_reports(self):
        evidence = Path(__file__).resolve().parents[2] / 'docs/baselines/rex-m0'
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            before, after = root / 'before', root / 'after'
            before.mkdir()
            after.mkdir()
            for churn in (1, 1000):
                data = (evidence / f'churn-{churn}.jsonl').read_text()
                for directory in (before, after):
                    (directory / f'{churn}.jsonl').write_text(data)
            compare.compare(before, after)
            records = json.loads((after / 'comparison.json').read_text())
            self.assertEqual({r['churn'] for r in records}, {1, 1000})
            markdown = (after / 'comparison.md').read_text()
            self.assertIn('Churn keys', markdown)
            for churn in (1, 1000):
                self.assertIn(f'| sparse-100 | {churn} |', markdown)

    def test_source_patch_restores_tracked_and_untracked_tooling(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=root)
            git('init', '-q')
            paths = ['tools/rex_baseline/main.go', 'scripts/baseline/run.py', 'scripts/baseline/new.py']
            for path in paths:
                (root / path).parent.mkdir(parents=True, exist_ok=True)
            for path in paths[:2]:
                (root / path).write_text('original\n')
            git('add', *paths[:2])
            git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'baseline')
            for path in paths:
                (root / path).write_text('updated\n')
            patch = run.source_patch(root, paths)
            git('restore', *paths[:2])
            (root / paths[2]).unlink()
            subprocess.run(['git', 'apply', '-'], cwd=root, input=patch, check=True)
            for path in paths:
                self.assertEqual((root / path).read_text(), 'updated\n')


if __name__ == '__main__':
    unittest.main()
