"""Validate reviewed contributions and merge explicitly enabled questions."""
import copy
import json
from pathlib import Path
import re
import stat
from explanation_validation import validate_explanations


FIELDS = {
    'id', 'question', 'help', 'image', 'answers', 'capture_completed',
    'training_enabled', 'topic_ids', 'primary_topic_id', 'classification_reviewed',
    'classification_reason', 'review_required', 'review_reason', 'help_html', 'help_images',
}


def referenced_images(question):
    """Return the primary illustration followed by any explanation illustrations."""
    return ([question['image']] if question['image'] else []) + question.get('help_images', [])


def validate_question_content(question):
    cid = question['id']
    if not re.fullmatch(r'[0-9a-f]{24}', cid) or not question['question'].strip():
        raise ValueError(f'Invalid question identity or wording: {cid}')
    if set(question['help']) != {'en', 'es', 'ru'}:
        raise ValueError(f'Invalid explanation languages for {cid}')
    choices = question['answers']
    if (not 2 <= len(choices) <= 3 or any(type(a.get('correct')) is not bool for a in choices)
            or sum(a['correct'] for a in choices) != 1):
        raise ValueError(f'Invalid answer key for {cid}')
    normalized = {' '.join(a['text'].lower().split()) for a in choices}
    if '' in normalized or len(normalized) != len(choices) or len({a['letter'] for a in choices}) != len(choices):
        raise ValueError(f'Invalid answer choices for {cid}')
    images = referenced_images(question)
    if question['image'] and question['image'] != f'/questions/{cid}_1.jpg':
        raise ValueError(f'Invalid primary image for {cid}')
    if len(set(images)) != len(images) or any(
            not re.fullmatch(r'/questions/' + cid + r'_[1-9][0-9]*\.jpg', image) for image in images):
        raise ValueError(f'Invalid illustration paths for {cid}')
    markup_images = re.findall(r'<img\b[^>]*\bsrc=["\x27]([^"\x27]+)', question.get('help_html', ''), re.I)
    if set(markup_images) != set(question.get('help_images', [])):
        raise ValueError(f'Explanation illustrations differ for {cid}')
    if re.search(r'(?:https?:)?//', question.get('help_html', ''), re.I):
        raise ValueError(f'External explanation asset for {cid}')


def validate_candidate_dataset(root, questions, staged):
    """Check all content and referenced files before any generated output is saved."""
    ids = [q['id'] for q in questions]
    if len(set(ids)) != len(ids):
        raise ValueError('Duplicate active question IDs')
    images = set()
    for question in questions + staged:
        validate_question_content(question)
        for image in referenced_images(question):
            path = Path(root) / 'public' / image.lstrip('/')
            try:
                mode = path.lstat().st_mode
            except FileNotFoundError as error:
                raise ValueError(f'Missing question image: {image}') from error
            if not stat.S_ISREG(mode):
                raise ValueError(f'Question image is not a regular file: {image}')
            with path.open('rb') as stream:
                if stream.read(3) != b'\xff\xd8\xff':
                    raise ValueError(f'Question image is not JPEG: {image}')
            images.add(path.name)
    return images


def load_staged_questions(root):
    path = Path(root) / 'materials/staged/questions.json'
    if not path.exists():
        return []
    questions = json.loads(path.read_text())
    taxonomy = json.loads((Path(root) / 'materials/taxonomy.json').read_text())
    leaves = {n['id'] for n in taxonomy['nodes'] if n['parent_id']}
    seen = set()
    for q in questions:
        cid = q.get('id', '')
        if not re.fullmatch(r'[0-9a-f]{24}', cid) or cid in seen:
            raise ValueError(f'Invalid or duplicate staged question ID: {cid}')
        seen.add(cid)
        if set(q) - FIELDS:
            raise ValueError(f'Unexpected staged metadata for {cid}')
        if type(q.get('training_enabled')) is not bool or q.get('capture_completed') is not True:
            raise ValueError(f'Invalid contribution state for {cid}')
        if type(q.get('review_required')) is not bool:
            raise ValueError(f'Missing review state for {cid}')
        if q['review_required'] and not q.get('review_reason', '').strip():
            raise ValueError(f'Missing review reason for {cid}')
        tags = q.get('topic_ids', [])
        if (q.get('classification_reviewed') is not True or not tags
                or q.get('primary_topic_id') not in tags or not set(tags) <= leaves):
            raise ValueError(f'Invalid reviewed topic assignment for {cid}')
        validate_question_content(q)
        validate_explanations(q)
    return questions


def merge_enabled_questions(questions, overrides, staged):
    """Promote new IDs only; never replace existing wording, keys or topic choices."""
    merged = copy.deepcopy(questions)
    reviewed = copy.deepcopy(overrides)
    existing = {q['id']: q for q in merged}
    for q in staged:
        if not q['training_enabled']:
            continue
        cid = q['id']
        if q['review_required']:
            raise ValueError(f'Question {cid} requires review before activation')
        if cid in existing:
            normalize = lambda value: ' '.join(value.lower().split())
            old = existing[cid]
            old_choices = {normalize(a['text']): a['correct'] for a in old['answers']}
            choices = {normalize(a['text']): a['correct'] for a in q['answers']}
            if normalize(old['question']) != normalize(q['question']) or old_choices != choices or old['image'] != q['image']:
                raise ValueError(f'Question {cid} differs from existing content; review it separately')
            continue
        entry = {field: copy.deepcopy(q[field]) for field in ('id', 'question', 'help', 'image', 'answers', 'help_html', 'help_images')}
        merged.append(entry)
        existing[cid] = entry
        reviewed[cid] = {
            'primary_topic_id': q['primary_topic_id'], 'topic_ids': q['topic_ids'],
            'basis': q['classification_reason'],
        }
    return sorted(merged, key=lambda q: q['id']), reviewed
