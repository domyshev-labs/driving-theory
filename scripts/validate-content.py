"""Validate question identity, choices, images and topic-library consistency."""
import json
from pathlib import Path
from staged_questions import load_staged_questions, validate_candidate_dataset

root = Path(__file__).resolve().parents[1]
questions = json.loads((root / 'backend/seed/questions.json').read_text())
staged = load_staged_questions(root)
library_bytes = (root / 'public/library.json').read_bytes()
assert library_bytes == (root / 'backend/seed/library.json').read_bytes(), 'Topic libraries differ'
library = json.loads(library_bytes)
nodes = {node['id']: node for node in library['nodes']}
ids = {q['id'] for q in questions}
images = validate_candidate_dataset(root, questions, staged)
assert images == {p.name for p in (root / 'public/questions').iterdir()}, 'Unused or missing images'
entries = library['questions']
assert len(entries) == len(ids), 'Repeated question records'
assert {q['canonical_id'] for q in entries} == ids, 'Content/navigation mismatch'
for question in entries:
    assert question['verified'] is True
    assert question['primary_topic_id'] in question['topic_ids'], question['canonical_id']
    assert all(tag in nodes and nodes[tag]['parent_id'] for tag in question['topic_ids'])
for node in nodes.values():
    assert not node['parent_id'] or nodes[node['parent_id']]['parent_id'] is None
print(f'Validated {len(ids)} active questions, {len(staged)} staged records and {len(images)} illustrations.')
