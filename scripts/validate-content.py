"""Validate question identity, choices, images and topic-library consistency."""
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[1]
questions = json.loads((root / 'backend/seed/questions.json').read_text())
library_bytes = (root / 'public/library.json').read_bytes()
assert library_bytes == (root / 'backend/seed/library.json').read_bytes(), 'Topic libraries differ'
library = json.loads(library_bytes)
nodes = {node['id']: node for node in library['nodes']}
ids, images = set(), set()
for question in questions:
    cid = question['id']
    assert re.fullmatch(r'[0-9a-f]{24}', cid), cid
    assert cid not in ids, cid
    ids.add(cid)
    assert question['question'].strip(), cid
    assert set(question['help']) == {'en', 'es', 'ru'}, cid
    choices = question['answers']
    assert sum(answer['correct'] is True for answer in choices) == 1, cid
    assert len({answer['letter'] for answer in choices}) == len(choices), cid
    normalized = {' '.join(answer['text'].lower().split()) for answer in choices}
    assert '' not in normalized and len(normalized) == len(choices), cid
    if question['image']:
        assert question['image'] == f'/questions/{cid}_1.jpg', cid
        path = root / 'public' / question['image'].lstrip('/')
        assert path.is_file() and path.read_bytes().startswith(b'\xff\xd8'), cid
        images.add(path.name)
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
print(f'Validated {len(ids)} unique questions and {len(images)} question images.')
