"""Check archive isolation, selective activation and existing-content protection."""
import copy
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

from staged_questions import load_staged_questions, merge_enabled_questions
from explanation_validation import validate_explanations


ROOT = Path(__file__).resolve().parents[1]


class StagedQuestionsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.bank = json.loads((ROOT / 'backend/seed/questions.json').read_text())
        cls.overrides = json.loads((ROOT / 'materials/topic-overrides.json').read_text())
        cls.staged = load_staged_questions(ROOT)
        ids = {q['id'] for q in cls.bank}
        cls.new = next(q for q in cls.staged if q['id'] not in ids)
        cls.existing = next(q for q in cls.staged if q['id'] in ids and not q['review_required'])
        cls.conflict = next(q for q in cls.staged if q['review_required'])

    def test_disabled_contributions_leave_active_content_unchanged(self):
        self.assertTrue(all(q['training_enabled'] is False for q in self.staged))
        bank, overrides = merge_enabled_questions(self.bank, self.overrides, self.staged)
        self.assertEqual(bank, self.bank)
        self.assertEqual(overrides, self.overrides)

    def test_selective_activation_keeps_existing_ids_and_reviewed_topics(self):
        chosen = copy.deepcopy(self.new)
        chosen['training_enabled'] = True
        bank, overrides = merge_enabled_questions(self.bank, self.overrides, [chosen])
        self.assertEqual(len(bank), len(self.bank) + 1)
        by_id = {q['id']: q for q in bank}
        self.assertTrue(all(by_id[q['id']] == q for q in self.bank))
        self.assertEqual(by_id[chosen['id']]['help_html'], chosen['help_html'])
        self.assertEqual(overrides[chosen['id']]['topic_ids'], chosen['topic_ids'])
        self.assertEqual(overrides[chosen['id']]['primary_topic_id'], chosen['primary_topic_id'])
        self.assertEqual(merge_enabled_questions(bank, overrides, [chosen]), (bank, overrides))
        chosen['training_enabled'] = False
        self.assertEqual(merge_enabled_questions(bank, overrides, [chosen]), (bank, overrides))

    def test_matching_existing_question_is_not_replaced_or_duplicated(self):
        chosen = copy.deepcopy(self.existing)
        chosen['training_enabled'] = True
        self.assertEqual(merge_enabled_questions(self.bank, self.overrides, [chosen]), (self.bank, self.overrides))

    def test_answer_conflict_blocks_activation_even_if_review_flag_is_cleared(self):
        for flag in (True, False):
            chosen = copy.deepcopy(self.conflict)
            chosen.update(training_enabled=True, review_required=flag)
            with self.subTest(review_required=flag), self.assertRaises(ValueError):
                merge_enabled_questions(self.bank, self.overrides, [chosen])

    def test_invalid_metadata_topics_and_image_paths_are_rejected(self):
        patches = [
            {'training_enabled': 'false'},
            {'unexpected_metadata': 'unused'},
            {'topic_ids': ['routine'], 'primary_topic_id': 'routine'},
            {'image': '/questions/other_1.jpg'},
            {'help_images': ['/elsewhere/image.jpg']},
        ]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'materials/staged').mkdir(parents=True)
            shutil.copyfile(ROOT / 'materials/taxonomy.json', root / 'materials/taxonomy.json')
            for patch in patches:
                chosen = copy.deepcopy(self.new)
                chosen.update(patch)
                (root / 'materials/staged/questions.json').write_text(json.dumps([chosen]))
                with self.subTest(patch=patch), self.assertRaises(ValueError):
                    load_staged_questions(root)

    def test_builder_changes_only_explicitly_activated_content(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ('scripts', 'materials', 'backend/seed'):
                shutil.copytree(ROOT / name, root / name)
            (root / 'public').mkdir()
            shutil.copyfile(ROOT / 'public/library.json', root / 'public/library.json')
            shutil.copytree(ROOT / 'public/questions', root / 'public/questions')
            paths = ('backend/seed/questions.json', 'backend/seed/library.json',
                     'public/library.json', 'materials/topic-overrides.json')
            before = {name: (root / name).read_bytes() for name in paths}

            def build():
                return subprocess.run([sys.executable, str(root / 'scripts/rebuild-library.py')],
                                      capture_output=True, text=True)

            self.assertEqual(build().returncode, 0)
            self.assertEqual(before, {name: (root / name).read_bytes() for name in paths})
            staged = copy.deepcopy(self.staged)
            next(q for q in staged if q['id'] == self.new['id'])['training_enabled'] = True
            path = root / 'materials/staged/questions.json'
            path.write_text(json.dumps(staged))
            self.assertEqual(build().returncode, 0)
            bank = json.loads((root / paths[0]).read_text())
            library = json.loads((root / 'public/library.json').read_text())
            self.assertEqual({q['id'] for q in bank}, {q['id'] for q in self.bank} | {self.new['id']})
            self.assertEqual({q['canonical_id'] for q in library['questions']}, {q['id'] for q in bank})
            self.assertEqual((root / 'public/library.json').read_bytes(), (root / 'backend/seed/library.json').read_bytes())
            after = {name: (root / name).read_bytes() for name in paths}
            next(q for q in staged if q['id'] == self.conflict['id'])['training_enabled'] = True
            path.write_text(json.dumps(staged))
            self.assertNotEqual(build().returncode, 0)
            self.assertEqual(after, {name: (root / name).read_bytes() for name in paths})

    def test_explanation_references_are_rejected_in_text_and_html(self):
        for language in ('en', 'es', 'ru'):
            question = copy.deepcopy(self.new)
            question['help'][language] = '(Page 123 of the handbook)'
            with self.subTest(language=language), self.assertRaises(ValueError):
                validate_explanations(question)
        for markup in ('<p>(Page <b>123</b> of the handbook)</p>',
                       '<p>(P&aacute;gina&nbsp;123 del manual)</p>',
                       '<p>Textbook &quot;Example&quot;</p>'):
            question = copy.deepcopy(self.new)
            question['help_html'] = markup
            with self.subTest(markup=markup), self.assertRaises(ValueError):
                validate_explanations(question)

    def test_traffic_rule_numbers_and_local_illustrations_remain_valid(self):
        question = copy.deepcopy(self.new)
        question['help'] = {'en': 'Keep 1.50 metres. The limit is 90 km/h. Renew every 10 years.', 'es': '', 'ru': ''}
        question['help_images'] = [f'/questions/{question["id"]}_2.jpg']
        question['help_html'] = '<p>Keep <b>1.50</b> metres.</p><img src="' + question['help_images'][0] + '">'
        validate_explanations(question)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'materials/staged').mkdir(parents=True)
            shutil.copyfile(ROOT / 'materials/taxonomy.json', root / 'materials/taxonomy.json')
            (root / 'materials/staged/questions.json').write_text(json.dumps([question]))
            self.assertEqual(load_staged_questions(root), [question])

    def assert_asset_failure_preserves_outputs(self, failure):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ('scripts', 'materials', 'backend/seed', 'public/questions'):
                shutil.copytree(ROOT / name, root / name)
            shutil.copyfile(ROOT / 'public/library.json', root / 'public/library.json')
            staged = copy.deepcopy(self.staged)
            new_ids = {q['id'] for q in staged} - {q['id'] for q in self.bank}
            chosen = next(q for q in staged if q['id'] in new_ids and q['image'])
            image = chosen['image']
            if failure == 'missing explanation':
                image = f'/questions/{chosen["id"]}_2.jpg'
                chosen['help_images'] = [image]
                chosen['help_html'] += '<img src="' + image + '">'
            chosen['training_enabled'] = True
            if failure == 'missing active image':
                image = next(q['image'] for q in self.bank if q['image'])
            path = root / 'public' / image.lstrip('/')
            if path.exists():
                path.unlink()
            if failure == 'non-JPEG':
                path.write_bytes(b'not an image')
            elif failure == 'directory':
                path.mkdir()
            elif failure == 'symlink':
                path.symlink_to(ROOT / 'public' / image.lstrip('/'))
            (root / 'materials/staged/questions.json').write_text(json.dumps(staged))
            outputs = ('backend/seed/questions.json', 'materials/topic-overrides.json',
                       'public/library.json', 'backend/seed/library.json')
            before = {name: (root / name).read_bytes() for name in outputs}
            result = subprocess.run([sys.executable, str(root / 'scripts/rebuild-library.py')],
                                    capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0, result.stdout)
            self.assertIn('image', result.stderr)
            self.assertEqual(before, {name: (root / name).read_bytes() for name in outputs})

    def test_missing_primary_image_fails_before_writes(self):
        self.assert_asset_failure_preserves_outputs('missing primary')

    def test_missing_explanation_image_fails_before_writes(self):
        self.assert_asset_failure_preserves_outputs('missing explanation')

    def test_non_jpeg_image_fails_before_writes(self):
        self.assert_asset_failure_preserves_outputs('non-JPEG')

    def test_missing_existing_active_image_fails_before_writes(self):
        self.assert_asset_failure_preserves_outputs('missing active image')

    def test_image_path_must_be_a_regular_file(self):
        self.assert_asset_failure_preserves_outputs('directory')

    def test_image_symlinks_are_rejected_before_writes(self):
        self.assert_asset_failure_preserves_outputs('symlink')


if __name__ == '__main__':
    unittest.main()
