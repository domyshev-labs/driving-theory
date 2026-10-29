"""Build topic navigation from the application question bank."""
import importlib.util
import json
from pathlib import Path
from staged_questions import load_staged_questions, merge_enabled_questions, validate_candidate_dataset

root = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('classification', root / 'scripts/topic-classification.py')
classifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(classifier)
questions = json.loads((root / 'backend/seed/questions.json').read_text())
original_questions = questions
taxonomy = json.loads((root / 'materials/taxonomy.json').read_text())
overrides = json.loads((root / 'materials/topic-overrides.json').read_text())
original_overrides = overrides
staged = load_staged_questions(root)
questions, overrides = merge_enabled_questions(questions, overrides, staged)
validate_candidate_dataset(root, questions, staged)
titles = json.loads((root / 'materials/question-titles-ru.json').read_text())['titles']
leaves = {node['id'] for node in taxonomy['nodes'] if node['parent_id']}
entries = []
for question in questions:
    cid = question['id']
    tags, primary = classifier.classify_text(question['question'], question['answers'])
    if cid in overrides:
        tags = overrides[cid]['topic_ids']
        primary = overrides[cid]['primary_topic_id']
    if not tags or primary not in tags or not set(tags) <= leaves:
        raise ValueError(f'Question {cid} requires a reviewed topic assignment')
    entry = {'canonical_id': cid, 'question': question['question'], 'topic_ids': sorted(tags),
             'primary_topic_id': primary, 'verified': True}
    if cid in titles:
        entry['question_ru'] = titles[cid]
    entries.append(entry)
nodes = [{key: node[key] for key in ('id', 'label_ru', 'parent_id')} for node in taxonomy['nodes']]
payload = json.dumps({'nodes': nodes, 'questions': sorted(entries, key=lambda q: q['canonical_id'])},
                     ensure_ascii=False, indent=2) + '\n'
# Validate and build every entry before saving newly activated content.
if questions != original_questions:
    (root / 'backend/seed/questions.json').write_text(json.dumps(questions, ensure_ascii=False, indent=2) + '\n')
if overrides != original_overrides:
    (root / 'materials/topic-overrides.json').write_text(json.dumps(overrides, ensure_ascii=False, indent=2) + '\n')
for name in ('public/library.json', 'backend/seed/library.json'):
    (root / name).write_text(payload)
print(f'Built topic navigation for {len(entries)} questions.')
